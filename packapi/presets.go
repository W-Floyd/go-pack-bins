package packapi

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// Presets are the ready-made demo setups the frontend offers in its "Load preset"
// menu. They were formerly hardcoded in the single-page app; serving them from
// here keeps the UI free of that data and lets the benchmark instances (compiled
// in from benchmarks.json) double as presets without duplicating their definitions.
//
// The wire shape matches the frontend's loadPreset() exactly (compact keys: w/h/d
// dimensions, s for scalars), so the page applies a fetched preset unchanged.

//go:embed presets.json
var presetsJSON []byte

// PresetItem is one item in a preset (h/d default to w when absent; rot overrides
// the global rotate toggle; s carries named scalars).
type PresetItem struct {
	W   float64            `json:"w"`
	H   float64            `json:"h,omitempty"`
	D   float64            `json:"d,omitempty"`
	Rot *bool              `json:"rot,omitempty"`
	S   map[string]float64 `json:"s,omitempty"`
}

// PresetGen is a preset whose items are produced on demand by a deterministic
// generator (kind+n+seed), instead of being listed explicitly. It keeps large
// instances (stress/benchmark demos) out of the payload — the frontend asks the
// backend to materialise the items when the preset is loaded (see GeneratePresetItems).
type PresetGen struct {
	Kind string `json:"kind"` // "mix" | "cartons" | "chunky"
	N    int    `json:"n"`
	Seed uint32 `json:"seed,omitempty"`
}

// PresetBin / PresetContainer / PresetContact mirror the compact frontend shapes.
type PresetBin struct {
	W float64 `json:"w"`
	H float64 `json:"h,omitempty"`
	D float64 `json:"d,omitempty"`
}

type PresetContainer struct {
	W    float64 `json:"w"`
	H    float64 `json:"h,omitempty"`
	D    float64 `json:"d,omitempty"`
	Max  int     `json:"max,omitempty"`
	Cost float64 `json:"cost,omitempty"`
}

type PresetContact struct {
	Bottom     float64 `json:"bottom,omitempty"` // % bottom support
	SideX      float64 `json:"side_x,omitempty"` // % side anti-slosh (X)
	SideY      float64 `json:"side_y,omitempty"` // % side anti-slosh (Y)
	NoFloating bool    `json:"no_floating,omitempty"`
}

// Preset is a complete demo setup. Optional fields are omitted when unused, so the
// JSON stays small and matches the legacy hand-written presets byte-for-byte.
type Preset struct {
	Label              string            `json:"label"`
	Algo               string            `json:"algo,omitempty"`
	Bin                PresetBin         `json:"bin"`
	Items              []PresetItem      `json:"items,omitempty"`
	Gen                *PresetGen        `json:"gen,omitempty"`
	Constraints        []ConstraintSpec  `json:"constraints,omitempty"`
	Preferences        []PreferenceSpec  `json:"preferences,omitempty"`
	InnerPreferences   []PreferenceSpec  `json:"innerPreferences,omitempty"`
	Contact            *PresetContact    `json:"contact,omitempty"`
	Nested             bool              `json:"nested,omitempty"`
	InnerBin           *PresetBin        `json:"innerBin,omitempty"`
	InnerAlgo          string            `json:"innerAlgo,omitempty"`
	Catalog            []PresetContainer `json:"catalog,omitempty"`
	InnerCatalog       []PresetContainer `json:"innerCatalog,omitempty"`
	BinCost            float64           `json:"binCost,omitempty"`
	InnerBinCost       float64           `json:"innerBinCost,omitempty"`
	LexObjectives      []string          `json:"lexObjectives,omitempty"`
	InnerLexObjectives []string          `json:"innerLexObjectives,omitempty"`
}

var (
	presetOnce  sync.Once
	presetCache map[string][]Preset
)

// Presets returns the demo presets keyed by mode ("1d"/"2d"/"3d"): the embedded
// curated set first, then one generator preset per benchmark scenario (from
// benchmarks.json). Benchmark presets carry the generator spec, not expanded items,
// so the payload stays small; the frontend materialises items via GeneratePresetItems
// when the preset is loaded. Parsed once and cached; callers must not mutate it.
func Presets() map[string][]Preset {
	presetOnce.Do(func() {
		byMode := map[string][]Preset{}
		if err := json.Unmarshal(presetsJSON, &byMode); err != nil {
			panic("packapi: parsing embedded presets.json: " + err.Error())
		}
		for _, s := range BenchScenarios() {
			// Skip non-generated scenarios and the mega-stress case: defaulting it to
			// the "auto" race over 10k items is impractical, and the curated
			// "Mega stress test" preset already covers the huge-instance demo.
			if s.Gen.Kind == "" || s.Gen.N > presetBenchMaxN {
				continue
			}
			byMode[s.Mode] = append(byMode[s.Mode], benchScenarioPreset(s))
		}
		presetCache = byMode
	})
	return presetCache
}

// GenerateRequest is the on-demand generator request (the /api/generate body and
// the goGenerate WASM argument): regenerate a generator preset's items.
type GenerateRequest struct {
	Mode string `json:"mode"`
	Kind string `json:"kind"`
	N    int    `json:"n"`
	Seed uint32 `json:"seed"`
}

// maxGenerateItems bounds on-demand generation so a bad request can't exhaust
// memory; comfortably above the largest preset (the 10k mega-stress demo).
const maxGenerateItems = 50000

// presetBenchMaxN caps which benchmark scenarios become presets — the 10k
// mega-stress scenario is excluded (see Presets).
const presetBenchMaxN = 2000

// GeneratePresetItems materialises a generator preset's items (PresetItem shape) for
// the given mode — the on-demand counterpart of a PresetGen, served when the UI
// loads a generator preset. Generation itself lives in GenerateItems (single source).
func GeneratePresetItems(mode, kind string, n int, seed uint32) []PresetItem {
	if n > maxGenerateItems {
		n = maxGenerateItems
	}
	if n < 0 {
		n = 0
	}
	gen := GenerateItems(mode, kind, n, seed)
	out := make([]PresetItem, len(gen))
	for i, it := range gen {
		out[i] = itemToPreset(mode, it)
	}
	return out
}

// itemToPreset converts a solver ItemSpec to the compact preset item shape, keeping
// only the dimensions the mode uses.
func itemToPreset(mode string, it ItemSpec) PresetItem {
	pi := PresetItem{W: it.Width, S: it.Scalars}
	if mode != "1d" {
		pi.H = it.Height
	}
	if mode == "3d" {
		pi.D = it.Depth
	}
	if it.AllowRotate {
		rot := true
		pi.Rot = &rot
	}
	return pi
}

// benchScenarioPreset adapts a benchmark scenario into a generator preset: same bin
// and contact (fractions → the percentages the UI inputs use), carrying the
// generator spec rather than expanded items, and defaulting to the "auto" race so
// loading it shows the best packing immediately.
func benchScenarioPreset(s BenchScenario) Preset {
	p := Preset{
		Label: "Benchmark: " + s.Title, Algo: "auto",
		Bin: PresetBin{W: s.Bin.Width},
		Gen: &PresetGen{Kind: s.Gen.Kind, N: s.Gen.N, Seed: s.Gen.Seed},
	}
	if s.Mode != "1d" {
		p.Bin.H = s.Bin.Height
	}
	if s.Mode == "3d" {
		p.Bin.D = s.Bin.Depth
	}
	if c := s.Contact; c.Bottom != 0 || c.SideX != 0 || c.SideY != 0 || c.NoFloating {
		p.Contact = &PresetContact{Bottom: c.Bottom * 100, SideX: c.SideX * 100, SideY: c.SideY * 100, NoFloating: c.NoFloating}
	}
	return p
}
