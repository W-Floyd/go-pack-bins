package packapi

import "github.com/W-Floyd/go-pack-bins/algoreg"

// AlgoCapabilities returns the self-configuration document the front-ends fetch on
// load to build their algorithm dropdowns, advanced-tunable inputs, decoder
// selector, and feature-panel gating — so none of that is hardcoded in the UI.
//
// NOTE (Phase 0): this is currently a hand-written payload that mirrors the
// frontend's former hardcoded tables exactly. A later phase replaces the body with
// algoreg.List() once each algorithm self-registers its descriptor; the wire shape
// served here stays the same, so the front-ends don't change again.
func AlgoCapabilities() algoreg.Payload {
	return algoreg.Payload{
		Modes: map[string][]algoreg.ModeAlgo{
			"1d": {
				{ID: "auto", Label: "Auto (best-of)"},
				{ID: "ff", Label: "First Fit (FF)"},
				{ID: "nf", Label: "Next Fit (NF)"},
				{ID: "nkf", Label: "Next K-Fit (NkF, k=3)"},
				{ID: "bf", Label: "Best Fit (BF)"},
				{ID: "wf", Label: "Worst Fit (WF)"},
				{ID: "awf", Label: "Almost Worst Fit (AWF)"},
				{ID: "rff", Label: "Refined First Fit (RFF)"},
				{ID: "hk", Label: "Harmonic-k (H11)"},
				{ID: "ss", Label: "Sum of Squares (SS)"},
				{ID: "ffd", Label: "First Fit Decreasing (FFD)"},
				{ID: "bfd", Label: "Best Fit Decreasing (BFD)"},
				{ID: "nfd", Label: "Next Fit Decreasing (NFD)"},
				{ID: "wfd", Label: "Worst Fit Decreasing (WFD)"},
				{ID: "mffd", Label: "Modified FFD (MFFD)"},
				{ID: "kk", Label: "Karmarkar-Karp (KK)"},
				{ID: "bc", Label: "Bin Completion (exact)"},
				{ID: "brute", Label: "Brute-force order (small N)"},
				{ID: "beam", Label: "Beam search"},
				{ID: "rr", Label: "Ruin & recreate"},
				{ID: "grasp", Label: "GRASP"},
				{ID: "gbpp", Label: "GBPP (optional items + profit)"},
				{ID: "lex", Label: "Lexicographic objectives"},
				{ID: "pref", Label: "Preference-Fit"},
			},
			"2d": {
				{ID: "auto", Label: "Auto (best-of)"},
				{ID: "ff", Label: "First Fit / MaxRects"},
				{ID: "guillotine", Label: "First Fit / Guillotine"},
				{ID: "skyline", Label: "First Fit / Skyline"},
				{ID: "nf", Label: "Next Fit"},
				{ID: "bf", Label: "Best Fit"},
				{ID: "wf", Label: "Worst Fit"},
				{ID: "ffd", Label: "First Fit Decreasing (FFD)"},
				{ID: "bfd", Label: "Best Fit Decreasing (BFD)"},
				{ID: "nfd", Label: "Next Fit Decreasing (NFD)"},
				{ID: "nfdh", Label: "Shelf NFDH (decreasing height)"},
				{ID: "ffdh", Label: "Shelf FFDH (decreasing height)"},
				{ID: "bfdh", Label: "Shelf BFDH (decreasing height)"},
				{ID: "brute", Label: "Brute-force order (small N)"},
				{ID: "beam", Label: "Beam search"},
				{ID: "rr", Label: "Ruin & recreate"},
				{ID: "grasp", Label: "GRASP"},
				{ID: "sat", Label: "SAT (exact, certified optimal)"},
				{ID: "gbpp", Label: "GBPP (optional items + profit)"},
				{ID: "lex", Label: "Lexicographic objectives"},
				{ID: "strip", Label: "Strip (fixed width, min height)"},
				{ID: "knapsack", Label: "Knapsack (one bin, max value)"},
				{ID: "pref", Label: "Preference-Fit"},
			},
			"3d": {
				{ID: "auto", Label: "Auto (best-of)"},
				{ID: "ff", Label: "First Fit / Extreme-point"},
				{ID: "joint", Label: "Joint (balance + placement, one pass)"},
				{ID: "laff", Label: "Largest-Area-Fit-First (layers)"},
				{ID: "layer", Label: "Layered (flat, sequential — streams)"},
				{ID: "blocks", Label: "Block-building (fused layers — streams)"},
				{ID: "columns", Label: "Column-building (fused columns — streams)"},
				{ID: "assemble", Label: "Assemble blocks → EMS (streams)"},
				{ID: "blf", Label: "Bottom-Left Fill (BLF)"},
				{ID: "ems", Label: "Empty Maximal Space (EMS)"},
				{ID: "fit", Label: "Fitness best-fit (max contact)"},
				{ID: "heightmap", Label: "Heightmap / skyline"},
				{ID: "nf", Label: "Next Fit"},
				{ID: "bf", Label: "Best Fit"},
				{ID: "wf", Label: "Worst Fit"},
				{ID: "ffd", Label: "First Fit Decreasing (FFD)"},
				{ID: "bfd", Label: "Best Fit Decreasing (BFD)"},
				{ID: "nfd", Label: "Next Fit Decreasing (NFD)"},
				{ID: "brute", Label: "Brute-force order (small N)"},
				{ID: "beam", Label: "Beam search"},
				{ID: "rr", Label: "Ruin & recreate"},
				{ID: "arr", Label: "Adaptive ruin & recreate"},
				{ID: "grasp", Label: "GRASP"},
				{ID: "gbpp", Label: "GBPP (optional items + profit)"},
				{ID: "lex", Label: "Lexicographic objectives"},
				{ID: "strip", Label: "Strip (fixed base, min height)"},
				{ID: "knapsack", Label: "Knapsack (one bin, max value)"},
				{ID: "pref", Label: "Preference-Fit"},
			},
		},
		Algos: map[string]algoreg.Capabilities{
			"beam":     {Tunables: []algoreg.Tunable{tCount("beam_width", "Beam width", 6), tCount("beam_branch", "Branch", 4), tCount("beam_max_items", "Max items", 200), tTimeLimit}, Decoder: true},
			"brute":    {Tunables: []algoreg.Tunable{tCount("brute_max_items", "Max items (n!)", 8)}},
			"rr":       {Tunables: []algoreg.Tunable{tCount("search_max_iters", "Max iters", 2000), tCount("search_seed", "Seed", 1), tTimeLimit}, Decoder: true, Preview: true},
			"arr":      {Tunables: []algoreg.Tunable{tCount("search_max_iters", "Max iters", 2000), tCount("search_seed", "Seed", 1), tTimeLimit}, Decoder: true, Preview: true},
			"grasp":    {Tunables: []algoreg.Tunable{tCount("search_max_iters", "Max iters", 2000), tCount("search_restarts", "Restarts", 16), tCount("search_seed", "Seed", 1), tTimeLimit}, Decoder: true},
			"bf":       {Tunables: refineKnobs(), Balanceable: true},
			"wf":       {Tunables: refineKnobs(), Balanceable: true},
			"pref":     {Tunables: refineKnobs(), Balanceable: true},
			"blocks":   {Tunables: []algoreg.Tunable{tCount("block_max_stack", "Max stack", 6)}},
			"auto":     {Balanceable: true},
			"joint":    {Balanceable: true},
			"gbpp":     {Panel: "bincost"},
			"lex":      {Panel: "lex"},
			"strip":    {Panel: "strip"},
			"knapsack": {Panel: "knapsack"},
			"sat": {Tunables: []algoreg.Tunable{
				tTimeLimit,
				{Key: "sat_max_memory_mb", Label: "Memory budget (MB)", Def: 4096, Scale: 1, Min: 64, Step: 256},
			}},
		},
		Decoders: []algoreg.Option{
			{Value: "", Label: "EMS (default, tight)"},
			{Value: "fit", Label: "Fitness best-fit"},
			{Value: "blf", Label: "Bottom-Left Fill"},
			{Value: "heightmap", Label: "Heightmap"},
			{Value: "extreme", Label: "Extreme-point (fast)"},
		},
		LexObjectives: []algoreg.Option{
			{Value: "unplaced", Label: "Fewest unplaced"},
			{Value: "bins", Label: "Fewest bins"},
			{Value: "spread", Label: "Even fill (low spread)"},
		},
	}
}

// tCount builds a positive-integer tunable input (min/step/scale = 1).
func tCount(key, label string, def float64) algoreg.Tunable {
	return algoreg.Tunable{Key: key, Label: label, Def: def, Scale: 1, Min: 1, Step: 1}
}

// tTimeLimit is the shared wall-clock cap input (seconds → ms), default "none".
var tTimeLimit = algoreg.Tunable{Key: "time_limit_ms", Label: "Time limit (s)", DefNone: true, Scale: 1000, Min: 0, Step: 0.5}

// refineKnobs are the balance-refinement tunables shared by bf/wf/pref.
func refineKnobs() []algoreg.Tunable {
	return []algoreg.Tunable{tCount("refine_max_items", "Refine max items", 80), tCount("refine_eval_budget", "Refine eval budget", 40000)}
}
