package packapi

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
)

// zoneReq: a bin with a pipe crossing it mid-air, and items that would
// otherwise pack straight through it.
func zoneReq(algo string, withZone bool) PackRequest {
	req := PackRequest{
		Mode: "3d", Algorithm: algo,
		Bin: BinSpec{Width: 6, Depth: 6, Height: 6},
	}
	for i := 0; i < 6; i++ {
		req.Items = append(req.Items, ItemSpec{
			ID: "b" + string(rune('1'+i)), Width: 3, Depth: 3, Height: 2,
		})
	}
	if withZone {
		// A pipe running the full width at mid-height.
		req.Zones = []ZoneSpec{{X: 0, Y: 0, Z: 2, W: 6, D: 2, H: 2}}
	}
	return req
}

// No placement may intrude on a zone, for every algorithm that claims to
// enforce them.
func TestZonesRespectedByEveryAdvertisedAlgorithm(t *testing.T) {
	for algo := range zoneAlgos3D {
		t.Run(algo, func(t *testing.T) {
			req := zoneReq(algo, true)
			resp := PackCtx(context.Background(), req)
			if resp.Error != "" {
				t.Fatalf("refused: %s", resp.Error)
			}
			ps := make([]*d3.Placement3D, 0, len(resp.Placements))
			for _, p := range resp.Placements {
				ps = append(ps, d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
			}
			if !d3.ZonesOK(ps, req.zones()) {
				t.Errorf("%s placed an item inside the keep-out zone", algo)
			}
		})
	}
}

// The zone must actually bite: without it, something occupies that region.
func TestZonesAreNotVacuous(t *testing.T) {
	req := zoneReq("ffd", false)
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("solve: %s", resp.Error)
	}
	zones := zoneReq("ffd", true).zones()
	ps := make([]*d3.Placement3D, 0, len(resp.Placements))
	for _, p := range resp.Placements {
		ps = append(ps, d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
	}
	if d3.ZonesOK(ps, zones) {
		t.Fatal("nothing occupies the zone region without the zone, so the test proves nothing")
	}
}

// Algorithms that cannot enforce zones refuse the request rather than packing
// through the keep-out region.
func TestZonesRejectedWhereUnsupported(t *testing.T) {
	for _, algo := range []string{"laff", "blocks", "columns", "layer", "assemble"} {
		t.Run(algo, func(t *testing.T) {
			resp := PackCtx(context.Background(), zoneReq(algo, true))
			if !strings.Contains(resp.Error, "does not enforce exclusion zones") {
				t.Errorf("error = %q, want a refusal", resp.Error)
			}
		})
	}
}

func TestZonesRejectedOutside3D(t *testing.T) {
	req := zoneReq("ffd", true)
	req.Mode = "2d"
	if resp := PackCtx(context.Background(), req); !strings.Contains(resp.Error, "3-D only") {
		t.Errorf("error = %q", resp.Error)
	}
}

// A malformed or bin-filling zone is refused rather than silently packing
// nothing.
func TestZonesRejectedWhenDegenerate(t *testing.T) {
	tests := []struct {
		name string
		zone ZoneSpec
		want string
	}{
		{"no volume", ZoneSpec{X: 0, Y: 0, Z: 0, W: 0, D: 2, H: 2}, "no volume"},
		{"fills the bin", ZoneSpec{X: 0, Y: 0, Z: 0, W: 6, D: 6, H: 6}, "no usable space"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := zoneReq("ffd", false)
			req.Zones = []ZoneSpec{tt.zone}
			if resp := PackCtx(context.Background(), req); !strings.Contains(resp.Error, tt.want) {
				t.Errorf("error = %q, want it to mention %q", resp.Error, tt.want)
			}
		})
	}
}

// Streaming enforces zones identically — it builds its own factory, the hole
// the bearing work had to close twice.
func TestZonesEnforcedWhenStreaming(t *testing.T) {
	req := zoneReq("ffd", true)
	if !isStreamable(req) {
		t.Skip("ffd does not stream here")
	}
	var placements []PlacementResult
	var streamErr string
	StreamPack(context.Background(), req, func(f StreamFrame) {
		switch f.Type {
		case "batch":
			placements = append(placements, f.Placements...)
		case "reposition":
			placements = f.Placements
		case "error":
			streamErr = f.Error
		}
	})
	if streamErr != "" {
		t.Fatalf("stream: %s", streamErr)
	}
	ps := make([]*d3.Placement3D, 0, len(placements))
	for _, p := range placements {
		ps = append(ps, d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
	}
	if !d3.ZonesOK(ps, req.zones()) {
		t.Error("a streamed placement intrudes on the keep-out zone")
	}
}

// Relocation post-passes must not slide an item into a zone.
func TestZonesSurviveRelocation(t *testing.T) {
	req := zoneReq("ffd", true)
	req.Contact = ContactSpec{SideX: 1, SideY: 1} // force lateral compaction
	req.RefineVoids = true                        // and the void refiner
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("solve: %s", resp.Error)
	}
	ps := make([]*d3.Placement3D, 0, len(resp.Placements))
	for _, p := range resp.Placements {
		ps = append(ps, d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
	}
	if !d3.ZonesOK(ps, req.zones()) {
		t.Error("a post-pass moved an item into the keep-out zone")
	}
}

// The advertised capability must match the enforced set, like the bearing flag.
func TestZoneCapabilityMatchesEnforcement(t *testing.T) {
	caps := AlgoCapabilities()
	for algo, c := range caps.Algos {
		if c.Zones != zoneAlgos3D[algo] {
			t.Errorf("%s: advertised zones=%v, enforced=%v", algo, c.Zones, zoneAlgos3D[algo])
		}
	}
	for algo := range zoneAlgos3D {
		if !caps.Algos[algo].Zones {
			t.Errorf("%s enforces zones but is not advertised", algo)
		}
	}
}

// Zones off must leave packing untouched.
func TestZonesDisabledIsUnchanged(t *testing.T) {
	a := PackCtx(context.Background(), zoneReq("ffd", false))
	b := PackCtx(context.Background(), zoneReq("ffd", false))
	if a.BinsUsed != b.BinsUsed || len(a.Placements) != len(b.Placements) {
		t.Fatalf("non-deterministic baseline")
	}
	for i := range a.Placements {
		if a.Placements[i] != b.Placements[i] {
			t.Errorf("placement %d differs", i)
		}
	}
}

// The keep-out preset must actually be obstructed: without the zones, items
// occupy the pipe and the doorway. A preset that packs identically either way
// shows nothing.
func TestZonePresetDemonstrates(t *testing.T) {
	const label = "Basement: keep the aisle clear, pack under the pipe"
	var p *Preset
	for _, c := range Presets()["3d"] {
		if c.Label == label {
			cc := c
			p = &cc
		}
	}
	if p == nil {
		t.Fatalf("preset %q is missing", label)
	}
	if len(p.Zones) < 2 {
		t.Fatalf("preset declares %d zones, want at least 2", len(p.Zones))
	}

	build := func(withZones bool) PackRequest {
		req := PackRequest{Mode: "3d", Algorithm: p.Algo,
			Bin: BinSpec{Width: p.Bin.W, Depth: p.Bin.D, Height: p.Bin.H}}
		for i, it := range p.Items {
			req.Items = append(req.Items, ItemSpec{ID: fmt.Sprintf("i%d", i),
				Width: it.W, Depth: it.D, Height: it.H})
		}
		if withZones {
			req.Zones = p.Zones
		}
		return req
	}
	placements := func(r PackResponse) []*d3.Placement3D {
		out := make([]*d3.Placement3D, 0, len(r.Placements))
		for _, q := range r.Placements {
			out = append(out, d3.NewPlacement3D("", q.ItemID, q.X, q.Y, q.Z, q.W, q.D, q.H))
		}
		return out
	}

	off := PackCtx(context.Background(), build(false))
	on := PackCtx(context.Background(), build(true))
	if off.Error != "" || on.Error != "" {
		t.Fatalf("errors: off=%q on=%q", off.Error, on.Error)
	}
	zs := build(true).zones()
	if d3.ZonesOK(placements(off), zs) {
		t.Error("without the zones nothing occupies them, so the preset demonstrates nothing")
	}
	if !d3.ZonesOK(placements(on), zs) {
		t.Error("the preset packs into its own keep-out zones")
	}
	if len(on.Placements) != len(p.Items) {
		t.Errorf("placed %d of %d items — the zones should divert, not defeat, the pack",
			len(on.Placements), len(p.Items))
	}

	// Every zone must actually be in the way, measured by how much cargo the
	// unzoned packing puts inside it. An earlier version checked only that the
	// zones were clear afterwards, which a zone floating above the packing
	// satisfies trivially — the first preset had a pipe the items never reached.
	overlapVol := func(ps []PlacementResult, z ZoneSpec) float64 {
		var v float64
		for _, q := range ps {
			ow := math.Min(q.X+q.W, z.X+z.W) - math.Max(q.X, z.X)
			od := math.Min(q.Y+q.D, z.Y+z.D) - math.Max(q.Y, z.Y)
			oh := math.Min(q.Z+q.H, z.Z+z.H) - math.Max(q.Z, z.Z)
			if ow > 0 && od > 0 && oh > 0 {
				v += ow * od * oh
			}
		}
		return v
	}
	var displaced, packed float64
	for _, q := range off.Placements {
		packed += q.W * q.D * q.H
	}
	for i, z := range p.Zones {
		v := overlapVol(off.Placements, z)
		if v <= 0 {
			t.Errorf("zone %d holds no cargo in the unzoned packing, so it obstructs nothing", i)
		}
		displaced += v
	}
	// And the displacement must be big enough to see, not a sliver.
	if packed > 0 && displaced/packed < 0.1 {
		t.Errorf("the zones displace only %.1f%% of the packed volume — too little to be visible",
			100*displaced/packed)
	}
	t.Logf("zones displace %.0f of %.0f packed volume (%.0f%%)", displaced, packed, 100*displaced/packed)
}

// A zone covering the origin must not brick the bin. An empty bin's only
// candidate position is the origin, so unless zones seed candidates of their own
// a zone there leaves nowhere to place anything — the first version of this
// feature placed zero of ten items.
func TestZoneOverOriginStillPacks(t *testing.T) {
	for algo := range zoneAlgos3D {
		t.Run(algo, func(t *testing.T) {
			req := PackRequest{Mode: "3d", Algorithm: algo,
				Bin: BinSpec{Width: 9, Depth: 9, Height: 9},
				// A column at the origin, floor to ceiling.
				Zones: []ZoneSpec{{X: 0, Y: 0, Z: 0, W: 3, D: 3, H: 9}},
			}
			for i := 0; i < 4; i++ {
				req.Items = append(req.Items, ItemSpec{
					ID: "b" + string(rune('1'+i)), Width: 3, Depth: 3, Height: 3})
			}
			resp := PackCtx(context.Background(), req)
			if resp.Error != "" {
				t.Fatalf("refused: %s", resp.Error)
			}
			if len(resp.Placements) != len(req.Items) {
				t.Errorf("%s placed %d of %d items with a zone over the origin",
					algo, len(resp.Placements), len(req.Items))
			}
			ps := make([]*d3.Placement3D, 0, len(resp.Placements))
			for _, q := range resp.Placements {
				ps = append(ps, d3.NewPlacement3D("", q.ItemID, q.X, q.Y, q.Z, q.W, q.D, q.H))
			}
			if !d3.ZonesOK(ps, req.zones()) {
				t.Errorf("%s placed into the zone", algo)
			}
		})
	}
}

// auto must not win its race with a packer that ignores the zones. The plan set
// dropped Fit and Layer only when *bearing* was enabled, and the self-managing
// packers likewise — so with zones on and bearing off, auto raced LayerStack
// (which delegates each layer to a 2-D bin and sees no zones), it won on bin
// count, and the result was returned as success with twelve intrusions.
func TestZonesAutoExcludesUnenforcingCandidates(t *testing.T) {
	req := zoneReq("auto", true)
	if autoSelfManaged3D(req) {
		t.Error("auto would race blocks/assemble/LAFF with zones on; none of them enforce zones")
	}
	for _, pl := range auto3DPlans(req) {
		if pl.strat == "layer" {
			t.Errorf("auto races %q, whose LayerStack cannot enforce zones", pl.label)
		}
	}

	// And the packing auto actually returns must honour them.
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("refused: %s", resp.Error)
	}
	ps := make([]*d3.Placement3D, 0, len(resp.Placements))
	for _, p := range resp.Placements {
		ps = append(ps, d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
	}
	if !d3.ZonesOK(ps, req.zones()) {
		t.Errorf("auto returned a zone-violating packing (winner %q)", resp.BestPacker)
	}
	if len(resp.Placements) != len(req.Items) {
		t.Errorf("placed %d of %d", len(resp.Placements), len(req.Items))
	}
}

// Fit stays in the race: its maximal-space strategy does enforce zones, even
// though it has no bearing gate. Dropping it would cost packing quality for no
// safety gain.
func TestZonesAutoKeepsFit(t *testing.T) {
	var found bool
	for _, pl := range auto3DPlans(zoneReq("auto", true)) {
		if pl.strat == "fit" {
			found = true
		}
	}
	if !found {
		t.Error("Fit dropped from the zoned auto race though it enforces zones")
	}
}

// The basement preset is the one that has to look like a real store: shelving
// runs you can walk between, boxes resting on the boards rather than stacked in
// a heap, and nothing left standing in an aisle.
func TestBasementShelvingPreset(t *testing.T) {
	const label = "Basement: shelving runs with walking aisles"
	var p *Preset
	for _, c := range Presets()["3d"] {
		if c.Label == label {
			cc := c
			p = &cc
		}
	}
	if p == nil {
		t.Fatalf("preset %q is missing", label)
	}

	req := PackRequest{Mode: "3d", Algorithm: p.Algo,
		Bin:     BinSpec{Width: p.Bin.W, Depth: p.Bin.D, Height: p.Bin.H},
		Zones:   p.Zones,
		Contact: ContactSpec{NoFloating: true}} // boxes must rest on something
	for i, it := range p.Items {
		req.Items = append(req.Items, ItemSpec{ID: fmt.Sprintf("i%d", i),
			Width: it.W, Depth: it.D, Height: it.H})
	}

	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("solve: %s", resp.Error)
	}
	if len(resp.Placements) != len(p.Items) {
		t.Errorf("placed %d of %d", len(resp.Placements), len(p.Items))
	}
	if resp.BinsUsed != 1 {
		t.Errorf("used %d rooms; the layout should hold the order in one", resp.BinsUsed)
	}

	// Aisles must stay walkable — an aisle with boxes standing in it is not an
	// aisle, and is the whole thing the preset is meant to show.
	var aisles []ZoneSpec
	var boards []ZoneSpec
	for _, z := range p.Zones {
		switch {
		case z.Permeable:
			aisles = append(aisles, z)
		case z.Supports:
			boards = append(boards, z)
		}
	}
	if len(aisles) < 2 || len(boards) < 4 {
		t.Fatalf("preset shape changed: %d aisles, %d shelf boards", len(aisles), len(boards))
	}
	for _, q := range resp.Placements {
		for _, a := range aisles {
			if q.Y >= a.Y && q.Y < a.Y+a.D {
				t.Errorf("%s is standing in a walking aisle at y=%v", q.ItemID, q.Y)
			}
		}
	}

	// And the shelves must be carrying boxes, not just be scenery: something has
	// to rest on each board's top face.
	used := 0
	for _, b := range boards {
		for _, q := range resp.Placements {
			if math.Abs(q.Z-(b.Z+b.H)) < 1e-9 {
				used++
				break
			}
		}
	}
	if used < len(boards) {
		t.Errorf("only %d of %d shelf boards carry anything; the rest are decoration", used, len(boards))
	}
}
