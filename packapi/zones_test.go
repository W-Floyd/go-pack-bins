package packapi

import (
	"context"
	"fmt"
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
	const label = "Basement: pipe overhead, doorway clear (keep-out zones)"
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
