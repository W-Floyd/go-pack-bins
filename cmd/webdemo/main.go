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
	http.HandleFunc("/api/pack/stream", handlePackStream)
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
	Contact     ContactSpec      `json:"contact,omitempty"` // per-face support/anti-slosh
}

// ContactSpec is the per-face contact requirement (fractions 0-1). Bottom is a
// hard support gate (3-D); SideX/SideY are lateral anti-slosh targets that drive
// contact-maximizing placement and lateral compaction.
type ContactSpec struct {
	Bottom     float64 `json:"bottom,omitempty"`
	SideX      float64 `json:"side_x,omitempty"`
	SideY      float64 `json:"side_y,omitempty"`
	NoFloating bool    `json:"no_floating,omitempty"` // every item must rest on floor/box (3-D)
}

// lateralAxes reports which lateral axes have an anti-slosh target (and whether any do).
func (c ContactSpec) lateralAxes() (doX, doY, any bool) {
	return c.SideX > 0, c.SideY > 0, c.SideX > 0 || c.SideY > 0
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

// ─── streaming handler ─────────────────────────────────────────────────────────

// streamBatch is the placement-batch chunk size. Placements are emitted in
// solve order so the client can render them progressively as they "appear".
const streamBatch = 64

// streamFrame is one NDJSON line on the /api/pack/stream response. Exactly one of
// the optional groups is populated per frame, keyed by Type:
//   - "meta":  bin/algorithm summary, sent once before any placements
//   - "batch": a slice of placements (in solve order)
//   - "done":  terminal marker
//   - "error": fatal error; terminal
type streamFrame struct {
	Type       string            `json:"type"`
	BinsUsed   int               `json:"bins_used,omitempty"`
	Count      int               `json:"count,omitempty"` // total placements to expect (meta)
	BestPacker string            `json:"best_packer,omitempty"`
	FreeRects  [][]FreeRect      `json:"free_rects,omitempty"`
	ItemErrors map[string]string `json:"item_errors,omitempty"`
	Unplaced   []string          `json:"unplaced,omitempty"`
	Placements []PlacementResult `json:"placements,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// handlePackStream runs the same packing as /api/pack but delivers the result as
// a stream of NDJSON frames (chunked + flushed): one "meta" frame, then placement
// "batch" frames in solve order, then "done". The solve itself runs to completion
// server-side (it is fast); the client paces rendering as frames arrive. The frame
// protocol leaves room for a future genuinely-incremental solver to emit batches
// mid-solve without any client change.
func handlePackStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering if present

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	enc := json.NewEncoder(w)
	send := func(f streamFrame) {
		_ = enc.Encode(f) // Encode writes a trailing newline → NDJSON
		flusher.Flush()
	}

	var req PackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		send(streamFrame{Type: "error", Error: err.Error()})
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
	if resp.Error != "" {
		send(streamFrame{Type: "error", Error: resp.Error})
		return
	}

	send(streamFrame{
		Type:       "meta",
		BinsUsed:   resp.BinsUsed,
		Count:      len(resp.Placements),
		BestPacker: resp.BestPacker,
		FreeRects:  resp.FreeRects,
		ItemErrors: resp.ItemErrors,
		Unplaced:   resp.Unplaced,
	})
	for i := 0; i < len(resp.Placements); i += streamBatch {
		j := i + streamBatch
		if j > len(resp.Placements) {
			j = len(resp.Placements)
		}
		send(streamFrame{Type: "batch", Placements: resp.Placements[i:j]})
	}
	send(streamFrame{Type: "done"})
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

	// Balance objectives layer on any balanceable algorithm (bf/wf/pref/auto).
	if prefs, weights := buildPreferences(req.Preferences); isBalanceable(req.Algorithm) && (len(prefs) > 0 || req.Algorithm == "pref") {
		result, best, perr := runBalanced(req.Algorithm, factory, prefs, weights, items)
		if perr != nil && !errors.Is(perr, pack.ErrItemTooLarge) {
			return PackResponse{Error: perr.Error()}, nil
		}
		sizeByID := make(map[string]float64, len(req.Items))
		for _, spec := range req.Items {
			sizeByID[spec.ID] = spec.Width
		}
		resp := buildResponse1D(result, req.Bin.Width, sizeByID)
		resp.BestPacker = best
		return resp, nil
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

	// Balance objectives layer on any balanceable algorithm (bf/wf/pref/auto).
	if prefs, weights := buildPreferences(req.Preferences); isBalanceable(req.Algorithm) && (len(prefs) > 0 || req.Algorithm == "pref") {
		result, best, perr := runBalanced(req.Algorithm, factory, prefs, weights, items)
		if perr != nil && !errors.Is(perr, pack.ErrItemTooLarge) {
			return PackResponse{Error: perr.Error()}, nil
		}
		if dx, dy, any := req.Contact.lateralAxes(); any {
			compactResult2D(result, bw, bh, dx, dy)
		}
		resp := buildResponse2D(result, false)
		resp.BestPacker = best
		return resp, nil
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

	// Skip compaction for guillotine — it would desync the free-rect overlay.
	if dx, dy, any := req.Contact.lateralAxes(); any && req.Algorithm != "guillotine" {
		compactResult2D(result, bw, bh, dx, dy)
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
	// Bottom → hard support gate; SideX/SideY → contact-maximizing placement.
	stratFn := d3.NewExtremePointStrategyContact(d3.ContactSpec{
		Bottom: req.Contact.Bottom, SideX: req.Contact.SideX, SideY: req.Contact.SideY,
		NoFloating: req.Contact.NoFloating,
	})
	factory := constrainedFactory(d3.NewFactory(bw, bd, bh, stratFn), req.Constraints)

	items := make([]pack.Item, len(req.Items))
	for i, spec := range req.Items {
		it := d3.NewItem(spec.ID, spec.Width, spec.Depth, spec.Height, spec.AllowRotate)
		for k, v := range spec.Scalars {
			it.WithScalar(k, v)
		}
		items[i] = it
	}

	// Balance objectives layer on any balanceable algorithm (bf/wf/pref/auto).
	if prefs, weights := buildPreferences(req.Preferences); isBalanceable(req.Algorithm) && (len(prefs) > 0 || req.Algorithm == "pref") {
		result, best, perr := runBalanced(req.Algorithm, factory, prefs, weights, items)
		if perr != nil && !errors.Is(perr, pack.ErrItemTooLarge) {
			return PackResponse{Error: perr.Error()}, nil
		}
		if dx, dy, any := req.Contact.lateralAxes(); any {
			compactResult3D(result, bw, bd, bh, dx, dy, req.Contact.Bottom)
		}
		resp := buildResponse3D(result)
		resp.BestPacker = best
		return resp, nil
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

	if dx, dy, any := req.Contact.lateralAxes(); any {
		compactResult3D(result, bw, bd, bh, dx, dy, req.Contact.Bottom)
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

// buildPreferences converts PreferenceSpec slice to parallel preference and
// weight slices. Weights are kept separate (not baked via pack.Weighted) so the
// normalized selector can apply them after min-max normalizing each preference —
// making weights comparable across preferences on different scales. "balance"
// spreads a scalar evenly (ColocateLow); "concentrate" groups it (ColocateHigh);
// an empty scalar uses item count. A missing or zero weight defaults to 1.
func buildPreferences(specs []PreferenceSpec) (prefs []pack.Preference, weights []float64) {
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
		prefs = append(prefs, base)
		weights = append(weights, w)
	}
	return prefs, weights
}

// runPreferenceFit packs items with the two-phase BalancedFit: it learns the
// minimum bin count, then distributes items across that many bins using the
// preferences. This balances within the fewest bins instead of spilling into an
// extra one the way single-pass online preference selection can. The factory is
// wrapped in a ConstrainedFactory if needed so bins expose Aggregates().
// isBalanceable reports whether balance objectives can layer on an algorithm.
// Only fit policies expressible as preferences qualify: Best-Fit (FillHigh),
// Worst-Fit (FillLow), Preference-Fit (objectives only), and Auto (which then
// chooses among the balanceable fit flavors).
func isBalanceable(algo string) bool {
	switch algo {
	case "bf", "wf", "pref", "auto":
		return true
	}
	return false
}

// runBalanced layers balance objectives on a balanceable algorithm via the
// two-pass BalancedFit. Best-Fit/Worst-Fit prepend a fill preference (at weight
// 1) so the distribution leans full/empty; Auto tries both balanceable flavors
// and keeps the one using fewer bins, breaking ties on lower imbalance. Returns
// the winning flavor label (for Auto).
func runBalanced(algo string, factory pack.BinFactory, prefs []pack.Preference, weights []float64, items []pack.Item) (pack.Result, string, error) {
	if _, ok := factory.(*pack.ConstrainedFactory); !ok {
		factory = pack.NewConstrainedFactory(factory)
	}
	run := func(fill pack.Preference) (pack.Result, error) {
		p, w := prefs, weights
		if fill != nil {
			p = append([]pack.Preference{fill}, prefs...)
			w = append([]float64{1}, weights...)
		}
		r, err := offline.NewBalancedFitW(factory, p, w).PackAll(items)
		// Local-search pass: move/swap items between bins to tighten the balance
		// (no-op above RefineBalanceMaxItems items).
		return offline.RefineBalance(factory, r, items), err
	}
	switch algo {
	case "bf":
		r, err := run(pack.FillHigh())
		return r, "", err
	case "wf":
		r, err := run(pack.FillLow())
		return r, "", err
	case "pref":
		r, err := run(nil)
		return r, "", err
	case "auto":
		rh, eh := run(pack.FillHigh())
		rl, el := run(pack.FillLow())
		hOK := eh == nil || errors.Is(eh, pack.ErrItemTooLarge)
		lOK := el == nil || errors.Is(el, pack.ErrItemTooLarge)
		switch {
		case hOK && !lOK:
			return rh, "Best-Fit + balance", eh
		case lOK && !hOK:
			return rl, "Worst-Fit + balance", el
		case rh.BinsUsed() != rl.BinsUsed():
			if rh.BinsUsed() < rl.BinsUsed() {
				return rh, "Best-Fit + balance", eh
			}
			return rl, "Worst-Fit + balance", el
		case binImbalance(rh, items) <= binImbalance(rl, items):
			return rh, "Best-Fit + balance", eh
		default:
			return rl, "Worst-Fit + balance", el
		}
	}
	return pack.Result{}, "", nil
}

// binImbalance scores how unevenly a result spreads its metrics across bins,
// summing the coefficient of variation (σ/mean) of item count and each scalar.
// Lower is more balanced; used to break Auto ties between fit flavors.
func binImbalance(r pack.Result, items []pack.Item) float64 {
	scalars := make(map[string]map[string]float64, len(items))
	for _, it := range items {
		scalars[it.ID()] = pack.ScalarsOf(it)
	}
	// Per-bin count and per-bin scalar totals.
	keys := map[string]bool{}
	counts := map[string]float64{}
	totals := map[string]map[string]float64{} // binID -> scalar -> sum
	for _, p := range r.Placements {
		if p == nil {
			continue
		}
		b := p.BinID()
		counts[b]++
		if totals[b] == nil {
			totals[b] = map[string]float64{}
		}
		for k, v := range scalars[p.ItemID()] {
			totals[b][k] += v
			keys[k] = true
		}
	}
	n := r.BinsUsed()
	if n == 0 {
		return 0
	}
	cv := func(vals []float64) float64 {
		mean := 0.0
		for _, v := range vals {
			mean += v
		}
		mean /= float64(n)
		if mean == 0 {
			return 0
		}
		varc := 0.0
		for _, v := range vals {
			varc += (v - mean) * (v - mean)
		}
		return (varc/float64(n)) / (mean * mean) // σ²/mean² — scale-free
	}
	// item count
	countVals := make([]float64, 0, n)
	for _, b := range r.Bins {
		countVals = append(countVals, counts[binID(b)])
	}
	total := cv(countVals)
	// each scalar
	for k := range keys {
		vals := make([]float64, 0, n)
		for _, b := range r.Bins {
			id := binID(b)
			vals = append(vals, totals[id][k])
		}
		total += cv(vals)
	}
	return total
}

// compactResult3D slides each bin's items toward the lateral walls to remove
// slosh (in place on the d3 placements).
func compactResult3D(r pack.Result, bw, bd, bh float64, doX, doY bool, support float64) {
	byBin := map[string][]*d3.Placement3D{}
	for _, p := range r.Placements {
		if pl, ok := p.(*d3.Placement3D); ok {
			byBin[pl.BinID()] = append(byBin[pl.BinID()], pl)
		}
	}
	for _, ps := range byBin {
		d3.Compact(ps, bw, bd, bh, doX, doY, support)
	}
}

// compactResult2D is the 2-D equivalent of compactResult3D.
func compactResult2D(r pack.Result, bw, bh float64, doX, doY bool) {
	byBin := map[string][]*d2.Placement2D{}
	for _, p := range r.Placements {
		if pl, ok := p.(*d2.Placement2D); ok {
			byBin[pl.BinID()] = append(byBin[pl.BinID()], pl)
		}
	}
	for _, ps := range byBin {
		d2.Compact(ps, bw, bh, doX, doY)
	}
}

// binID extracts a bin's ID via its optional ID() method.
func binID(b pack.Bin) string {
	if idr, ok := b.(interface{ ID() string }); ok {
		return idr.ID()
	}
	return ""
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
	Contact     ContactSpec      `json:"contact,omitempty"`
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
	// Inherit the outer level's bottom-support requirement so the physical
	// stacking rule is enforced inside cartons as well as on pallets.
	l0spec := req.Levels[0]
	l0Contact := l0spec.Contact
	if b := req.Levels[1].Contact.Bottom; b > l0Contact.Bottom {
		l0Contact.Bottom = b
	}
	if req.Levels[1].Contact.NoFloating {
		l0Contact.NoFloating = true
	}
	l0req := PackRequest{
		Mode:        req.Mode,
		Algorithm:   l0spec.Algorithm,
		Bin:         l0spec.Bin,
		Items:       req.Items,
		Constraints: l0spec.Constraints,
		Preferences: l0spec.Preferences,
		Contact:     l0Contact,
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
		Contact:     l1spec.Contact,
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
