package packapi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
)

// benchmarks.json is the single source of truth for the comparison instances:
// the head-to-head benchmarks in bench_test.go and the doc renders produced by
// cmd/render both build their scenarios from it, so the two never drift. Each
// entry names a bin, an item generator (deterministic from a kind+seed), the
// algorithms to race, and human-facing metadata.
//
//go:embed benchmarks.json
var benchmarksJSON []byte

// BenchScenario is one fully-materialised benchmark/render instance: the bin, the
// generated items, the algorithms to run, and metadata (a slug for file names, a
// group heading for the benchmark table, a title for renders, and a description).
type BenchScenario struct {
	Slug    string
	Group   string
	Mode    string // "1d", "2d", "3d"
	Title   string
	Desc    string
	Bin     BinSpec
	Contact ContactSpec
	Items   []ItemSpec
	Algos   []string
	Gen     BenchGen // the deterministic generator (kind+n+seed) that produced Items
}

// BenchGen is a scenario's item-generator spec: deterministic from kind+n+seed, so
// a preset can carry it (instead of the expanded items) and have the same instance
// regenerated on demand. See GenerateItems.
type BenchGen struct {
	Kind string
	N    int
	Seed uint32
}

// GenerateItems reproduces a generated instance from its spec — the same item set a
// benchmark scenario of (mode, kind, n, seed) holds. It is the single source of the
// generators, shared by the benchmark loader and the on-demand preset generator.
func GenerateItems(mode, kind string, n int, seed uint32) []ItemSpec {
	switch kind {
	case "mix":
		return benchMix(mode, n, seed)
	case "cartons":
		return benchCartons(n, seed)
	case "chunky":
		return benchChunky(n, seed)
	}
	return nil
}

// benchFile mirrors the on-disk JSON shape; items are described by a generator
// spec (kind + count + seed) rather than enumerated, keeping the file compact and
// the instances reproducible.
type benchFile struct {
	Scenarios []struct {
		Slug    string                                 `json:"slug"`
		Group   string                                 `json:"group"`
		Mode    string                                 `json:"mode"`
		Title   string                                 `json:"title"`
		Desc    string                                 `json:"desc"`
		Bin     struct{ Width, Height, Depth float64 } `json:"bin"`
		Contact *ContactSpec                           `json:"contact"`
		Gen     struct {
			Kind string `json:"kind"` // "mix" | "cartons"
			N    int    `json:"n"`
			Seed uint32 `json:"seed"`
		} `json:"gen"`
		Algos []string `json:"algos"`
	} `json:"scenarios"`
}

var (
	benchOnce  sync.Once
	benchCache []BenchScenario
)

// BenchScenarios returns the canonical comparison instances from benchmarks.json,
// with each scenario's items generated. The set is parsed once and cached; the
// returned slice (and its item slices) is shared and must not be mutated.
func BenchScenarios() []BenchScenario {
	benchOnce.Do(func() { benchCache = loadBenchScenarios() })
	return benchCache
}

// BenchScenarioBySlug returns the named scenario, or ok=false if no scenario has
// that slug.
func BenchScenarioBySlug(slug string) (BenchScenario, bool) {
	for _, s := range BenchScenarios() {
		if s.Slug == slug {
			return s, true
		}
	}
	return BenchScenario{}, false
}

func loadBenchScenarios() []BenchScenario {
	var f benchFile
	if err := json.Unmarshal(benchmarksJSON, &f); err != nil {
		panic("packapi: parsing embedded benchmarks.json: " + err.Error())
	}
	out := make([]BenchScenario, len(f.Scenarios))
	for i, s := range f.Scenarios {
		sc := BenchScenario{
			Slug: s.Slug, Group: s.Group, Mode: s.Mode, Title: s.Title, Desc: s.Desc,
			Bin:   BinSpec{Width: s.Bin.Width, Height: s.Bin.Height, Depth: s.Bin.Depth},
			Algos: s.Algos,
			Gen:   BenchGen{Kind: s.Gen.Kind, N: s.Gen.N, Seed: s.Gen.Seed},
		}
		if s.Contact != nil {
			sc.Contact = *s.Contact
		}
		switch s.Gen.Kind {
		case "mix":
			sc.Items = benchMix(s.Mode, s.Gen.N, s.Gen.Seed)
		case "cartons":
			sc.Items = benchCartons(s.Gen.N, s.Gen.Seed)
		case "chunky":
			sc.Items = benchChunky(s.Gen.N, s.Gen.Seed)
		default:
			panic(fmt.Sprintf("packapi: benchmarks.json scenario %q: unknown gen kind %q", s.Slug, s.Gen.Kind))
		}
		out[i] = sc
	}
	return out
}

// ─── deterministic item generators ───────────────────────────────────────────

// benchLCG returns a deterministic [0,1) generator (a linear congruential
// sequence) seeded by seed; identical seeds reproduce identical instances.
func benchLCG(seed uint32) func() float64 {
	s := seed
	if s == 0 {
		s = 1
	}
	return func() float64 { s = s*1664525 + 1013904223; return float64(s>>8) / (1 << 24) }
}

// benchMix builds n items with sizes drawn from a mode-appropriate palette: small
// integer sides for 1-D/3-D, mid-range rectangles for 2-D. 2-D/3-D items rotate.
func benchMix(mode string, n int, seed uint32) []ItemSpec {
	next := benchLCG(seed)
	pick := func(a []float64) float64 { return a[int(next()*float64(len(a)))%len(a)] }

	out := make([]ItemSpec, n)
	for i := range out {
		switch mode {
		case "1d":
			w := pick([]float64{1, 2, 2, 3, 3, 4, 4, 5, 6, 7, 8})
			out[i] = ItemSpec{ID: strconv.Itoa(i), Width: w}
		case "2d":
			sz := []float64{10, 12, 15, 18, 20, 25, 30, 35, 40, 50}
			out[i] = ItemSpec{ID: strconv.Itoa(i), Width: pick(sz), Height: pick(sz), AllowRotate: true}
		default:
			sz := []float64{1, 2, 2, 3, 3, 4, 4, 5, 6}
			out[i] = ItemSpec{ID: strconv.Itoa(i), Width: pick(sz), Depth: pick(sz), Height: pick(sz), AllowRotate: true}
		}
	}
	return out
}

// benchChunky builds n sizable cub-ish boxes with each side drawn from {5,6,7},
// destined for a 12×12×12 bin. The point is combinatorial tension: on a side of 12
// a 7 pairs only with a 5 (7+5=12), 6 pairs with 6, but 6+7 and 7+7 overflow — so
// how items are *grouped* into bins decides the bin count, and a greedy decreasing
// pass can strand a bin that reordering (rr/arr) recovers. This is the small,
// ordering-sensitive regime where the order-search metaheuristics earn their cost,
// which the other (bin-count-saturated) 3-D scenarios don't exercise.
func benchChunky(n int, seed uint32) []ItemSpec {
	next := benchLCG(seed)
	sides := []float64{5, 6, 7}
	pick := func() float64 { return sides[int(next()*float64(len(sides)))%len(sides)] }
	out := make([]ItemSpec, n)
	for i := range out {
		out[i] = ItemSpec{ID: strconv.Itoa(i), Width: pick(), Depth: pick(), Height: pick(), AllowRotate: true}
	}
	return out
}

// benchCartons builds n boxes drawn from a small SKU palette whose footprints and
// heights are divisors of a 12×12×12 bin — repeated, tile-friendly sizes (as in a
// carton/pallet load) where fusing and stacking pay off, unlike the fully-random
// mix. This is a single-level packer comparison, not true (nested) palletizing.
func benchCartons(n int, seed uint32) []ItemSpec {
	next := benchLCG(seed)
	type sku struct{ w, d, h float64 }
	skus := []sku{
		{6, 6, 6}, {6, 6, 3}, {6, 6, 2}, {6, 3, 3}, {4, 4, 4},
		{3, 3, 6}, {12, 6, 2}, {6, 12, 3}, {4, 4, 2}, {3, 6, 4},
	}
	out := make([]ItemSpec, n)
	for i := range out {
		k := skus[int(next()*float64(len(skus)))%len(skus)]
		out[i] = ItemSpec{ID: strconv.Itoa(i), Width: k.w, Depth: k.d, Height: k.h, AllowRotate: true}
	}
	return out
}
