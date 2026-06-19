package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/wfloyd/go-pack-bins/d1"
	"github.com/wfloyd/go-pack-bins/d2"
	"github.com/wfloyd/go-pack-bins/d3"
	"github.com/wfloyd/go-pack-bins/offline"
	"github.com/wfloyd/go-pack-bins/online"
	"github.com/wfloyd/go-pack-bins/pack"
)

func main() {
	http.Handle("/", http.FileServer(http.Dir("static")))
	http.HandleFunc("/api/pack", handlePack)
	log.Println("Listening on :8082")
	log.Fatal(http.ListenAndServe(":8082", nil))
}

// ─── request / response types ─────────────────────────────────────────────────

type ItemSpec struct {
	ID          string  `json:"id"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	Depth       float64 `json:"depth"`
	AllowRotate bool    `json:"allow_rotate"`
}

type BinSpec struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Depth  float64 `json:"depth"`
}

type PackRequest struct {
	Mode      string     `json:"mode"`      // "1d", "2d", "3d"
	Algorithm string     `json:"algorithm"` // "ff", "bf", "wf", "nf", "nkf", "awf", "ffd", "bfd", "nfd", "wfd", "mffd", "kk", "bc", "hk", "rff"
	Bin       BinSpec    `json:"bin"`
	Items     []ItemSpec `json:"items"`
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
	factory := d1.NewFactory(cap)

	items := make([]pack.Item, len(req.Items))
	for i, spec := range req.Items {
		items[i] = d1.NewItem(spec.ID, spec.Width)
	}

	var result pack.Result
	var err error

	switch req.Algorithm {
	case "nf":
		p := online.NextFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "nkf":
		p := online.NextKFit(3, factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "bf":
		p := online.BestFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "wf":
		p := online.WorstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "awf":
		p := online.AlmostWorstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "rff":
		p := online.NewRFF(cap, factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "hk":
		p := online.NewHarmonicK(11, cap, factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
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
		result, err = offline.BinCompletion(items, cap, factory)
	default:
		p := online.FirstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	}

	if err != nil {
		return PackResponse{Error: err.Error()}, nil
	}

	// Build a lookup from item ID → size for offset tracking.
	sizeByID := make(map[string]float64, len(req.Items))
	for _, spec := range req.Items {
		sizeByID[spec.ID] = spec.Width
	}
	return buildResponse1D(result, req.Bin.Width, sizeByID), nil
}

func buildResponse1D(result pack.Result, binWidth float64, sizeByID map[string]float64) PackResponse {
	resp := PackResponse{
		BinsUsed: result.BinsUsed(),
		Unplaced: result.Unplaced,
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
	factory := d2.NewFactory(bw, bh, makeStrat)

	items := make([]pack.Item, len(req.Items))
	for i, spec := range req.Items {
		items[i] = d2.NewItem(spec.ID, spec.Width, spec.Height, spec.AllowRotate)
	}

	var result pack.Result
	var err error

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
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "bf":
		p := online.BestFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "wf":
		p := online.WorstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	default: // ff, maxrects, guillotine
		p := online.FirstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	}

	if err != nil {
		return PackResponse{Error: err.Error()}, nil
	}

	return buildResponse2D(result, req.Algorithm == "guillotine"), nil
}

func buildResponse2D(result pack.Result, includeGuillotineFree bool) PackResponse {
	resp := PackResponse{
		BinsUsed: result.BinsUsed(),
		Unplaced: result.Unplaced,
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
	factory := d3.NewFactory(bw, bd, bh, d3.NewExtremePointStrategy)

	items := make([]pack.Item, len(req.Items))
	for i, spec := range req.Items {
		items[i] = d3.NewItem(spec.ID, spec.Width, spec.Depth, spec.Height, spec.AllowRotate)
	}

	var result pack.Result
	var err error

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
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "bf":
		p := online.BestFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	case "wf":
		p := online.WorstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	default: // ff
		p := online.FirstFit(factory)
		for _, it := range items {
			if _, e := p.Pack(it); e != nil {
				return PackResponse{Error: e.Error()}, nil
			}
		}
		result = p.Result()
	}

	if err != nil {
		return PackResponse{Error: err.Error()}, nil
	}

	return buildResponse3D(result), nil
}

func buildResponse3D(result pack.Result) PackResponse {
	resp := PackResponse{
		BinsUsed: result.BinsUsed(),
		Unplaced: result.Unplaced,
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

func binIndexFromID(bins []pack.Bin, id string) int {
	for i, b := range bins {
		type idder interface{ ID() string }
		if b2, ok := b.(idder); ok && b2.ID() == id {
			return i
		}
	}
	return 0
}
