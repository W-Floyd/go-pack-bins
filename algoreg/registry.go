// Package algoreg is the self-describing algorithm registry: the single source of
// truth for which packing algorithms exist and what each can do. It is a leaf
// package (it imports only pack), so every layer can reach it without an import
// cycle — the registry's container lives here, while the dispatch closures that
// compose the dimension/online/offline packers are registered into it from
// packapi (the only package importing that full set).
//
// The metadata below is serialized verbatim to the front-ends (HTTP server and
// WASM), which build their UI from it instead of hardcoding per-algorithm lists,
// tunables, and feature gating. JSON tags live here but algoreg imports no
// encoding/json; the cmd layers do the marshaling.
package algoreg

// Tunable describes one numeric "advanced" input a UI renders for an algorithm.
// The input value is multiplied by Scale to produce the algorithm_options value
// the solver reads (e.g. a seconds input with Scale 1000 → time_limit_ms).
type Tunable struct {
	Key     string  `json:"key"`                // algorithm_options key
	Label   string  `json:"label"`              // UI label
	Def     float64 `json:"def"`                // placeholder default (ignored when DefNone)
	DefNone bool    `json:"def_none,omitempty"` // blank/"none" default (e.g. time limit)
	Scale   float64 `json:"scale"`              // input × Scale → option value (1 = identity)
	Min     float64 `json:"min"`                // input min attribute
	Step    float64 `json:"step"`               // input step attribute
}

// Option is a value/label pair for a UI dropdown (decoders, lex objectives).
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Capabilities is the mode-independent description of one algorithm. Labels and
// mode membership live in Payload.Modes (labels vary by mode), so this carries
// only what the UI gates on regardless of mode.
type Capabilities struct {
	Tunables    []Tunable `json:"tunables,omitempty"`
	Decoder     bool      `json:"decoder,omitempty"`     // 3-D order-search: show the decoder dropdown
	Balanceable bool      `json:"balanceable,omitempty"` // can host balance/preference objectives
	Streaming   bool      `json:"streaming,omitempty"`   // commits placements incrementally
	Preview     bool      `json:"preview,omitempty"`     // anytime search: emits live best-so-far snapshots
	Panel       string    `json:"panel,omitempty"`       // extra UI panel: "bincost" (gbpp) | "lex" | ""
}

// ModeAlgo is one ordered entry in a mode's algorithm dropdown.
type ModeAlgo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Payload is the full self-configuration document served to the front-ends.
type Payload struct {
	// Modes maps "1d"/"2d"/"3d" to that mode's ordered dropdown (id + per-mode label).
	Modes map[string][]ModeAlgo `json:"modes"`
	// Algos maps an algorithm id to its mode-independent capabilities.
	Algos map[string]Capabilities `json:"algos"`
	// Decoders is the 3-D order-search decoder dropdown (rr/arr/beam/grasp).
	Decoders []Option `json:"decoders"`
	// LexObjectives is the label set for the lexicographic-objective picker.
	LexObjectives []Option `json:"lex_objectives"`
}
