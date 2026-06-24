package packapi

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// These benchmarks compare the packing algorithms head-to-head on identical,
// deterministic instances, reporting BOTH speed (ns/op, the built-in metric) and
// solution quality as custom metrics: "bins" (containers used — lower is better)
// and "fill%" (packed volume ÷ used-bin volume — higher is tighter). Any items an
// algorithm can't place show up as "unfit".
//
// Run them with, e.g.:
//
//	go test ./packapi/ -bench BenchmarkAlgos3D -benchmem -run '^$'
//	go test ./packapi/ -bench BenchmarkAlgos      # all modes
//
// Running any BenchmarkAlgos* also refreshes the comparison table in the repo
// README, in place between the <!-- BENCH:START --> / <!-- BENCH:END --> markers
// (point PACK_BENCH_MD at another file to write there instead). Plain `go test`
// and CI run no benchmarks, so they never touch the README.
//
// Because the instance is fixed per sub-benchmark, bins/fill% are constant across
// iterations; they're reported from the final solve.

// ─── markdown accumulation ──────────────────────────────────────────────────

type benchRow struct {
	slug, group, algo            string // slug = section marker key; group = heading (e.g. "3D · anti-slosh")
	nsPerOp, bins, fill, compact float64
	unfit                        int
	dnf                          bool // did not finish within the time budget
	budget                       bool // anytime algorithm: ran to the budget, reporting best-so-far
}

// benchSectionMeta is a section's heading and instance description, keyed by slug.
type benchSectionMeta struct{ group, desc string }

// anytimeAlgos are improvement searches that honour the context as a deadline and
// return their best packing found so far when it fires. Unlike algorithms that run
// to completion (and overrun → DNF), these always yield a valid result, so when
// they consume the whole budget we report what they achieved rather than DNF.
var anytimeAlgos = map[string]bool{"rr": true, "arr": true, "grasp": true, "beam": true}

// benchTimeout is the per-solve budget; a solve that doesn't finish is reported
// DNF. It defaults to 1s — an interactive-request budget, the regime in which a
// user actually waits on a synchronous solve — and is overridable via
// PACK_BENCH_TIMEOUT (e.g. "30s") to regenerate an offline-planning table where the
// metaheuristics and exact solvers have room to run.
var benchTimeout = parseBenchTimeout()

func parseBenchTimeout() time.Duration {
	if v := os.Getenv("PACK_BENCH_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 4 * time.Second
}

var (
	benchMu    sync.Mutex
	benchRows  = map[string]benchRow{}         // keyed by "slug/algo"; last write wins
	benchOrder []string                        // insertion order of keys
	benchMeta  = map[string]benchSectionMeta{} // slug → section heading + description
)

// recordBench stores one algorithm's result. Go may invoke a benchmark func
// several times while sizing b.N; keying by slug/algo and overwriting keeps just
// the final run per algorithm.
func recordBench(r benchRow) {
	benchMu.Lock()
	defer benchMu.Unlock()
	k := r.slug + "/" + r.algo
	if _, ok := benchRows[k]; !ok {
		benchOrder = append(benchOrder, k)
	}
	benchRows[k] = r
}

// TestMain runs the suite, then — if any algorithm benchmark recorded a result —
// writes the markdown comparison. It owns os.Exit for the package.
func TestMain(m *testing.M) {
	code := m.Run()
	if len(benchOrder) > 0 {
		if err := writeBenchMarkdown(); err != nil {
			fmt.Fprintln(os.Stderr, "bench markdown:", err)
		}
	}
	os.Exit(code)
}

func fmtDuration(ns float64) string {
	return time.Duration(int64(ns)).Round(time.Microsecond).String()
}

const (
	benchMarkerStart = "<!-- BENCH:START -->"
	benchMarkerEnd   = "<!-- BENCH:END -->"
)

// Per-section markers let each scenario's table update independently: a run that
// touches only some scenarios rewrites just their blocks and preserves the rest.
func sectionStartMarker(slug string) string { return "<!-- BENCH:" + slug + ":START -->" }
func sectionEndMarker(slug string) string   { return "<!-- BENCH:" + slug + ":END -->" }

// sectionInner returns the content currently between a section's markers in doc
// (trimmed of surrounding blank lines), so a section the current run didn't
// regenerate can be carried over verbatim.
func sectionInner(doc, slug string) (string, bool) {
	start, end := sectionStartMarker(slug), sectionEndMarker(slug)
	i := strings.Index(doc, start)
	if i < 0 {
		return "", false
	}
	rest := doc[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		return "", false
	}
	return strings.Trim(rest[:j], "\n"), true
}

// benchLegend is the explanatory paragraph shown once at the top of the comparison
// block, above the per-section tables.
func benchLegend() string {
	var b strings.Builder
	b.WriteString("_Arrows mark the better direction (↓ lower-is-better, ↑ higher-is-better); the best value in each column is **bold** (all ties, unless every row matches). ")
	b.WriteString("`fill%` = packed volume ÷ (bins × bin volume); higher is tighter. ")
	b.WriteString("`compact%` = packed volume ÷ the items' bounding-box volume, averaged over bins — ")
	b.WriteString("how void-free the occupied envelope is, *independent* of how full the bin is, so it isn't flattered by underfill. ")
	fmt.Fprintf(&b, "Each solve is timeboxed to %s (an interactive-request budget; raise PACK_BENCH_TIMEOUT for an offline-planning table); **DNF** = did not finish in time. ", benchTimeout)
	b.WriteString("A `≤`-prefixed time marks an anytime improvement search (rr/arr/grasp/beam) that ran to the budget and reports its best-so-far. ")
	b.WriteString("Time is per solve; absolute numbers vary by machine._")
	return b.String()
}

// renderSectionBody renders one scenario's heading and result table — the content
// that lives between that section's BENCH:<slug> markers.
func renderSectionBody(group, desc string, rows []benchRow) string {
	round1 := func(v float64) float64 { return math.Round(v*10) / 10 }
	usOf := func(ns float64) float64 { return float64(time.Duration(int64(ns)).Round(time.Microsecond)) }
	cell := func(s string, win bool) string {
		if win {
			return "**" + s + "**"
		}
		return s
	}

	wBins := winners(rows, true, func(r benchRow) float64 { return float64(int(r.bins)) })
	wFill := winners(rows, false, func(r benchRow) float64 { return round1(r.fill) })
	wCompact := winners(rows, false, func(r benchRow) float64 { return round1(r.compact) })
	wUnfit := winners(rows, true, func(r benchRow) float64 { return float64(r.unfit) })
	wTime := winners(rows, true, func(r benchRow) float64 { return usOf(r.nsPerOp) })
	// Budget-bound rows always consume the full deadline by design, so they never
	// "win" the time column — drop them from the time-winner highlighting.
	for i, r := range rows {
		if r.budget {
			wTime[i] = false
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "### %s — %s\n\n", group, desc)
	b.WriteString("| Algorithm | Bins ↓ | Fill % ↑ | Compact % ↑ | Unfit ↓ | Time/op ↓ |\n")
	b.WriteString("|-----------|-------:|---------:|------------:|--------:|----------:|\n")
	for i, r := range rows {
		if r.dnf {
			fmt.Fprintf(&b, "| %s | — | — | — | — | **DNF** |\n", r.algo)
			continue
		}
		timeCell := cell(fmtDuration(r.nsPerOp), wTime[i])
		if r.budget {
			timeCell = "≤" + fmtDuration(r.nsPerOp) // ran to the budget; best-so-far reported
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", r.algo,
			cell(fmt.Sprintf("%d", int(r.bins)), wBins[i]),
			cell(fmt.Sprintf("%.1f", r.fill), wFill[i]),
			cell(fmt.Sprintf("%.1f", r.compact), wCompact[i]),
			cell(fmt.Sprintf("%d", r.unfit), wUnfit[i]),
			timeCell)
	}
	return strings.TrimRight(b.String(), "\n")
}

// winners marks which rows hold the best value of a column. key extracts the
// comparable value; lowerBetter picks the direction. All rows tying for best are
// marked — except when every row matches (a uniform column has no winner worth
// highlighting), in which case none are.
func winners(rows []benchRow, lowerBetter bool, key func(benchRow) float64) []bool {
	out := make([]bool, len(rows))
	best, nFin := 0.0, 0 // best over finishers only; DNF rows never win
	for _, r := range rows {
		if r.dnf {
			continue
		}
		k := key(r)
		if nFin == 0 || (lowerBetter && k < best) || (!lowerBetter && k > best) {
			best = k
		}
		nFin++
	}
	if nFin == 0 {
		return out
	}
	win := 0
	for i, r := range rows {
		if !r.dnf && key(r) == best {
			out[i] = true
			win++
		}
	}
	if win == nFin { // every finisher ties — nothing to distinguish
		for i := range out {
			out[i] = false
		}
	}
	return out
}

// writeBenchMarkdown rewrites the comparison block in place between the
// <!-- BENCH:START --> / <!-- BENCH:END --> markers of the target file (the repo
// README by default; PACK_BENCH_MD overrides). Each scenario's table is wrapped in
// its own <!-- BENCH:<slug>:START/END --> markers, so a run that recorded only some
// scenarios refreshes just those blocks and carries the rest over verbatim from the
// existing file — letting individual benchmark sets update as they run. Section
// order is the canonical scenario order, independent of which subset ran. The
// benchmark's cwd is the package directory, so the README sits one level up.
func writeBenchMarkdown() error {
	path := os.Getenv("PACK_BENCH_MD")
	if path == "" {
		path = filepath.Join("..", "README.md")
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc := string(existing)

	// Group this run's recorded rows by slug, preserving first-seen order.
	benchMu.Lock()
	bySlug := map[string][]benchRow{}
	for _, k := range benchOrder {
		r := benchRows[k]
		bySlug[r.slug] = append(bySlug[r.slug], r)
	}
	meta := map[string]benchSectionMeta{}
	for slug, m := range benchMeta {
		meta[slug] = m
	}
	benchMu.Unlock()

	var b strings.Builder
	b.WriteString(benchMarkerStart + "\n\n")
	b.WriteString(benchLegend() + "\n")
	// Canonical section order from the shared scenario set keeps the table order
	// stable no matter which subset of benchmarks ran.
	for _, sc := range BenchScenarios() {
		slug := sc.Slug
		var inner string
		switch rows, fresh := bySlug[slug]; {
		case fresh:
			m := meta[slug]
			inner = renderSectionBody(m.group, m.desc, rows) // regenerated this run
		default:
			prev, ok := sectionInner(doc, slug)
			if !ok {
				continue // never benchmarked yet — no block to carry over
			}
			inner = prev // untouched this run — preserve the existing table
		}
		b.WriteString("\n" + sectionStartMarker(slug) + "\n" + inner + "\n" + sectionEndMarker(slug) + "\n")
	}
	b.WriteString("\n" + benchMarkerEnd)
	block := b.String()

	s := strings.Index(doc, benchMarkerStart)
	e := strings.Index(doc, benchMarkerEnd)
	if s >= 0 && e > s {
		doc = doc[:s] + block + doc[e+len(benchMarkerEnd):]
	} else {
		// Markers absent: append a fresh Benchmarks section so the run isn't lost.
		if !strings.HasSuffix(doc, "\n") {
			doc += "\n"
		}
		doc += "\n## Benchmarks\n\n" + block + "\n"
	}
	return os.WriteFile(path, []byte(doc), 0o644)
}

// ─── runners ────────────────────────────────────────────────────────────────

// scenario is one benchmark instance: a section heading (group), the solve mode,
// a human description, the bin, an optional contact spec (support + anti-slosh),
// the items, and the algorithms to race.
type scenario struct {
	slug              string // section marker key (BENCH:<slug>:START/END)
	group, mode, desc string
	bin               BinSpec
	contact           ContactSpec
	items             []ItemSpec
	algos             []string
}

// splitAlgo parses a scenario algo token into the algorithm id and an optional
// 3-D decoder override for the order-search metaheuristics (see searchDecoder3D).
// "rr" → ("rr", ""); "rr+extreme" → ("rr", "extreme"). The token (with suffix) is
// kept as the row label so the decoder variant is self-describing in the table.
func splitAlgo(tok string) (algo, decoder string) {
	algo, decoder, _ = strings.Cut(tok, "+")
	return algo, decoder
}

// runScenarioAlgo times one algorithm of a scenario, reports quality metrics to
// the benchmark output, and records a row for the markdown table.
func runScenarioAlgo(b *testing.B, sc scenario, algo string) {
	algorithm, decoder := splitAlgo(algo)
	req := PackRequest{Mode: sc.mode, Algorithm: algorithm, Decoder: decoder, Bin: sc.bin, Items: sc.items, Contact: sc.contact}
	volByID := make(map[string]float64, len(sc.items))
	for _, it := range sc.items {
		volByID[it.ID] = binVolume(sc.mode, BinSpec{Width: it.Width, Height: it.Height, Depth: it.Depth})
	}

	// Each solve is timeboxed: if it doesn't finish within benchTimeout the context
	// deadline fires, the solver returns early, and ctx.Err() is set. For most
	// algorithms that means DNF — we stop (no point timing an interrupted solve);
	// solvers that ignore the context (e.g. LAFF) run to completion and likewise
	// count as DNF if they overrun. The improvement searches (anytimeAlgos) are
	// different: they are *designed* to run until the deadline and return their
	// best-so-far, so consuming the budget is expected, not a failure — we record
	// what they achieved and flag the row as budget-bound rather than DNF.
	anytime := anytimeAlgos[algorithm]
	var resp PackResponse
	dnf, ranToBudget := false, false
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), benchTimeout)
		resp = PackCtx(ctx, req)
		timedOut := ctx.Err() != nil
		cancel()
		if timedOut {
			if anytime {
				ranToBudget = true // valid best-so-far in resp; one budget-length solve suffices
			} else {
				dnf = true
			}
			break
		}
	}
	b.StopTimer()

	if dnf {
		b.ReportMetric(0, "DNF")
		recordBench(benchRow{slug: sc.slug, group: sc.group, algo: algo, dnf: true})
		return
	}

	fill := 0.0
	if denom := float64(resp.BinsUsed) * binVolume(sc.mode, sc.bin); denom > 0 {
		fill = 100 * placedVolume(resp, volByID) / denom
	}
	compact := MeanCompactnessPct(resp, sc.mode)
	b.ReportMetric(float64(resp.BinsUsed), "bins")
	b.ReportMetric(fill, "fill%")
	b.ReportMetric(compact, "compact%")
	if n := len(resp.Unplaced); n > 0 {
		b.ReportMetric(float64(n), "unfit")
	}
	nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
	if ranToBudget {
		nsPerOp = float64(benchTimeout.Nanoseconds()) // ran to the deadline by design
	}
	recordBench(benchRow{
		slug: sc.slug, group: sc.group, algo: algo,
		nsPerOp: nsPerOp,
		bins:    float64(resp.BinsUsed), fill: fill, compact: compact, unfit: len(resp.Unplaced),
		budget: ranToBudget,
	})
}

func runScenario(b *testing.B, sc scenario) {
	benchMu.Lock()
	benchMeta[sc.slug] = benchSectionMeta{group: sc.group, desc: sc.desc}
	benchMu.Unlock()
	for _, algo := range sc.algos {
		algo := algo
		b.Run(algo, func(b *testing.B) { runScenarioAlgo(b, sc, algo) })
	}
	// Refresh this section's table now (not only at TestMain), so each benchmark set
	// updates the README as it finishes and a partial/interrupted run still lands its
	// completed sections.
	if err := writeBenchMarkdown(); err != nil {
		b.Logf("bench markdown: %v", err)
	}
}

// benchScenario fetches a shared scenario (from packapi's benchmarks.json — the
// same instances cmd/render draws) by slug and adapts it to the runner's struct.
func benchScenario(b *testing.B, slug string) scenario {
	b.Helper()
	s, ok := BenchScenarioBySlug(slug)
	if !ok {
		b.Fatalf("unknown bench scenario %q", slug)
	}
	return scenario{
		slug: s.Slug, group: s.Group, mode: s.Mode, desc: s.Desc,
		bin: s.Bin, contact: s.Contact, items: s.Items, algos: s.Algos,
	}
}

func BenchmarkAlgos3D(b *testing.B) { runScenario(b, benchScenario(b, "3d-mixed")) }

// BenchmarkAlgos3DSlosh re-runs the 3-D instance under a contact spec: a 60%
// bottom-support gate plus 50% lateral anti-slosh targets on both axes. The
// anti-slosh drives a post-solve compaction pass, so this shows its quality and
// time cost. (laff/joint manage their own geometry; the placement strategies —
// extreme-point/blf/ems/heightmap — all honour the support gate.)
func BenchmarkAlgos3DSlosh(b *testing.B) { runScenario(b, benchScenario(b, "3d-slosh")) }

// BenchmarkAlgos3DCartons is the case the block/assemble packers are built for:
// repeated carton SKUs that tile and stack into a 12×12×12 bin. Fusion should match
// or beat the free packers here, where on the fully-random instance it lags. (This
// is a single-bin comparison; true cartons-into-pallets is nested mode.)
func BenchmarkAlgos3DCartons(b *testing.B) { runScenario(b, benchScenario(b, "3d-cartons")) }

// BenchmarkAlgos3DMega is the scalability stress test: 10 000 mixed boxes into a
// large 75×75×75 bin. With the 1 s timebox, the O(k²)-per-insert placers (extreme-
// point / EMS / heightmap) DNF on the huge per-bin item counts, while the layered
// and block packers — which cap per-step work — finish. It's the case that
// separates "scales" from "doesn't".
func BenchmarkAlgos3DMega(b *testing.B) { runScenario(b, benchScenario(b, "3d-mega")) }

// BenchmarkAlgos3DChunky is the small, ordering-sensitive instance: 24 sizable
// boxes (sides 5–7) into a 12×12×12 bin, where a 7 pairs only with a 5 on a side
// and how items group into bins sets the count. It's the regime the order-search
// metaheuristics are for — rr/arr save a bin the greedy/constructive packers
// strand — which the bin-count-saturated scenarios above never exercise.
func BenchmarkAlgos3DChunky(b *testing.B) { runScenario(b, benchScenario(b, "3d-chunky")) }

func BenchmarkAlgos2D(b *testing.B) { runScenario(b, benchScenario(b, "2d")) }

func BenchmarkAlgos1D(b *testing.B) { runScenario(b, benchScenario(b, "1d")) }
