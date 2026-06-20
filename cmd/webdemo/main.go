package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/meta"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func main() {
	http.Handle("/", http.FileServer(http.Dir("static")))
	http.HandleFunc("/api/pack", handlePack)
	http.HandleFunc("/api/pack/nested", handleNestedPack)
	log.Println("Listening on :8082")
	log.Fatal(http.ListenAndServe(":8082", nil))
}

// ─── request / response types ─────────────────────────────────────────────────

type ItemSpec struct {
	ID          string             `json:"id"`
	Width       float64            `json:"width"`
	Height      float64            `json:"height"`
	Depth       float64            `json:"depth"`
	AllowRotate bool               `json:"allow_rotate"`
	Scalars     map[string]float64 `json:"scalars,omitempty"`
}

type BinSpec struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Depth  float64 `json:"depth"`
}

// ConstraintSpec is a hard constraint on the bin's accumulated scalar totals.
type ConstraintSpec struct {
	Scalar string  `json:"scalar"` // name of the scalar
	Op     string  `json:"op"`     // "max" or "min"
	Value  float64 `json:"value"`
}

// PreferenceSpec is a soft balancing objective on a scalar. When any are
// present the packer switches to PreferenceFit and the algorithm dropdown is
// ignored (preferences are an online-selection concept).
type PreferenceSpec struct {
	Scalar string  `json:"scalar"`           // name of the scalar to balance
	Mode   string  `json:"mode"`             // "concentrate" (fill fullest first) or "balance" (even totals)
	Weight float64 `json:"weight,omitempty"` // relative pull; defaults to 1
}

type PackRequest struct {
	Mode        string           `json:"mode"`      // "1d", "2d", "3d"
	Algorithm   string           `json:"algorithm"` // "ff", "bf", "wf", "nf", "nkf", "awf", "ffd", "bfd", "nfd", "wfd", "mffd", "kk", "bc", "hk", "rff"
	Bin         BinSpec          `json:"bin"`
	Items       []ItemSpec       `json:"items"`
	Constraints []ConstraintSpec `json:"constraints,omitempty"`
	Preferences []PreferenceSpec `json:"preferences,omitempty"`
}

type PlacementResult struct {
	BinIndex int     `json:"bin_index"`
	ItemID   string  `json:"item_id"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
	W        float64 `json:"w"`
	H        float64 `json:"h"`
	D        float64 `json:"d"`
	Rotated  bool    `json:"rotated"`
}

// FreeRect is a free rectangle produced by the guillotine algorithm: [x, y, w, h].
type FreeRect [4]float64

type PackResponse struct {
	BinsUsed   int                `json:"bins_used"`
	Placements []PlacementResult  `json:"placements"`
	Unplaced   []string           `json:"unplaced"`
	FreeRects  [][]FreeRect       `json:"free_rects,omitempty"` // per-bin, guillotine only
	ItemErrors map[string]string  `json:"item_errors,omitempty"`
	BestPacker string             `json:"best_packer,omitempty"` // winning algorithm name (auto mode)
	Error      string             `json:"error,omitempty"`
}

// ─── handler ─────────────────────────────────────────────────────────────────

func handlePack(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(PackResponse{Error: err.Error()})
		return
	}

	var resp PackResponse
	var err error

	switch req.Mode {
	case "1d":
		resp, err = pack1D(req)
	case "2d":
		resp, err = pack2D(req)
	case "3d":
		resp, err = pack3D(req)
	default:
		resp = PackResponse{Error: "unknown mode: " + req.Mode}
	}

	if err != nil {
		resp = PackResponse{Error: err.Error()}
	}
	json.NewEncoder(w).Encode(resp)
}

// ─── 1-D ─────────────────────────────────────────────────────────────────────

func pack1D(req PackRequest) (PackResponse, error) {
	cap := req.Bin.Width
	factory := constrainedFactory(d1.NewFactory(cap), req.Constraints)

	items := make([]pack.Item, len(req.Items))
	for i, spec := range req.Items {
		it := d1.NewItem(spec.ID, spec.Width)
		for k, v := range spec.Scalars {
			it.WithScalar(k, v)
		}
		items[i] = it
	}

	// Preference-fit is its own selection algorithm, scored by the balance objectives.
	if req.Algorithm == "pref" {
		result, perr := runPreferenceFit(factory, buildPreferences(req.Preferences), items)
		if perr != nil {
			return PackResponse{Error: perr.Error()}, nil
		}
		sizeByID := make(map[string]float64, len(req.Items))
		for _, spec := range req.Items {
			sizeByID[spec.ID] = spec.Width
		}
		return buildResponse1D(result, req.Bin.Width, sizeByID), nil
	}

	var result pack.Result
	var err error
	var bestPacker string

	switch req.Algorithm {
	case "nf":
		p := online.NextFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "nkf":
		p := online.NextKFit(3, factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "bf":
		p := online.BestFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "wf":
		p := online.WorstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "awf":
		p := online.AlmostWorstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "rff":
		p := online.NewRFF(cap, factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "hk":
		p := online.NewHarmonicK(11, cap, factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "ffd":
		p := offline.FirstFitDecreasing(factory)
		result, err = p.PackAll(items)
	case "bfd":
		p := offline.BestFitDecreasing(factory)
		result, err = p.PackAll(items)
	case "nfd":
		p := offline.NextFitDecreasing(factory)
		result, err = p.PackAll(items)
	case "wfd":
		p := offline.WorstFitDecreasing(factory)
		result, err = p.PackAll(items)
	case "mffd":
		p := offline.ModifiedFirstFitDecreasing(cap, factory)
		result, err = p.PackAll(items)
	case "kk":
		result, err = offline.KarmarkarKarp(items, cap, factory)
	case "bc":
		result, err = offline.BinCompletion(items, cap, d1.NewFactory(cap), buildConstraints(req.Constraints)...)
	case "auto":
		p := meta.BestOf(
			offline.FirstFitDecreasing(factory),
			offline.BestFitDecreasing(factory),
			offline.WorstFitDecreasing(factory),
			offline.ModifiedFirstFitDecreasing(cap, factory),
			meta.NewFunc("kk", func(it []pack.Item) (pack.Result, error) {
				return offline.KarmarkarKarp(it, cap, d1.NewFactory(cap))
			}),
		)
		result, err = p.PackAll(items)
		bestPacker = p.Winner()
	default:
		p := online.FirstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	}

	if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
		return PackResponse{Error: err.Error()}, nil
	}

	// Build a lookup from item ID → size for offset tracking.
	sizeByID := make(map[string]float64, len(req.Items))
	for _, spec := range req.Items {
		sizeByID[spec.ID] = spec.Width
	}
	resp := buildResponse1D(result, req.Bin.Width, sizeByID)
	resp.BestPacker = bestPacker
	return resp, nil
}

func buildResponse1D(result pack.Result, binWidth float64, sizeByID map[string]float64) PackResponse {
	resp := PackResponse{
		BinsUsed:   result.BinsUsed(),
		Unplaced:   result.Unplaced,
		ItemErrors: placementErrors(result.PlacementErrors),
	}
	// Track how far along each bin we are so we can return x-offsets.
	offsets := make(map[string]float64)
	for _, p := range result.Placements {
		if p == nil {
			continue
		}
		pl1, ok := p.(*d1.Placement1D)
		if !ok {
			continue
		}
		sz := sizeByID[pl1.ItemID()]
		x := offsets[pl1.BinID()]
		resp.Placements = append(resp.Placements, PlacementResult{
			BinIndex: binIndexFromID(result.Bins, pl1.BinID()),
			ItemID:   pl1.ItemID(),
			X:        x,
			W:        sz,
			H:        binWidth,
		})
		offsets[pl1.BinID()] += sz
	}
	return resp
}

// ─── 2-D ─────────────────────────────────────────────────────────────────────

func pack2D(req PackRequest) (PackResponse, error) {
	bw, bh := req.Bin.Width, req.Bin.Height
	makeStrat := d2.NewMaxRectsDefault
	if req.Algorithm == "guillotine" {
		makeStrat = d2.NewGuillotineDefault
	}
	factory := constrainedFactory(d2.NewFactory(bw, bh, makeStrat), req.Constraints)

	items := make([]pack.Item, len(req.Items))
	for i, spec := range req.Items {
		it := d2.NewItem(spec.ID, spec.Width, spec.Height, spec.AllowRotate)
		for k, v := range spec.Scalars {
			it.WithScalar(k, v)
		}
		items[i] = it
	}

	// Preference-fit is its own selection algorithm, scored by the balance objectives.
	if req.Algorithm == "pref" {
		result, perr := runPreferenceFit(factory, buildPreferences(req.Preferences), items)
		if perr != nil {
			return PackResponse{Error: perr.Error()}, nil
		}
		return buildResponse2D(result, false), nil
	}

	var result pack.Result
	var err error
	var bestPacker string

	switch req.Algorithm {
	case "ffd":
		p := offline.FirstFitDecreasing(factory)
		result, err = p.PackAll(items)
	case "bfd":
		p := offline.BestFitDecreasing(factory)
		result, err = p.PackAll(items)
	case "nfd":
		p := offline.NextFitDecreasing(factory)
		result, err = p.PackAll(items)
	case "nf":
		p := online.NextFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "bf":
		p := online.BestFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "wf":
		p := online.WorstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "auto":
		mrFactory := constrainedFactory(d2.NewFactory(bw, bh, d2.NewMaxRectsDefault), req.Constraints)
		gFactory := constrainedFactory(d2.NewFactory(bw, bh, d2.NewGuillotineDefault), req.Constraints)
		p := meta.BestOf(
			offline.FirstFitDecreasing(mrFactory),
			offline.BestFitDecreasing(mrFactory),
			offline.NextFitDecreasing(mrFactory),
			offline.FirstFitDecreasing(gFactory),
			offline.BestFitDecreasing(gFactory),
		)
		result, err = p.PackAll(items)
		bestPacker = p.Winner()
	default: // ff, maxrects, guillotine
		p := online.FirstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	}

	if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
		return PackResponse{Error: err.Error()}, nil
	}

	resp := buildResponse2D(result, req.Algorithm == "guillotine")
	resp.BestPacker = bestPacker
	return resp, nil
}

func buildResponse2D(result pack.Result, includeGuillotineFree bool) PackResponse {
	resp := PackResponse{
		BinsUsed:   result.BinsUsed(),
		Unplaced:   result.Unplaced,
		ItemErrors: placementErrors(result.PlacementErrors),
	}
	for _, p := range result.Placements {
		if p == nil {
			continue
		}
		pl2, ok := p.(*d2.Placement2D)
		if !ok {
			continue
		}
		resp.Placements = append(resp.Placements, PlacementResult{
			BinIndex: binIndexFromID(result.Bins, pl2.BinID()),
			ItemID:   pl2.ItemID(),
			X:        pl2.X,
			Y:        pl2.Y,
			W:        pl2.W,
			H:        pl2.H,
			Rotated:  pl2.Rotated,
		})
	}
	if includeGuillotineFree {
		resp.FreeRects = make([][]FreeRect, len(result.Bins))
		for i, bin := range result.Bins {
			b2, ok := bin.(*d2.Bin2D)
			if !ok {
				continue
			}
			g, ok := b2.Strategy().(*d2.Guillotine)
			if !ok {
				continue
			}
			for _, r := range g.FreeRects() {
				resp.FreeRects[i] = append(resp.FreeRects[i], FreeRect(r))
			}
		}
	}
	return resp
}

// ─── 3-D ─────────────────────────────────────────────────────────────────────

func pack3D(req PackRequest) (PackResponse, error) {
	bw, bd, bh := req.Bin.Width, req.Bin.Depth, req.Bin.Height
	minSup, scalarSpecs := extractMinSupport(req.Constraints)
	stratFn := d3.NewExtremePointStrategy
	if minSup > 0 {
		stratFn = d3.NewExtremePointStrategyWithSupport(minSup)
	}
	factory := constrainedFactory(d3.NewFactory(bw, bd, bh, stratFn), scalarSpecs)

	items := make([]pack.Item, len(req.Items))
	for i, spec := range req.Items {
		it := d3.NewItem(spec.ID, spec.Width, spec.Depth, spec.Height, spec.AllowRotate)
		for k, v := range spec.Scalars {
			it.WithScalar(k, v)
		}
		items[i] = it
	}

	// Preference-fit is its own selection algorithm, scored by the balance objectives.
	if req.Algorithm == "pref" {
		result, perr := runPreferenceFit(factory, buildPreferences(req.Preferences), items)
		if perr != nil {
			return PackResponse{Error: perr.Error()}, nil
		}
		return buildResponse3D(result), nil
	}

	var result pack.Result
	var err error
	var bestPacker string

	switch req.Algorithm {
	case "ffd":
		p := offline.FirstFitDecreasing(factory)
		result, err = p.PackAll(items)
	case "bfd":
		p := offline.BestFitDecreasing(factory)
		result, err = p.PackAll(items)
	case "nfd":
		p := offline.NextFitDecreasing(factory)
		result, err = p.PackAll(items)
	case "nf":
		p := online.NextFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "bf":
		p := online.BestFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "wf":
		p := online.WorstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "auto":
		p := meta.BestOf(
			offline.FirstFitDecreasing(factory),
			offline.BestFitDecreasing(factory),
			offline.NextFitDecreasing(factory),
		)
		result, err = p.PackAll(items)
		bestPacker = p.Winner()
	default: // ff
		p := online.FirstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	}

	if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
		return PackResponse{Error: err.Error()}, nil
	}

	resp := buildResponse3D(result)
	resp.BestPacker = bestPacker
	return resp, nil
}

func buildResponse3D(result pack.Result) PackResponse {
	resp := PackResponse{
		BinsUsed:   result.BinsUsed(),
		Unplaced:   result.Unplaced,
		ItemErrors: placementErrors(result.PlacementErrors),
	}
	for _, p := range result.Placements {
		if p == nil {
			continue
		}
		pl3, ok := p.(*d3.Placement3D)
		if !ok {
			continue
		}
		resp.Placements = append(resp.Placements, PlacementResult{
			BinIndex: binIndexFromID(result.Bins, pl3.BinID()),
			ItemID:   pl3.ItemID(),
			X:        pl3.X,
			Y:        pl3.Y,
			Z:        pl3.Z,
			W:        pl3.W,
			D:        pl3.D,
			H:        pl3.H,
		})
	}
	return resp
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// placementErrors converts a map[string]error to map[string]string for JSON serialisation.
func placementErrors(errs map[string]error) map[string]string {
	if len(errs) == 0 {
		return nil
	}
	out := make(map[string]string, len(errs))
	for k, v := range errs {
		out[k] = v.Error()
	}
	return out
}

// extractMinSupport pulls out any "minsupport" spec (value 0-100) and returns
// the fraction (0-1) plus the remaining scalar-only specs.
func extractMinSupport(specs []ConstraintSpec) (frac float64, rest []ConstraintSpec) {
	for _, s := range specs {
		if s.Op == "minsupport" {
			frac = s.Value / 100.0
		} else {
			rest = append(rest, s)
		}
	}
	return
}

// buildPreferences converts PreferenceSpec slice to weighted pack.Preference slice.
// "balance" spreads a scalar evenly (ColocateLow); anything else concentrates it
// (ColocateHigh). A missing or zero weight defaults to 1.
func buildPreferences(specs []PreferenceSpec) []pack.Preference {
	prefs := make([]pack.Preference, 0, len(specs))
	for _, s := range specs {
		w := s.Weight
		if w == 0 {
			w = 1
		}
		var base pack.Preference
		switch s.Mode {
		case "fillhigh":
			base = pack.FillHigh() // best-fit as a preference, no scalar
		case "filllow":
			base = pack.FillLow() // worst-fit as a preference, no scalar
		case "minheight":
			base = pack.MinimizeHeight() // geometric, no scalar needed
		case "mincg":
			if s.Scalar == "" {
				continue
			}
			base = pack.MinimizeCG(s.Scalar) // s.Scalar names the mass scalar
		case "balance":
			if s.Scalar == "" {
				base = pack.BalanceCount() // no scalar → balance by item count
			} else {
				base = pack.ColocateLow(s.Scalar)
			}
		default: // "concentrate"
			if s.Scalar == "" {
				base = pack.ConcentrateCount() // no scalar → concentrate by item count
			} else {
				base = pack.ColocateHigh(s.Scalar)
			}
		}
		prefs = append(prefs, pack.Weighted(base, w))
	}
	return prefs
}

// runPreferenceFit packs items with the two-phase BalancedFit: it learns the
// minimum bin count, then distributes items across that many bins using the
// preferences. This balances within the fewest bins instead of spilling into an
// extra one the way single-pass online preference selection can. The factory is
// wrapped in a ConstrainedFactory if needed so bins expose Aggregates().
func runPreferenceFit(factory pack.BinFactory, prefs []pack.Preference, items []pack.Item) (pack.Result, error) {
	if _, ok := factory.(*pack.ConstrainedFactory); !ok {
		factory = pack.NewConstrainedFactory(factory)
	}
	r, err := offline.NewBalancedFit(factory, prefs...).PackAll(items)
	if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
		return pack.Result{}, err
	}
	return r, nil
}

// buildConstraints converts ConstraintSpec slice to pack.Constraint slice.
func buildConstraints(specs []ConstraintSpec) []pack.Constraint {
	cs := make([]pack.Constraint, 0, len(specs))
	for _, s := range specs {
		switch s.Op {
		case "max":
			cs = append(cs, pack.MaxAggregate(s.Scalar, s.Value))
		case "min":
			cs = append(cs, pack.MinAggregate(s.Scalar, s.Value))
		case "allsame":
			cs = append(cs, pack.AllSame(s.Scalar))
		}
	}
	return cs
}

// constrainedFactory wraps factory with hard constraints if any are specified.
func constrainedFactory(factory pack.BinFactory, specs []ConstraintSpec) pack.BinFactory {
	cs := buildConstraints(specs)
	if len(cs) == 0 {
		return factory
	}
	return pack.NewConstrainedFactory(factory, cs...)
}

func binIndexFromID(bins []pack.Bin, id string) int {
	for i, b := range bins {
		type idder interface{ ID() string }
		if b2, ok := b.(idder); ok && b2.ID() == id {
			return i
		}
	}
	return 0
}

// ─── nested pack ─────────────────────────────────────────────────────────────

type NestedLevelSpec struct {
	Bin         BinSpec          `json:"bin"`
	Algorithm   string           `json:"algorithm"`
	Constraints []ConstraintSpec `json:"constraints,omitempty"`
	Preferences []PreferenceSpec `json:"preferences,omitempty"`
}

type NestedPackRequest struct {
	Mode   string            `json:"mode"`
	Levels []NestedLevelSpec `json:"levels"`
	Items  []ItemSpec        `json:"items"`
}

type NestedLevelResult struct {
	BinsUsed   int               `json:"bins_used"`
	Placements []PlacementResult `json:"placements"`
	Unplaced   []string          `json:"unplaced"`
	BinDims    BinSpec           `json:"bin_dims"`
}

type NestedPackResponse struct {
	Levels []NestedLevelResult `json:"levels"`
	Error  string              `json:"error,omitempty"`
}

func handleNestedPack(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req NestedPackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(NestedPackResponse{Error: err.Error()})
		return
	}
	resp, err := doNestedPack(req)
	if err != nil {
		json.NewEncoder(w).Encode(NestedPackResponse{Error: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(resp)
}

func doNestedPack(req NestedPackRequest) (NestedPackResponse, error) {
	if len(req.Levels) < 2 {
		return NestedPackResponse{}, fmt.Errorf("nested packing requires at least 2 levels")
	}

	// Level 0: pack items into inner bins (cartons).
	// Forward any minsupport constraint from the outer level so the physical
	// stacking rule is enforced inside cartons as well as on pallets.
	l0spec := req.Levels[0]
	l0Constraints := l0spec.Constraints
	if len(req.Levels) > 1 {
		for _, c := range req.Levels[1].Constraints {
			if c.Op == "minsupport" {
				l0Constraints = append(l0Constraints, c)
				break
			}
		}
	}
	l0req := PackRequest{
		Mode:        req.Mode,
		Algorithm:   l0spec.Algorithm,
		Bin:         l0spec.Bin,
		Items:       req.Items,
		Constraints: l0Constraints,
		Preferences: l0spec.Preferences,
	}
	l0resp, err := packByMode(l0req)
	if err != nil {
		return NestedPackResponse{}, err
	}

	// Accumulate scalars per carton bin from placed items.
	binScalars := make(map[int]map[string]float64)
	itemScalarsByID := make(map[string]map[string]float64, len(req.Items))
	for _, spec := range req.Items {
		itemScalarsByID[spec.ID] = spec.Scalars
	}
	for _, p := range l0resp.Placements {
		if p.ItemID == "" {
			continue
		}
		bs := binScalars[p.BinIndex]
		if bs == nil {
			bs = make(map[string]float64)
			binScalars[p.BinIndex] = bs
		}
		for k, v := range itemScalarsByID[p.ItemID] {
			bs[k] += v
		}
	}

	// Build one carton item per filled bin.
	cartonItems := make([]ItemSpec, l0resp.BinsUsed)
	for b := 0; b < l0resp.BinsUsed; b++ {
		cartonItems[b] = ItemSpec{
			ID:      fmt.Sprintf("carton_%d", b),
			Width:   l0spec.Bin.Width,
			Height:  l0spec.Bin.Height,
			Depth:   l0spec.Bin.Depth,
			Scalars: binScalars[b],
		}
	}

	// Level 1: pack carton items into outer bins (pallets).
	l1spec := req.Levels[1]
	l1req := PackRequest{
		Mode:        req.Mode,
		Algorithm:   l1spec.Algorithm,
		Bin:         l1spec.Bin,
		Items:       cartonItems,
		Constraints: l1spec.Constraints,
		Preferences: l1spec.Preferences,
	}
	l1resp, err := packByMode(l1req)
	if err != nil {
		return NestedPackResponse{}, err
	}

	return NestedPackResponse{
		Levels: []NestedLevelResult{
			{BinsUsed: l0resp.BinsUsed, Placements: l0resp.Placements, Unplaced: l0resp.Unplaced, BinDims: l0spec.Bin},
			{BinsUsed: l1resp.BinsUsed, Placements: l1resp.Placements, Unplaced: l1resp.Unplaced, BinDims: l1spec.Bin},
		},
	}, nil
}

// packByMode dispatches to the appropriate dimensioned packer.
func packByMode(req PackRequest) (PackResponse, error) {
	switch req.Mode {
	case "1d":
		return pack1D(req)
	case "2d":
		return pack2D(req)
	case "3d":
		return pack3D(req)
	default:
		return PackResponse{}, fmt.Errorf("unknown mode: %s", req.Mode)
	}
}
