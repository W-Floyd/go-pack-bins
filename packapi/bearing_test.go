package packapi

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func bearingReq(algo string) PackRequest {
	return PackRequest{
		Mode:      "3d",
		Algorithm: algo,
		Bin:       BinSpec{Width: 1, Depth: 1, Height: 4},
		Bearing: BearingSpec{
			WeightScalar:     "weight",
			LimitScalar:      "bearlimit",
			DefaultUnlimited: true,
		},
		Items: []ItemSpec{
			// A fragile base, then a heavy item that can only go on top of it.
			{ID: "fragile", Width: 1, Depth: 1, Height: 1,
				Scalars: map[string]float64{"weight": 1, "bearlimit": 0}},
			{ID: "heavy", Width: 1, Depth: 1, Height: 1,
				Scalars: map[string]float64{"weight": 100}},
		},
	}
}

// Every algorithm advertised as bearing-capable must actually refuse to bury a
// fragile item, and every algorithm not advertised must refuse the request
// outright rather than pack without the constraint. This is the drift test that
// keeps bearingAlgos3D honest, in the spirit of TestRegistryMatchesCapabilities.
func TestBearingAlgosEnforce(t *testing.T) {
	caps := AlgoCapabilities()
	var algos []string
	for _, a := range caps.Modes["3d"] {
		algos = append(algos, a.ID)
	}
	if len(algos) == 0 {
		t.Fatal("no 3-D algorithms advertised")
	}

	for _, algo := range algos {
		t.Run(algo, func(t *testing.T) {
			resp := PackCtx(context.Background(), bearingReq(algo))

			if !bearingAlgos3D[algo] {
				if !strings.HasPrefix(resp.Error, "bearing:") {
					t.Errorf("%s does not enforce bearing but accepted the request (error %q); "+
						"either gate it or leave it out of bearingAlgos3D", algo, resp.Error)
				}
				return
			}

			if resp.Error != "" {
				t.Fatalf("%s is advertised as bearing-capable but errored: %s", algo, resp.Error)
			}
			// The heavy item must not sit on the fragile one. Either it went to a
			// second bin or it was left unplaced; both are safe, stacking is not.
			var fragile, heavy *PlacementResult
			for i := range resp.Placements {
				switch resp.Placements[i].ItemID {
				case "fragile":
					fragile = &resp.Placements[i]
				case "heavy":
					heavy = &resp.Placements[i]
				}
			}
			if fragile == nil || heavy == nil {
				return // one was not placed: nothing was crushed
			}
			if fragile.BinIndex == heavy.BinIndex && heavy.Z > fragile.Z {
				t.Errorf("%s stacked the heavy item (z=%v) on the fragile one (z=%v) in bin %d",
					algo, heavy.Z, fragile.Z, heavy.BinIndex)
			}
		})
	}
}

// A bearing request must be refused outside 3-D and in catalog mode, where no
// solve path enforces it.
func TestBearingRejectedWhereUnsupported(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*PackRequest)
		want string
	}{
		{
			name: "2d mode",
			mut:  func(r *PackRequest) { r.Mode = "2d" },
			want: "3-D only",
		},
		{
			name: "1d mode",
			mut:  func(r *PackRequest) { r.Mode = "1d" },
			want: "3-D only",
		},
		{
			name: "catalog mode with a non-enforcing algorithm",
			mut: func(r *PackRequest) {
				r.Algorithm = "gbpp"
				r.Containers = []ContainerSpec{{Bin: BinSpec{Width: 1, Depth: 1, Height: 4}}}
			},
			want: "does not enforce load-bearing",
		},
		{
			name: "self-managed algorithm",
			mut:  func(r *PackRequest) { r.Algorithm = "laff" },
			want: "does not enforce load-bearing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := bearingReq("ffd")
			tt.mut(&req)
			resp := PackCtx(context.Background(), req)
			if !strings.Contains(resp.Error, tt.want) {
				t.Errorf("error = %q, want it to mention %q", resp.Error, tt.want)
			}
		})
	}
}

// Streaming must refuse the same requests the unary path does, before emitting
// any placement frames.
func TestBearingRejectedWhenStreaming(t *testing.T) {
	req := bearingReq("laff")
	var frames []StreamFrame
	StreamPack(context.Background(), req, func(f StreamFrame) { frames = append(frames, f) })

	if len(frames) != 1 || frames[0].Type != "error" {
		t.Fatalf("want a single error frame, got %d frames (first %+v)", len(frames), frames[0])
	}
	if !strings.Contains(frames[0].Error, "does not enforce load-bearing") {
		t.Errorf("error = %q", frames[0].Error)
	}
}

// Bearing off must leave results untouched, so the feature costs nothing when
// unused.
func TestBearingDisabledMatchesPlainSolve(t *testing.T) {
	for algo := range bearingAlgos3D {
		t.Run(algo, func(t *testing.T) {
			plain := bearingReq(algo)
			plain.Bearing = BearingSpec{}
			plain.Bin = BinSpec{Width: 4, Depth: 4, Height: 4}
			plain.Items = manyItems()

			off := plain // identical; asserts the zero spec is inert end to end
			a := PackCtx(context.Background(), plain)
			b := PackCtx(context.Background(), off)
			if a.BinsUsed != b.BinsUsed || len(a.Placements) != len(b.Placements) {
				t.Fatalf("bins %d vs %d, placements %d vs %d",
					a.BinsUsed, b.BinsUsed, len(a.Placements), len(b.Placements))
			}
			for i := range a.Placements {
				if a.Placements[i] != b.Placements[i] {
					t.Errorf("placement %d differs", i)
				}
			}
		})
	}
}

func manyItems() []ItemSpec {
	var out []ItemSpec
	for i, d := range [][3]float64{
		{2, 1, 1}, {1, 2, 1}, {1, 1, 2}, {2, 2, 1}, {1, 1, 1}, {3, 1, 1},
	} {
		out = append(out, ItemSpec{
			ID: string(rune('a' + i)), Width: d[0], Depth: d[1], Height: d[2],
			Scalars: map[string]float64{"weight": float64(i + 1)},
		})
	}
	return out
}

// The whole result of a bearing solve must satisfy the rule, post-passes
// included — the constructive gate alone does not guarantee this.
func TestBearingResultValidatesAfterPostPasses(t *testing.T) {
	for algo := range bearingAlgos3D {
		t.Run(algo, func(t *testing.T) {
			req := bearingReq(algo)
			req.Bin = BinSpec{Width: 3, Depth: 3, Height: 3}
			req.Contact = ContactSpec{SideX: 1, SideY: 1} // force lateral compaction
			req.RefineVoids = true                        // and the void refiner
			req.Items = []ItemSpec{
				{ID: "f1", Width: 1, Depth: 1, Height: 1, Scalars: map[string]float64{"weight": 1, "bearlimit": 0}},
				{ID: "s1", Width: 1, Depth: 1, Height: 1, Scalars: map[string]float64{"weight": 1}},
				{ID: "h1", Width: 1, Depth: 1, Height: 1, Scalars: map[string]float64{"weight": 50}},
				{ID: "h2", Width: 2, Depth: 1, Height: 1, Scalars: map[string]float64{"weight": 40}},
				{ID: "f2", Width: 1, Depth: 2, Height: 1, Scalars: map[string]float64{"weight": 2, "bearlimit": 0}},
			}

			resp := PackCtx(context.Background(), req)
			if resp.Error != "" {
				t.Fatalf("solve failed: %s", resp.Error)
			}

			// Rebuild the placements per bin and check the rule directly.
			guard := req.bearingGuard()
			if guard == nil {
				t.Fatal("expected a guard")
			}
			byBin := map[int][]*d3.Placement3D{}
			for _, p := range resp.Placements {
				byBin[p.BinIndex] = append(byBin[p.BinIndex], d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
			}
			for bin, ps := range byBin {
				if !guard.OK(ps) {
					t.Errorf("bin %d violates the bearing rule after post-passes", bin)
				}
			}
		})
	}
}

// The advertised bearing capability must match the enforced set exactly — the
// frontend self-configures from /api/algos, so a flag that drifts from
// bearingAlgos3D would offer users a constraint the solve silently drops.
func TestBearingCapabilityMatchesEnforcement(t *testing.T) {
	caps := AlgoCapabilities()
	for algo, c := range caps.Algos {
		if c.Bearing != bearingAlgos3D[algo] {
			t.Errorf("%s: advertised bearing=%v, enforced=%v", algo, c.Bearing, bearingAlgos3D[algo])
		}
	}
	for algo := range bearingAlgos3D {
		if !caps.Algos[algo].Bearing {
			t.Errorf("%s enforces bearing but is not advertised", algo)
		}
	}
}

// The bearing demo preset must actually demonstrate the constraint: packed
// without bearing it crushes a fragile item, packed with it the result is legal.
// With strength ordering on it also costs no extra bin, which is the point of
// the ordering — so the demo shows the constraint being satisfied rather than
// merely being expensive. A preset that behaves identically either way teaches
// nothing, and this is the only place that property is checked.
func TestBearingPresetDemonstratesTheConstraint(t *testing.T) {
	const label = "Fragile on top (load-bearing limits)"
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
	if p.Bearing == nil || !p.Bearing.Enabled {
		t.Fatal("preset does not enable bearing")
	}

	build := func(withBearing bool) PackRequest {
		req := PackRequest{
			Mode: "3d", Algorithm: p.Algo,
			Bin: BinSpec{Width: p.Bin.W, Depth: p.Bin.D, Height: p.Bin.H},
		}
		for i, it := range p.Items {
			req.Items = append(req.Items, ItemSpec{
				ID: "i" + string(rune('a'+i)), Width: it.W, Depth: it.D, Height: it.H, Scalars: it.S,
			})
		}
		if withBearing {
			// Mirror the preset exactly, ordering flag included — copying only
			// some fields would leave the demo's real behaviour untested.
			req.Bearing = BearingSpec{
				WeightScalar:     p.Bearing.WeightScalar,
				LimitScalar:      p.Bearing.LimitScalar,
				DefaultLimit:     p.Bearing.DefaultLimit,
				DefaultUnlimited: p.Bearing.DefaultUnlimited,
				OrderByStrength:  p.Bearing.OrderByStrength,
			}
		}
		return req
	}

	off := PackCtx(context.Background(), build(false))
	on := PackCtx(context.Background(), build(true))
	if off.Error != "" || on.Error != "" {
		t.Fatalf("solve errors: off=%q on=%q", off.Error, on.Error)
	}

	guard := build(true).bearingGuard()
	violates := func(resp PackResponse) bool {
		byBin := map[int][]*d3.Placement3D{}
		for _, pl := range resp.Placements {
			byBin[pl.BinIndex] = append(byBin[pl.BinIndex],
				d3.NewPlacement3D("", pl.ItemID, pl.X, pl.Y, pl.Z, pl.W, pl.D, pl.H))
		}
		for _, ps := range byBin {
			if !guard.OK(ps) {
				return true
			}
		}
		return false
	}

	if !violates(off) {
		t.Error("without bearing the preset packs legally anyway — it no longer demonstrates the constraint")
	}
	if violates(on) {
		t.Error("with bearing the preset still crushes an item")
	}
	// Enforcing the constraint must not cost a bin here: the legal arrangement
	// exists, and the preset's algorithm is expected to find it unaided.
	if on.BinsUsed != off.BinsUsed {
		t.Errorf("enforcing bearing cost a bin: %d without, %d with (winner %q) — "+
			"the solver is not finding the legal arrangement on its own",
			off.BinsUsed, on.BinsUsed, on.BestPacker)
	}
}

// Streaming must enforce bearing exactly as the unary path does. streamSolve
// builds its own factory rather than going through pack3D, so gating pack3D
// alone left every streamable algorithm silently unconstrained — which is what
// the demo actually hit. Comparing the two paths is the only check that covers
// a solve path nobody remembered to gate.
func TestBearingEnforcedWhenStreaming(t *testing.T) {
	for algo := range bearingAlgos3D {
		t.Run(algo, func(t *testing.T) {
			req := bearingReq(algo)
			req.Bin = BinSpec{Width: 10, Depth: 10, Height: 10}
			req.Items = nil
			// Four fragile cartons FFD lays on the floor, then four heavy crates
			// that can only go on top of them.
			for i := 0; i < 4; i++ {
				req.Items = append(req.Items, ItemSpec{
					ID: "f" + string(rune('1'+i)), Width: 5, Depth: 5, Height: 4,
					Scalars: map[string]float64{"weight": 5, "bearlimit": 0},
				})
			}
			for i := 0; i < 4; i++ {
				req.Items = append(req.Items, ItemSpec{
					ID: "h" + string(rune('1'+i)), Width: 5, Depth: 5, Height: 3,
					Scalars: map[string]float64{"weight": 60},
				})
			}

			if !isStreamable(req) {
				t.Skipf("%s does not stream", algo)
			}

			unary := PackCtx(context.Background(), req)
			if unary.Error != "" {
				t.Fatalf("unary: %s", unary.Error)
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

			// The streamed placements must satisfy the rule.
			guard := req.bearingGuard()
			byBin := map[int][]*d3.Placement3D{}
			for _, p := range placements {
				byBin[p.BinIndex] = append(byBin[p.BinIndex],
					d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
			}
			for bin, ps := range byBin {
				if !guard.OK(ps) {
					t.Errorf("streamed bin %d violates the bearing rule", bin)
				}
			}

			// And must use the same number of bins as the unary solve: a stream
			// that quietly drops the constraint packs tighter, which is the exact
			// symptom to catch.
			bins := 0
			for b := range byBin {
				if b+1 > bins {
					bins = b + 1
				}
			}
			if bins != unary.BinsUsed {
				t.Errorf("streamed %d bins, unary %d — the paths disagree", bins, unary.BinsUsed)
			}
		})
	}
}

// bearingOrderReq is the pathological case: four large fragile cartons and four
// smaller heavy crates. Volume order puts the fragile ones on the floor, filling
// it, so the heavy ones need a second bin — even though "heavy underneath,
// fragile on top" fits in one.
func bearingOrderReq(algo string, orderByStrength bool) PackRequest {
	req := PackRequest{
		Mode: "3d", Algorithm: algo,
		Bin: BinSpec{Width: 10, Depth: 10, Height: 10},
		Bearing: BearingSpec{
			WeightScalar: "weight", LimitScalar: "bearlimit",
			DefaultUnlimited: true, OrderByStrength: orderByStrength,
		},
	}
	for i := 0; i < 4; i++ {
		req.Items = append(req.Items, ItemSpec{
			ID: "f" + string(rune('1'+i)), Width: 5, Depth: 5, Height: 4,
			Scalars: map[string]float64{"weight": 5, "bearlimit": 0},
		})
	}
	for i := 0; i < 4; i++ {
		req.Items = append(req.Items, ItemSpec{
			ID: "h" + string(rune('1'+i)), Width: 5, Depth: 5, Height: 3,
			Scalars: map[string]float64{"weight": 60},
		})
	}
	return req
}

// Ordering by strength must recover the single-bin packing the gate alone cannot
// reach, without violating the constraint.
func TestBearingOrderByStrengthRecoversTheBin(t *testing.T) {
	for _, algo := range []string{"ffd", "bfd", "nfd"} {
		t.Run(algo, func(t *testing.T) {
			plain := PackCtx(context.Background(), bearingOrderReq(algo, false))
			sorted := PackCtx(context.Background(), bearingOrderReq(algo, true))
			if plain.Error != "" || sorted.Error != "" {
				t.Fatalf("errors: plain=%q sorted=%q", plain.Error, sorted.Error)
			}
			if plain.BinsUsed != 2 {
				t.Fatalf("expected volume order to need 2 bins, got %d — the case no longer demonstrates the problem", plain.BinsUsed)
			}
			if sorted.BinsUsed != 1 {
				t.Errorf("strength order used %d bins, want 1", sorted.BinsUsed)
			}

			// And the recovered packing must still honour every limit.
			guard := bearingOrderReq(algo, true).bearingGuard()
			byBin := map[int][]*d3.Placement3D{}
			for _, p := range sorted.Placements {
				byBin[p.BinIndex] = append(byBin[p.BinIndex],
					d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
			}
			for bin, ps := range byBin {
				if !guard.OK(ps) {
					t.Errorf("bin %d violates the bearing rule", bin)
				}
			}
		})
	}
}

// The ordering applies only to the sorting algorithms. The online ones pack in
// arrival order by definition, so the flag must not quietly reorder them.
func TestBearingOrderByStrengthLeavesOnlineAlgosAlone(t *testing.T) {
	for _, algo := range []string{"ff", "nf", "bf", "wf", "blf", "ems", "heightmap"} {
		t.Run(algo, func(t *testing.T) {
			off := PackCtx(context.Background(), bearingOrderReq(algo, false))
			on := PackCtx(context.Background(), bearingOrderReq(algo, true))
			if off.BinsUsed != on.BinsUsed || len(off.Placements) != len(on.Placements) {
				t.Fatalf("flag changed an online algorithm: %d/%d bins, %d/%d placements",
					off.BinsUsed, on.BinsUsed, len(off.Placements), len(on.Placements))
			}
			for i := range off.Placements {
				if off.Placements[i] != on.Placements[i] {
					t.Errorf("placement %d differs", i)
				}
			}
		})
	}
}

// Streaming must apply the same ordering as the unary path. Gating pack3D but
// not streamSolve is exactly the bug this feature already shipped once.
func TestBearingOrderByStrengthMatchesWhenStreaming(t *testing.T) {
	for _, algo := range []string{"ffd", "bfd", "nfd"} {
		t.Run(algo, func(t *testing.T) {
			req := bearingOrderReq(algo, true)
			if !isStreamable(req) {
				t.Skipf("%s does not stream", algo)
			}
			unary := PackCtx(context.Background(), req)

			var placements []PlacementResult
			StreamPack(context.Background(), req, func(f StreamFrame) {
				switch f.Type {
				case "batch":
					placements = append(placements, f.Placements...)
				case "reposition":
					placements = f.Placements
				}
			})
			bins := 0
			for _, p := range placements {
				if p.BinIndex+1 > bins {
					bins = p.BinIndex + 1
				}
			}
			if bins != unary.BinsUsed {
				t.Errorf("streamed %d bins, unary %d — the paths disagree on the ordering", bins, unary.BinsUsed)
			}
		})
	}
}

// With bearing off the flag must be inert, so it cannot change ordinary solves.
func TestBearingOrderByStrengthInertWithoutBearing(t *testing.T) {
	req := bearingOrderReq("ffd", true)
	req.Bearing = BearingSpec{OrderByStrength: true} // no weight scalar ⇒ disabled
	withFlag := PackCtx(context.Background(), req)
	req.Bearing = BearingSpec{}
	plain := PackCtx(context.Background(), req)
	if withFlag.BinsUsed != plain.BinsUsed {
		t.Errorf("flag changed a non-bearing solve: %d vs %d bins", withFlag.BinsUsed, plain.BinsUsed)
	}
}

// The whole point of "auto": a user enters items and constraints and gets the
// best legal packing, without choosing an algorithm or knowing that an item
// ordering exists. On the pathological case auto must reach the single bin that
// only strength ordering finds, with OrderByStrength left off.
func TestBearingAutoFindsTheBestLegalPacking(t *testing.T) {
	req := bearingOrderReq("auto", false) // no ordering hint from the user
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("auto refused a bearing request: %s", resp.Error)
	}
	if resp.BinsUsed != 1 {
		t.Errorf("auto used %d bins; strength ordering reaches 1, so auto should find it (winner %q)",
			resp.BinsUsed, resp.BestPacker)
	}

	// Whatever auto picked must honour every limit.
	guard := req.bearingGuard()
	byBin := map[int][]*d3.Placement3D{}
	for _, p := range resp.Placements {
		byBin[p.BinIndex] = append(byBin[p.BinIndex],
			d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
	}
	for bin, ps := range byBin {
		if !guard.OK(ps) {
			t.Errorf("auto's winner (%q) violates the bearing rule in bin %d", resp.BestPacker, bin)
		}
	}
	if len(resp.Placements) != len(req.Items) {
		t.Errorf("auto placed %d of %d items", len(resp.Placements), len(req.Items))
	}
}

// Auto must never win its race with a packer that ignores the gate. The
// self-managing packers build their own bins and cannot enforce it, so they have
// to be out of the race whenever bearing is on.
func TestBearingAutoExcludesUnenforcingCandidates(t *testing.T) {
	if autoSelfManaged3D(bearingOrderReq("auto", false)) {
		t.Error("auto would race blocks/assemble/LAFF with bearing on; they cannot enforce it")
	}
	// And every plan auto races must name a bearing-capable strategy.
	for _, pl := range auto3DPlans(bearingOrderReq("auto", false)) {
		switch pl.strat {
		case "fit", "layer":
			t.Errorf("auto races %q (strategy %q), which has no bearing gate", pl.label, pl.strat)
		}
	}
}

// Auto's two implementations — the registry solver and the streaming mirror —
// must race the same candidates, or Pack and StreamPack pick different winners.
func TestBearingAutoStreamingAgrees(t *testing.T) {
	req := bearingOrderReq("auto", false)
	unary := PackCtx(context.Background(), req)

	// auto streams every racing candidate at once, each tagged with its segment,
	// so only the winning segment's placements are the answer.
	bySeg := map[int][]PlacementResult{}
	var streamErr, winnerLabel string
	var winnerSeg *int
	var doneBins int
	StreamPack(context.Background(), req, func(f StreamFrame) {
		switch f.Type {
		case "batch":
			bySeg[f.Seg] = append(bySeg[f.Seg], f.Placements...)
		case "error":
			streamErr = f.Error
		case "done":
			winnerSeg, doneBins, winnerLabel = f.WinnerSeg, f.BinsUsed, f.BestPacker
		}
	})
	if streamErr != "" {
		t.Fatalf("stream: %s", streamErr)
	}
	if winnerSeg == nil {
		t.Fatal("no winning segment reported")
	}
	if doneBins != unary.BinsUsed {
		t.Errorf("auto streamed %d bins (winner %q), unary %d (winner %q) — the candidate sets disagree",
			doneBins, winnerLabel, unary.BinsUsed, unary.BestPacker)
	}

	// The winning segment's packing must itself honour every limit: a candidate
	// that cannot enforce the gate must never be able to win the race.
	guard := req.bearingGuard()
	byBin := map[int][]*d3.Placement3D{}
	for _, p := range bySeg[*winnerSeg] {
		byBin[p.BinIndex] = append(byBin[p.BinIndex],
			d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
	}
	for bin, ps := range byBin {
		if !guard.OK(ps) {
			t.Errorf("streamed winner %q violates the bearing rule in bin %d", winnerLabel, bin)
		}
	}
}

// Auto without bearing must be unchanged: the refactor into auto3DPlans must not
// alter the ordinary race.
func TestAutoUnchangedWithoutBearing(t *testing.T) {
	req := bearingOrderReq("auto", false)
	req.Bearing = BearingSpec{}
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("auto: %s", resp.Error)
	}
	if len(resp.Placements) != len(req.Items) {
		t.Errorf("auto placed %d of %d", len(resp.Placements), len(req.Items))
	}
	// Fit and Layer belong in the race when bearing is off.
	var labels []string
	for _, pl := range auto3DPlans(req) {
		labels = append(labels, pl.label)
	}
	for _, want := range []string{"Fit", "Layer"} {
		found := false
		for _, l := range labels {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s dropped from the non-bearing auto race (%v)", want, labels)
		}
	}
}

// auto's tail re-packs shuffled orderings through the search decoder and
// replaces the race winner if the result scores better. That decoder builds its
// own factory, so without the gate auto could return a crushing packing that
// none of its racing candidates produced. It can also be "fit", whose strategy
// has no gate at all.
//
// All-fragile items make this loud: nothing may stack, so every item must sit on
// the floor and eight 5x5 footprints cannot share one 10x10 bin.
func TestBearingAutoSweepCannotUndoTheGate(t *testing.T) {
	for _, algo := range []string{"auto", "ffd", "ems"} {
		t.Run(algo, func(t *testing.T) {
			req := bearingOrderReq(algo, false)
			req.Items = append([]ItemSpec(nil), req.Items...)
			for i := range req.Items {
				sc := map[string]float64{}
				for k, v := range req.Items[i].Scalars {
					sc[k] = v
				}
				sc["bearlimit"] = 0 // everything fragile
				req.Items[i].Scalars = sc
			}
			resp := PackCtx(context.Background(), req)
			if resp.Error != "" {
				t.Fatalf("solve: %s", resp.Error)
			}

			guard := req.bearingGuard()
			byBin := map[int][]*d3.Placement3D{}
			for _, p := range resp.Placements {
				byBin[p.BinIndex] = append(byBin[p.BinIndex],
					d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
			}
			for bin, ps := range byBin {
				if !guard.OK(ps) {
					t.Errorf("%s (winner %q) crushed a fragile item in bin %d", algo, resp.BestPacker, bin)
				}
			}
			for _, p := range resp.Placements {
				if p.Z > compactEpsUI {
					t.Errorf("%s put %s at z=%v though nothing may be stacked on", algo, p.ItemID, p.Z)
				}
			}
		})
	}
}

const compactEpsUI = 1e-9

// A scalar name no item carries must be refused, not silently honoured. A
// mis-typed weight scalar makes every item weightless, so no limit can ever be
// exceeded and the constraint looks enforced while doing nothing.
func TestBearingRejectsScalarNamesNoItemCarries(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*PackRequest)
		want string
	}{
		{"weight scalar absent", func(r *PackRequest) { r.Bearing.WeightScalar = "mass" }, "no item has a \"mass\" scalar"},
		{"limit scalar absent", func(r *PackRequest) { r.Bearing.LimitScalar = "typo" }, "no item has a \"typo\" scalar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := bearingOrderReq("auto", false)
			tt.mut(&req)
			resp := PackCtx(context.Background(), req)
			if !strings.Contains(resp.Error, tt.want) {
				t.Errorf("error = %q, want it to mention %q", resp.Error, tt.want)
			}
		})
	}
}

// Container-catalog mode must enforce bearing, not refuse it: choosing the best
// container size and respecting crush limits are the same job. Single-type and
// cascade both solve each candidate through packOneBin → pack3D, so the gate
// applies; this pins that it really does.
func TestBearingEnforcedInCatalogMode(t *testing.T) {
	// Two container sizes. The small one cannot hold the order legally once
	// stacking on the fragile cartons is forbidden; the large one can.
	build := func(withBearing bool) PackRequest {
		req := bearingOrderReq("auto", false)
		req.Containers = []ContainerSpec{
			{Bin: BinSpec{Width: 10, Depth: 10, Height: 10}},
			{Bin: BinSpec{Width: 10, Depth: 10, Height: 20}},
		}
		if !withBearing {
			req.Bearing = BearingSpec{}
		}
		return req
	}

	on := PackCtx(context.Background(), build(true))
	if on.Error != "" {
		t.Fatalf("catalog + bearing refused: %s", on.Error)
	}
	if len(on.Placements) != len(build(true).Items) {
		t.Errorf("placed %d of %d items", len(on.Placements), len(build(true).Items))
	}

	guard := build(true).bearingGuard()
	byBin := map[int][]*d3.Placement3D{}
	for _, p := range on.Placements {
		byBin[p.BinIndex] = append(byBin[p.BinIndex],
			d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
	}
	if len(byBin) == 0 {
		t.Fatal("no placements")
	}
	for bin, ps := range byBin {
		if !guard.OK(ps) {
			t.Errorf("catalog bin %d (container %q) violates the bearing rule", bin, on.Container)
		}
	}
}

// The cascade path (reached when no single container holds the whole order)
// concatenates results from separate solves, so it has to be checked too.
func TestBearingEnforcedInCatalogCascade(t *testing.T) {
	req := bearingOrderReq("auto", false)
	// Double the order so it cannot fit in one bin, and cap *every* type at one
	// bin — otherwise an uncapped type holds the whole order and the single-type
	// branch wins, leaving the cascade untested.
	base := req.Items
	var doubled []ItemSpec
	for i := 0; i < 2; i++ {
		for _, it := range base {
			c := it
			c.ID = it.ID + string(rune('A'+i))
			doubled = append(doubled, c)
		}
	}
	req.Items = doubled
	req.Containers = []ContainerSpec{
		{Bin: BinSpec{Width: 10, Depth: 10, Height: 10}, MaxCount: 1},
		{Bin: BinSpec{Width: 10, Depth: 10, Height: 10}, MaxCount: 1},
	}
	if len(solveCatalogSingle(context.Background(), req).Unplaced) == 0 {
		t.Fatal("a single container type held the whole order — the cascade branch is not being exercised")
	}

	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("cascade refused: %s", resp.Error)
	}
	guard := req.bearingGuard()
	byBin := map[int][]*d3.Placement3D{}
	for _, p := range resp.Placements {
		byBin[p.BinIndex] = append(byBin[p.BinIndex],
			d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
	}
	for bin, ps := range byBin {
		if !guard.OK(ps) {
			t.Errorf("cascade bin %d violates the bearing rule", bin)
		}
	}
}

// Balance preferences and bearing must compose without costing a bin. The
// balanced path does not go through the registry, so it races the orderings
// itself: BalancedFit probes for the achievable bin count and pre-opens that
// many, and probing in an order the gate cannot honour inflates the probe, so
// the balance is then spread over bins that were never needed.
func TestBearingWithBalancePreferences(t *testing.T) {
	for _, algo := range []string{"auto", "bf", "wf", "pref"} {
		t.Run(algo, func(t *testing.T) {
			req := bearingOrderReq(algo, false)
			req.Preferences = []PreferenceSpec{{Scalar: "weight", Mode: "balance"}}
			resp := PackCtx(context.Background(), req)
			if resp.Error != "" {
				t.Fatalf("refused: %s", resp.Error)
			}
			if resp.BinsUsed != 1 {
				t.Errorf("used %d bins; the legal packing fits in 1, so balancing lost a bin", resp.BinsUsed)
			}
			guard := req.bearingGuard()
			byBin := map[int][]*d3.Placement3D{}
			for _, p := range resp.Placements {
				byBin[p.BinIndex] = append(byBin[p.BinIndex],
					d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
			}
			for bin, ps := range byBin {
				if !guard.OK(ps) {
					t.Errorf("bin %d violates the bearing rule", bin)
				}
			}
			if len(resp.Placements) != len(req.Items) {
				t.Errorf("placed %d of %d items", len(resp.Placements), len(req.Items))
			}
		})
	}
}

// The ordering race must not change balanced solves that have no bearing spec.
func TestBalanceUnchangedWithoutBearing(t *testing.T) {
	req := bearingOrderReq("auto", false)
	req.Bearing = BearingSpec{}
	req.Preferences = []PreferenceSpec{{Scalar: "weight", Mode: "balance"}}
	a := PackCtx(context.Background(), req)
	b := PackCtx(context.Background(), req)
	if a.BinsUsed != b.BinsUsed || len(a.Placements) != len(b.Placements) {
		t.Fatalf("non-deterministic: %d/%d bins", a.BinsUsed, b.BinsUsed)
	}
	for i := range a.Placements {
		if a.Placements[i] != b.Placements[i] {
			t.Errorf("placement %d differs between identical solves", i)
		}
	}
}

// RefineBalance re-packs each bin's item set and its rebuild used to discard any
// item that failed to re-place — not recording it as unplaced either, so items
// vanished from the result. A refinement pass must never return less than it was
// given. The load-bearing gate is what exposed it: a set that packs in strength
// order need not pack largest-first, which is the order the rebuild used.
func TestRefineBalanceNeverLosesItems(t *testing.T) {
	req := bearingOrderReq("bf", false)
	req.Preferences = []PreferenceSpec{{Scalar: "weight", Mode: "balance"}}

	spec := d3.ContactSpec{Bottom: req.Contact.Bottom, NoFloating: req.Contact.NoFloating}
	factory := pack.NewConstrainedFactory(constrainedFactory(
		d3.NewFactory(req.Bin.Width, req.Bin.Depth, req.Bin.Height,
			strat3DForBearing(req.Algorithm, spec, req.Bearing.toD3())), req.Constraints))

	prefs, weights := buildPreferences(req.Preferences)
	items := items3D(req)
	packed, err := offline.NewBalancedFitW(factory, prefs, weights).
		WithOrder(req.bearingSort3D()).PackAll(items)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if len(packed.Placements) != len(items) {
		t.Fatalf("setup packed %d of %d items", len(packed.Placements), len(items))
	}

	// Refining with the wrong ordering must not lose items — it should decline
	// the rebuild and hand back what it was given.
	wrong := offline.RefineBalance(factory, packed, items, offline.RefineOptions{})
	if len(wrong.Placements) != len(items) {
		t.Errorf("refine with a mismatched order returned %d placements, was given %d",
			len(wrong.Placements), len(items))
	}

	// With the matching ordering it may refine freely, but still not lose any.
	right := offline.RefineBalance(factory, packed, items,
		offline.RefineOptions{Order: req.bearingSort3D()})
	if len(right.Placements) != len(items) {
		t.Errorf("refine with the matching order returned %d placements, was given %d",
			len(right.Placements), len(items))
	}
}

// Pressure end to end: a box rated for plenty of total weight must still refuse
// a load concentrated on a narrow foot.
func TestBearingPressureEndToEnd(t *testing.T) {
	build := func(withPressure bool) PackRequest {
		req := PackRequest{
			Mode: "3d", Algorithm: "auto",
			Bin: BinSpec{Width: 5, Depth: 5, Height: 10},
			Bearing: BearingSpec{
				WeightScalar: "weight", LimitScalar: "bearlimit", DefaultUnlimited: true,
			},
			Items: []ItemSpec{
				// 5x5 pallet: takes 100kg total, but only 2 per unit of area.
				{ID: "pallet", Width: 5, Depth: 5, Height: 1,
					Scalars: map[string]float64{"weight": 1, "bearlimit": 100, "bearpressure": 2}},
				// 1x1 post weighing 25: 25kg over 1 unit of area.
				{ID: "post", Width: 1, Depth: 1, Height: 1,
					Scalars: map[string]float64{"weight": 25}},
			},
		}
		if withPressure {
			req.Bearing.PressureScalar = "bearpressure"
		}
		return req
	}

	// Without the pressure check the post stacks: 25kg is well under 100kg.
	off := PackCtx(context.Background(), build(false))
	if off.Error != "" {
		t.Fatalf("off: %s", off.Error)
	}
	var offPost float64 = -1
	for _, p := range off.Placements {
		if p.ItemID == "post" {
			offPost = p.Z
		}
	}
	if offPost <= 0 {
		t.Fatalf("without the pressure check the post should stack (z=%v); the case proves nothing", offPost)
	}

	// With it, the concentrated load is refused and the post goes elsewhere.
	on := PackCtx(context.Background(), build(true))
	if on.Error != "" {
		t.Fatalf("on: %s", on.Error)
	}
	guard := build(true).bearingGuard()
	byBin := map[int][]*d3.Placement3D{}
	for _, p := range on.Placements {
		byBin[p.BinIndex] = append(byBin[p.BinIndex],
			d3.NewPlacement3D("", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H))
	}
	for bin, ps := range byBin {
		if !guard.OK(ps) {
			t.Errorf("bin %d exceeds the pressure cap", bin)
		}
	}
	for _, p := range on.Placements {
		if p.ItemID == "post" && p.Z > 0 && p.BinIndex == 0 {
			t.Errorf("post still stacked on the pallet at z=%v despite the pressure cap", p.Z)
		}
	}
}

// A pressure scalar no item carries is refused, like the other scalar names.
func TestBearingRejectsUnusedPressureScalar(t *testing.T) {
	req := bearingOrderReq("auto", false)
	req.Bearing.PressureScalar = "nope"
	if resp := PackCtx(context.Background(), req); !strings.Contains(resp.Error, `no item has a "nope" scalar`) {
		t.Errorf("error = %q", resp.Error)
	}
}

// preset3D finds a 3-D demo preset by label.
func preset3D(t *testing.T, label string) *Preset {
	t.Helper()
	for _, p := range Presets()["3d"] {
		if p.Label == label {
			c := p
			return &c
		}
	}
	t.Fatalf("preset %q is missing", label)
	return nil
}

func presetItemSpecs(p *Preset) []ItemSpec {
	out := make([]ItemSpec, 0, len(p.Items))
	for i, it := range p.Items {
		out = append(out, ItemSpec{ID: fmt.Sprintf("i%d", i),
			Width: it.W, Depth: it.D, Height: it.H, Scalars: it.S})
	}
	return out
}

func presetBearingSpec(p *Preset) BearingSpec {
	if p.Bearing == nil {
		return BearingSpec{}
	}
	return BearingSpec{
		WeightScalar: p.Bearing.WeightScalar, LimitScalar: p.Bearing.LimitScalar,
		PressureScalar: p.Bearing.PressureScalar, DefaultLimit: p.Bearing.DefaultLimit,
		DefaultPressure: p.Bearing.DefaultPressure, DefaultUnlimited: p.Bearing.DefaultUnlimited,
		OrderByStrength: p.Bearing.OrderByStrength,
	}
}

// The pressure preset has to be legible without toggling anything: the wide
// crate must *stay* stacked while the lighter narrow posts are pushed off. Bin
// counts alone would pass on a preset where nothing stacks at all, which reads
// as "bearing forbids stacking" rather than "pressure forbids concentration".
func TestBearingPressurePresetDemonstrates(t *testing.T) {
	p := preset3D(t, "Point loads: pressure, not just weight")
	if p.Bearing == nil || p.Bearing.PressureScalar == "" {
		t.Fatal("preset does not name a pressure scalar")
	}
	build := func(withBearing bool) PackRequest {
		req := PackRequest{Mode: "3d", Algorithm: p.Algo,
			Bin:   BinSpec{Width: p.Bin.W, Depth: p.Bin.D, Height: p.Bin.H},
			Items: presetItemSpecs(p)}
		if withBearing {
			req.Bearing = presetBearingSpec(p)
		}
		return req
	}
	off := PackCtx(context.Background(), build(false))
	on := PackCtx(context.Background(), build(true))
	if off.Error != "" || on.Error != "" {
		t.Fatalf("errors: off=%q on=%q", off.Error, on.Error)
	}
	if on.BinsUsed <= off.BinsUsed {
		t.Errorf("pressure changed nothing: %d bins without, %d with", off.BinsUsed, on.BinsUsed)
	}

	// Classify by footprint: the base and the wide crate span the bin, the posts
	// are the narrow ones.
	base := p.Items[0]
	var wide, narrow []PlacementResult
	for _, q := range on.Placements {
		if q.W*q.D >= base.W*base.D {
			wide = append(wide, q)
		} else {
			narrow = append(narrow, q)
		}
	}
	if len(wide) < 2 || len(narrow) == 0 {
		t.Fatalf("preset shape changed: %d wide, %d narrow", len(wide), len(narrow))
	}
	// A wide item must still rest on the base — spread load is fine.
	stacked := false
	for _, q := range wide {
		if q.Z > 0 {
			stacked = true
		}
	}
	if !stacked {
		t.Error("no wide item stacked, so the preset reads as 'nothing may stack' rather than " +
			"'concentrated load may not'")
	}
	// The narrow posts must not be on the base, though they weigh less.
	var narrowWeight float64
	for _, q := range narrow {
		if q.BinIndex == 0 && q.Z > 0 {
			t.Errorf("narrow post %s is still stacked at z=%v", q.ItemID, q.Z)
		}
	}
	for _, it := range p.Items[1:] {
		if it.W*it.D < base.W*base.D {
			narrowWeight += it.S["weight"]
		}
	}
	// And the weight limit alone cannot explain the refusal, which is the point.
	if limit := base.S["bearlimit"]; narrowWeight >= limit {
		t.Errorf("the posts weigh %v against a limit of %v — the weight limit would have "+
			"refused them anyway, so the preset does not isolate pressure", narrowWeight, limit)
	}
}

// The nested pair must differ only by the rigid flag, and must pack differently:
// load runs through flush contents when the carton is not rigid, so the same
// order needs fewer pallets.
func TestBearingNestedPresetsContrast(t *testing.T) {
	flex := preset3D(t, "Corner posts carry the stack (nested)")
	rigid := preset3D(t, "Rigid cartons: the lid carries it (nested)")

	if flex.CartonBearing == nil || rigid.CartonBearing == nil {
		t.Fatal("a nested bearing preset is missing its carton rating")
	}
	if flex.CartonBearing.Rigid || !rigid.CartonBearing.Rigid {
		t.Fatalf("the pair must differ by the rigid flag: flex=%v rigid=%v",
			flex.CartonBearing.Rigid, rigid.CartonBearing.Rigid)
	}
	if flex.CartonBearing.Limit != rigid.CartonBearing.Limit {
		t.Errorf("the pair must share a carton rating to isolate the flag: %v vs %v",
			flex.CartonBearing.Limit, rigid.CartonBearing.Limit)
	}
	if len(flex.Items) != len(rigid.Items) {
		t.Errorf("the pair must share an item set: %d vs %d items", len(flex.Items), len(rigid.Items))
	}

	pallets := func(p *Preset) int {
		bs := presetBearingSpec(p)
		req := NestedPackRequest{Mode: "3d", Items: presetItemSpecs(p),
			Levels: []NestedLevelSpec{
				{Bin: BinSpec{Width: p.InnerBin.W, Depth: p.InnerBin.D, Height: p.InnerBin.H},
					Algorithm: p.InnerAlgo, Bearing: bs},
				{Bin: BinSpec{Width: p.Bin.W, Depth: p.Bin.D, Height: p.Bin.H},
					Algorithm: p.Algo, Bearing: bs, ContainerBearing: p.CartonBearing},
			}}
		r, err := PackNestedCtx(context.Background(), req)
		if err != nil {
			t.Fatalf("%s: %v", p.Label, err)
		}
		if r.Error != "" {
			t.Fatalf("%s: %s", p.Label, r.Error)
		}
		return r.Levels[1].BinsUsed
	}

	f, r := pallets(flex), pallets(rigid)
	if f >= r {
		t.Errorf("load paths bought nothing: %d pallets with flush contents carrying, "+
			"%d with a rigid carton", f, r)
	}
}

// The nested preset must actually look like posts in a carton. Its first
// version put one item per carton, filling it completely — no corners, no
// partial footprint, nothing a reader would recognise as the arrangement the
// name promises. Bin counts alone did not notice.
func TestBearingNestedPresetHasCornerPosts(t *testing.T) {
	p := preset3D(t, "Corner posts carry the stack (nested)")
	bs := presetBearingSpec(p)
	req := NestedPackRequest{Mode: "3d", Items: presetItemSpecs(p),
		Levels: []NestedLevelSpec{
			{Bin: BinSpec{Width: p.InnerBin.W, Depth: p.InnerBin.D, Height: p.InnerBin.H},
				Algorithm: p.InnerAlgo, Bearing: bs},
			{Bin: BinSpec{Width: p.Bin.W, Depth: p.Bin.D, Height: p.Bin.H},
				Algorithm: p.Algo, Bearing: bs, ContainerBearing: p.CartonBearing},
		}}
	r, err := PackNestedCtx(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != "" {
		t.Fatalf("solve: %s", r.Error)
	}

	var first []PlacementResult
	for _, q := range r.Levels[0].Placements {
		if q.BinIndex == 0 {
			first = append(first, q)
		}
	}
	if len(first) < 4 {
		t.Fatalf("carton 0 holds %d items; the name promises several posts", len(first))
	}
	// Distinct corners: no two contents may share an (x,y).
	seen := map[[2]float64]bool{}
	for _, q := range first {
		k := [2]float64{q.X, q.Y}
		if seen[k] {
			t.Errorf("two contents share the footprint corner %v", k)
		}
		seen[k] = true
	}
	// Each must reach the carton's lid, or no load would pass through it.
	for _, q := range first {
		if q.Z+q.H != p.InnerBin.H {
			t.Errorf("content %s tops out at %v, not the carton lid at %v — it carries nothing",
				q.ItemID, q.Z+q.H, p.InnerBin.H)
		}
		// And each must leave room for others: a single item filling the carton
		// is not a post.
		if q.W*q.D >= p.InnerBin.W*p.InnerBin.D {
			t.Errorf("content %s spans the whole carton footprint; that is a filled carton, not posts",
				q.ItemID)
		}
	}
}
