package packapi

import (
	"context"
	"strings"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
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

// A carton's weight at the pallet level is its contents plus its declared tare,
// and its limit is its own structural rating rather than anything its contents
// can bear — the gate reads those separately.
func TestNestedCartonScalars(t *testing.T) {
	l1 := BearingSpec{WeightScalar: "weight", LimitScalar: "bearlimit", DefaultUnlimited: true}
	it := ItemSpec{ID: "carton_0", Width: 4, Depth: 4, Height: 2,
		Scalars: map[string]float64{"weight": 10}} // summed from contents
	applyCartonBearing(&it, &ContainerBearingSpec{Tare: 5, Limit: 100}, l1)

	if got := it.Scalars["weight"]; got != 15 {
		t.Errorf("carton weight = %v, want 15 (10 contents + 5 tare)", got)
	}
	if got := it.Scalars["bearlimit"]; got != 100 {
		t.Errorf("carton limit = %v, want its own rating of 100", got)
	}

	// With no declared rating the carton's own structure is unrestricted, so the
	// load path through its contents is what binds.
	bare := ItemSpec{ID: "carton_0", Scalars: map[string]float64{"weight": 10}}
	applyCartonBearing(&bare, nil, l1)
	if got := bare.Scalars["bearlimit"]; got != d3.NoLimit {
		t.Errorf("undeclared carton limit = %v, want NoLimit", got)
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

// The level-1 gate must resolve load paths through a carton's contents at
// placement time, not work from a collapsed number and leave the validator to
// clean up. The difference shows where the collapsed limit and the real load
// path disagree: a carton whose contents reach its lid only over part of its
// footprint. The effective limit sums those contents' limits as though load
// spread across them, while the real rule charges the uncovered part to the
// carton's own weak structure.
func TestNestedGateResolvesContentsAtPlacement(t *testing.T) {
	// Carton 4x4x2. Its single content is a strong 1x1 post reaching the lid, so
	// only a sixteenth of the lid transmits; the rest rests on the carton.
	inner := BearingSpec{WeightScalar: "weight", LimitScalar: "bearlimit", DefaultUnlimited: true}
	l0 := PackResponse{
		BinsUsed: 1,
		Placements: []PlacementResult{
			{BinIndex: 0, ItemID: "post", X: 0, Y: 0, Z: 0, W: 1, D: 1, H: 2},
		},
	}
	scalars := map[string]map[string]float64{
		"post": {"weight": 1, "bearlimit": 500},
	}
	lookup := cartonContentsFn(l0, inner.toD3(), scalars, &ContainerBearingSpec{Limit: 1})

	contents, rigid := lookup("carton_0")
	if rigid {
		t.Error("carton reported rigid without the flag")
	}
	if len(contents) != 1 {
		t.Fatalf("lookup returned %d contents, want 1", len(contents))
	}
	// Positions must come back in the carton's own frame — the gate translates
	// them once it knows where the carton lands.
	if contents[0].X != 0 || contents[0].Z != 0 {
		t.Errorf("contents not in the carton's local frame: %+v", contents[0])
	}

	// Build the carton at a pallet position and load its whole lid. The strong
	// post takes its sixteenth; the weak carton must refuse the rest.
	carton := d3.BearBox{
		X: 2, Y: 3, Z: 0, W: 4, D: 4, H: 2,
		Weight: 1, Limit: 1, PressureLimit: d3.NoLimit,
	}
	carton.Contents = append([]d3.BearBox(nil), contents...)
	for i := range carton.Contents {
		carton.Contents[i].X += carton.X
		carton.Contents[i].Y += carton.Y
		carton.Contents[i].Z += carton.Z
	}
	load := d3.BearBox{X: 2, Y: 3, Z: 2, W: 4, D: 4, H: 1,
		Weight: 160, Limit: d3.NoLimit, PressureLimit: d3.NoLimit}

	if d3.LoadPathOK([]d3.BearBox{carton, load}) {
		t.Error("a weak carton accepted the fifteen-sixteenths of the load resting on its own lid")
	}
	// The carton item handed to the level-1 solve must carry its *own* structural
	// rating, not anything its contents can bear: the gate reads the contents
	// separately, so a limit describing them would rate the lid for load that
	// never touches it.
	it := ItemSpec{ID: "carton_0", Width: 4, Depth: 4, Height: 2, Scalars: map[string]float64{"weight": 1}}
	applyCartonBearing(&it, &ContainerBearingSpec{Limit: 1}, inner)
	if it.Scalars["bearlimit"] != 1 {
		t.Errorf("carton limit = %v, want 1 (its own rating, not its contents')",
			it.Scalars["bearlimit"])
	}
}

// End to end: with the gate contents-aware, a nested solve that the collapsed
// approximation would have mis-placed comes back consistent — the validator
// finds nothing left to reject.
func TestNestedGateAndValidatorAgree(t *testing.T) {
	items := []ItemSpec{
		{ID: "post1", Width: 4, Depth: 4, Height: 2,
			Scalars: map[string]float64{"weight": 2, "bearlimit": 300}},
		{ID: "post2", Width: 4, Depth: 4, Height: 2,
			Scalars: map[string]float64{"weight": 2, "bearlimit": 300}},
		{ID: "post3", Width: 4, Depth: 4, Height: 2,
			Scalars: map[string]float64{"weight": 2, "bearlimit": 300}},
	}
	req := nestedBearReq(items, &ContainerBearingSpec{Limit: 2})
	resp, err := PackNestedCtx(context.Background(), req)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("solve: %s", resp.Error)
	}
	if got := len(resp.Levels[0].Placements); got != len(items) {
		t.Errorf("placed %d of %d goods", got, len(items))
	}
	// The gate placed it; the exact rule must agree.
	if msg := nestedBearingError(levelResp(resp, 0), levelResp(resp, 1), req); msg != "" {
		t.Errorf("gate and validator disagree: %s", msg)
	}
}

// Nested solves reach pack3D through packByMode rather than dispatch, so the
// bearing checks that guard a single-container solve have to run for each level
// too. Without this a level using an algorithm that cannot enforce the gate
// packed without it and said nothing.
func TestNestedRejectsUnenforcingAlgorithm(t *testing.T) {
	items := []ItemSpec{
		{ID: "fragile", Width: 4, Depth: 4, Height: 1,
			Scalars: map[string]float64{"weight": 1, "bearlimit": 0}},
		{ID: "heavy", Width: 4, Depth: 4, Height: 1,
			Scalars: map[string]float64{"weight": 50}},
	}
	for _, tc := range []struct {
		level int
		name  string
	}{
		{0, "inner (carton)"},
		{1, "outer (pallet)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := nestedBearReq(items, nil)
			req.Levels[tc.level].Algorithm = "laff"
			resp, err := PackNestedCtx(context.Background(), req)
			if err != nil {
				t.Fatalf("pack: %v", err)
			}
			if !strings.Contains(resp.Error, "does not enforce load-bearing") {
				t.Errorf("error = %q, want a refusal", resp.Error)
			}
			if !strings.Contains(resp.Error, tc.name) {
				t.Errorf("error = %q, want it to name the level %q", resp.Error, tc.name)
			}
		})
	}
}

// A mis-typed scalar name must be refused at the level that packs the items.
func TestNestedRejectsUnusedScalarName(t *testing.T) {
	req := nestedBearReq([]ItemSpec{
		{ID: "g", Width: 4, Depth: 4, Height: 1, Scalars: map[string]float64{"weight": 1}},
	}, nil)
	req.Levels[0].Bearing.WeightScalar = "mass"
	resp, err := PackNestedCtx(context.Background(), req)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if !strings.Contains(resp.Error, `no item has a "mass" scalar`) {
		t.Errorf("error = %q", resp.Error)
	}
}
