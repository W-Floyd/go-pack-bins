package packapi

import (
	"context"
	"fmt"
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
	group, algo         string // group = section heading (e.g. "3D", "3D · anti-slosh")
	nsPerOp, bins, fill float64
	unfit               int
}

var (
	benchMu    sync.Mutex
	benchRows  = map[string]benchRow{} // keyed by "group/algo"; last write wins
	benchOrder []string                // insertion order of keys
	benchMeta  = map[string]string{}   // group → instance description
)

// recordBench stores one algorithm's result. Go may invoke a benchmark func
// several times while sizing b.N; keying by group/algo and overwriting keeps just
// the final run per algorithm.
func recordBench(r benchRow) {
	benchMu.Lock()
	defer benchMu.Unlock()
	k := r.group + "/" + r.algo
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

// benchTables renders the per-mode comparison tables (the block that lives
// between the README markers).
func benchTables() string {
	// Group rows by section, preserving the order groups first appeared.
	var groups []string
	byGroup := map[string][]benchRow{}
	for _, k := range benchOrder {
		r := benchRows[k]
		if _, seen := byGroup[r.group]; !seen {
			groups = append(groups, r.group)
		}
		byGroup[r.group] = append(byGroup[r.group], r)
	}

	var b strings.Builder
	b.WriteString("_`fill%` = packed volume ÷ (bins × bin volume); higher is tighter. ")
	b.WriteString("Time is per solve; absolute numbers vary by machine._\n")
	for _, g := range groups {
		fmt.Fprintf(&b, "\n### %s — %s\n\n", g, benchMeta[g])
		b.WriteString("| Algorithm | Bins | Fill % | Unfit | Time/op |\n")
		b.WriteString("|-----------|-----:|-------:|------:|--------:|\n")
		for _, r := range byGroup[g] {
			fmt.Fprintf(&b, "| %s | %d | %.1f | %d | %s |\n",
				r.algo, int(r.bins), r.fill, r.unfit, fmtDuration(r.nsPerOp))
		}
	}
	return b.String()
}

// writeBenchMarkdown rewrites the comparison tables in place between the
// <!-- BENCH:START --> / <!-- BENCH:END --> markers of the target file (the repo
// README by default; PACK_BENCH_MD overrides). The benchmark's cwd is the package
// directory, so the README sits one level up.
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
	block := benchMarkerStart + "\n\n" + benchTables() + "\n" + benchMarkerEnd

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

// ─── instances ──────────────────────────────────────────────────────────────

// benchMix builds n deterministic mixed-size items for a mode via a small LCG, so
// the set is reproducible across runs (mirrors the webdemo's bigMix palette).
func benchMix(mode string, n int, seed uint32) []ItemSpec {
	s := seed
	if s == 0 {
		s = 1
	}
	next := func() float64 { s = s*1664525 + 1013904223; return float64(s>>8) / (1 << 24) }
	pick := func(a []float64) float64 { return a[int(next()*float64(len(a)))%len(a)] }

	out := make([]ItemSpec, n)
	for i := range out {
		switch mode {
		case "1d":
			w := pick([]float64{1, 2, 2, 3, 3, 4, 4, 5, 6, 7, 8})
			out[i] = ItemSpec{ID: itoa(i), Width: w}
		case "2d":
			sz := []float64{10, 12, 15, 18, 20, 25, 30, 35, 40, 50}
			out[i] = ItemSpec{ID: itoa(i), Width: pick(sz), Height: pick(sz), AllowRotate: true}
		default:
			sz := []float64{1, 2, 2, 3, 3, 4, 4, 5, 6}
			out[i] = ItemSpec{ID: itoa(i), Width: pick(sz), Depth: pick(sz), Height: pick(sz), AllowRotate: true}
		}
	}
	return out
}

// ─── runners ────────────────────────────────────────────────────────────────

// scenario is one benchmark instance: a section heading (group), the solve mode,
// a human description, the bin, an optional contact spec (support + anti-slosh),
// the items, and the algorithms to race.
type scenario struct {
	group, mode, desc string
	bin               BinSpec
	contact           ContactSpec
	items             []ItemSpec
	algos             []string
}

// runScenarioAlgo times one algorithm of a scenario, reports quality metrics to
// the benchmark output, and records a row for the markdown table.
func runScenarioAlgo(b *testing.B, sc scenario, algo string) {
	req := PackRequest{Mode: sc.mode, Algorithm: algo, Bin: sc.bin, Items: sc.items, Contact: sc.contact}
	volByID := make(map[string]float64, len(sc.items))
	for _, it := range sc.items {
		volByID[it.ID] = binVolume(sc.mode, BinSpec{Width: it.Width, Height: it.Height, Depth: it.Depth})
	}

	var resp PackResponse
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp = PackCtx(context.Background(), req)
	}
	b.StopTimer()

	fill := 0.0
	if denom := float64(resp.BinsUsed) * binVolume(sc.mode, sc.bin); denom > 0 {
		fill = 100 * placedVolume(resp, volByID) / denom
	}
	b.ReportMetric(float64(resp.BinsUsed), "bins")
	b.ReportMetric(fill, "fill%")
	if n := len(resp.Unplaced); n > 0 {
		b.ReportMetric(float64(n), "unfit")
	}
	recordBench(benchRow{
		group: sc.group, algo: algo,
		nsPerOp: float64(b.Elapsed().Nanoseconds()) / float64(b.N),
		bins:    float64(resp.BinsUsed), fill: fill, unfit: len(resp.Unplaced),
	})
}

func runScenario(b *testing.B, sc scenario) {
	benchMu.Lock()
	benchMeta[sc.group] = sc.desc
	benchMu.Unlock()
	for _, algo := range sc.algos {
		algo := algo
		b.Run(algo, func(b *testing.B) { runScenarioAlgo(b, sc, algo) })
	}
}

func BenchmarkAlgos3D(b *testing.B) {
	runScenario(b, scenario{
		group: "3D", mode: "3d", desc: "500 mixed boxes (sides 1–6) into a 20×20×20 bin",
		bin:   BinSpec{Width: 20, Depth: 20, Height: 20},
		items: benchMix("3d", 500, 33),
		algos: []string{"ff", "ffd", "bfd", "nfd", "blf", "ems", "heightmap", "laff", "layer", "auto"},
	})
}

// BenchmarkAlgos3DSlosh re-runs the 3-D instance under a contact spec: a 60%
// bottom-support gate plus 50% lateral anti-slosh targets on both axes. The
// anti-slosh drives a post-solve compaction pass, so this shows its quality and
// time cost. (laff/joint manage their own geometry; the placement strategies —
// extreme-point/blf/ems/heightmap — all honour the support gate.)
func BenchmarkAlgos3DSlosh(b *testing.B) {
	runScenario(b, scenario{
		group: "3D · anti-slosh", mode: "3d",
		desc:    "same 500 boxes with 60% bottom support + 50% side anti-slosh (X & Y)",
		bin:     BinSpec{Width: 20, Depth: 20, Height: 20},
		contact: ContactSpec{Bottom: 0.6, SideX: 0.5, SideY: 0.5},
		items:   benchMix("3d", 500, 33),
		algos:   []string{"ff", "ffd", "bfd", "nfd", "blf", "ems", "heightmap", "layer"},
	})
}

func BenchmarkAlgos2D(b *testing.B) {
	runScenario(b, scenario{
		group: "2D", mode: "2d", desc: "400 mixed rectangles (10–50) into a 300×300 bin",
		bin:   BinSpec{Width: 300, Height: 300},
		items: benchMix("2d", 400, 22),
		algos: []string{"ff", "ffd", "bfd", "nfd", "skyline", "auto"},
	})
}

func BenchmarkAlgos1D(b *testing.B) {
	runScenario(b, scenario{
		group: "1D", mode: "1d", desc: "1000 mixed items (1–8) into capacity-10 bins",
		bin:   BinSpec{Width: 10},
		items: benchMix("1d", 1000, 11),
		algos: []string{"ff", "bf", "wf", "ffd", "bfd", "wfd", "mffd", "auto"},
	})
}
