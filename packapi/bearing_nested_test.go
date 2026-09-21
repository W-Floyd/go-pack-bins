package packapi

import (
	"context"
	"strings"
	"testing"
)

// nestedBearReq builds a two-level request: goods into cartons, cartons onto a
// pallet, with bearing on at both levels.
func nestedBearReq(items []ItemSpec, carton *ContainerBearingSpec) NestedPackRequest {
	bear := BearingSpec{WeightScalar: "weight", LimitScalar: "bearlimit", DefaultUnlimited: true}
	return NestedPackRequest{
		Mode:  "3d",
		Items: items,
		Levels: []NestedLevelSpec{
			{Bin: BinSpec{Width: 4, Depth: 4, Height: 2}, Algorithm: "ffd", Bearing: bear},
			{Bin: BinSpec{Width: 8, Depth: 8, Height: 8}, Algorithm: "ffd", Bearing: bear,
				ContainerBearing: carton},
		},
	}
}

// Goods inside a carton must honour their own limits — the level-0 solve is
// gated like any other 3-D solve.
func TestNestedBearingGatesInsideCartons(t *testing.T) {
	items := []ItemSpec{
		{ID: "fragile", Width: 4, Depth: 4, Height: 1,
			Scalars: map[string]float64{"weight": 1, "bearlimit": 0}},
		{ID: "heavy", Width: 4, Depth: 4, Height: 1,
			Scalars: map[string]float64{"weight": 50}},
	}
	resp, err := PackNestedCtx(context.Background(), nestedBearReq(items, nil))
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("solve: %s", resp.Error)
	}
	// Both are 4x4x1 in a 4x4x2 carton: ungated they share one carton, stacked.
	// Gated, the heavy one may not sit on the fragile one.
	inner := resp.Levels[0]
	byID := map[string]PlacementResult{}
	for _, p := range inner.Placements {
		byID[p.ItemID] = p
	}
	f, h := byID["fragile"], byID["heavy"]
	if f.BinIndex == h.BinIndex && h.Z > f.Z {
		t.Errorf("heavy item stacked on the fragile one inside carton %d", f.BinIndex)
	}
}

// A carton's weight at the pallet level is its contents plus its declared tare.
func TestNestedCartonTareCounts(t *testing.T) {
	items := []ItemSpec{
		{ID: "g1", Width: 4, Depth: 4, Height: 2, Scalars: map[string]float64{"weight": 10}},
	}
	withTare := nestedBearReq(items, &ContainerBearingSpec{Tare: 5, Limit: 100})
	tree := func(req NestedPackRequest) float64 {
		l0, _ := packByMode(context.Background(), PackRequest{
			Mode: "3d", Algorithm: "ffd", Bin: req.Levels[0].Bin, Items: req.Items,
			Bearing: req.Levels[0].Bearing,
		})
		var total float64
		for b := 0; b < l0.BinsUsed; b++ {
			it := ItemSpec{ID: "carton_0", Width: 4, Depth: 4, Height: 2,
				Scalars: map[string]float64{"weight": 10}}
			applyCartonBearing(&it, l0.Placements, req.Levels[0].Bearing.toD3(),
				req.Levels[1].ContainerBearing, req.Levels[1].Bearing,
				map[string]map[string]float64{"g1": items[0].Scalars})
			total = it.Scalars["weight"]
		}
		return total
	}
	if got := tree(withTare); got != 15 {
		t.Errorf("carton weight = %v, want 15 (10 contents + 5 tare)", got)
	}
}

// The headline case, end to end: a carton whose contents reach its lid carries
// load through them, so a carton rated for almost nothing on its own structure
// is still fine under a heavy pallet load.
func TestNestedLoadPathThroughCartonContents(t *testing.T) {
	// Goods that exactly fill the carton's height, so they reach the lid.
	items := []ItemSpec{
		{ID: "post1", Width: 4, Depth: 4, Height: 2,
			Scalars: map[string]float64{"weight": 1, "bearlimit": 500}},
		{ID: "post2", Width: 4, Depth: 4, Height: 2,
			Scalars: map[string]float64{"weight": 1, "bearlimit": 500}},
	}
	// The carton's own lid is rated for almost nothing.
	weak := &ContainerBearingSpec{Limit: 1}

	resp, err := PackNestedCtx(context.Background(), nestedBearReq(items, weak))
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if resp.Error != "" {
		t.Errorf("load did not transfer through the carton contents: %s", resp.Error)
	}
}

// When the contents cannot bear the load either, the exact validator rejects the
// packing rather than letting it through.
func TestNestedLoadPathRejectsCrushedContents(t *testing.T) {
	items := []ItemSpec{
		{ID: "weak1", Width: 4, Depth: 4, Height: 2,
			Scalars: map[string]float64{"weight": 40, "bearlimit": 0}},
		{ID: "weak2", Width: 4, Depth: 4, Height: 2,
			Scalars: map[string]float64{"weight": 40, "bearlimit": 0}},
	}
	req := nestedBearReq(items, &ContainerBearingSpec{Limit: 1})
	resp, err := PackNestedCtx(context.Background(), req)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	// Either the pallet solve refused to stack, or the validator caught it.
	if resp.Error != "" {
		if !strings.Contains(resp.Error, "bearing") {
			t.Errorf("unexpected error: %s", resp.Error)
		}
		return
	}
	// If it did pack, the load-path check must hold.
	if msg := nestedBearingError(
		levelResp(resp, 0), levelResp(resp, 1), req); msg != "" {
		t.Errorf("packing violates the load-path rule but was returned: %s", msg)
	}
}

// levelResp reconstructs a PackResponse view of one nested level for validation.
func levelResp(r NestedPackResponse, i int) PackResponse {
	return PackResponse{
		BinsUsed:   r.Levels[i].BinsUsed,
		Placements: r.Levels[i].Placements,
	}
}

// Bearing off at both levels must leave nested packing exactly as it was.
func TestNestedUnchangedWithoutBearing(t *testing.T) {
	items := []ItemSpec{
		{ID: "a", Width: 4, Depth: 4, Height: 1, Scalars: map[string]float64{"weight": 3}},
		{ID: "b", Width: 4, Depth: 4, Height: 1, Scalars: map[string]float64{"weight": 3}},
	}
	req := nestedBearReq(items, nil)
	req.Levels[0].Bearing = BearingSpec{}
	req.Levels[1].Bearing = BearingSpec{}

	resp, err := PackNestedCtx(context.Background(), req)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("solve: %s", resp.Error)
	}
	if len(resp.Levels[0].Placements) != len(items) {
		t.Errorf("placed %d of %d goods", len(resp.Levels[0].Placements), len(items))
	}
}

// The validator must be able to reject, not just pass things through. A pallet
// load that crushes a carton's contents has to be caught even though the
// level-1 solve, working from one collapsed number per carton, let it stand.
func TestNestedBearingValidatorRejects(t *testing.T) {
	req := nestedBearReq(nil, &ContainerBearingSpec{Limit: 1000})
	req.Items = []ItemSpec{
		{ID: "weak", Width: 4, Depth: 4, Height: 2,
			Scalars: map[string]float64{"weight": 1, "bearlimit": 2}},
		{ID: "heavy", Width: 4, Depth: 4, Height: 2,
			Scalars: map[string]float64{"weight": 50}},
	}

	// Carton 0 holds the weak item, flush to its lid. Carton 1 holds the heavy
	// one and sits on carton 0, so the load runs through carton 0's lid into the
	// weak item — which cannot take it.
	l0 := PackResponse{
		BinsUsed: 2,
		Placements: []PlacementResult{
			{BinIndex: 0, ItemID: "weak", X: 0, Y: 0, Z: 0, W: 4, D: 4, H: 2},
			{BinIndex: 1, ItemID: "heavy", X: 0, Y: 0, Z: 0, W: 4, D: 4, H: 2},
		},
	}
	l1 := PackResponse{
		BinsUsed: 1,
		Placements: []PlacementResult{
			{BinIndex: 0, ItemID: "carton_0", X: 0, Y: 0, Z: 0, W: 4, D: 4, H: 2},
			{BinIndex: 0, ItemID: "carton_1", X: 0, Y: 0, Z: 2, W: 4, D: 4, H: 2},
		},
	}
	if msg := nestedBearingError(l0, l1, req); msg == "" {
		t.Error("a pallet stack crushing a carton's contents was accepted")
	} else if !strings.Contains(msg, "load paths") {
		t.Errorf("error = %q, want it to mention load paths", msg)
	}

	// Raise the item's limit above the load and the same packing is fine.
	req.Items[0].Scalars["bearlimit"] = 1000
	if msg := nestedBearingError(l0, l1, req); msg != "" {
		t.Errorf("a stack within every limit was rejected: %s", msg)
	}
}
