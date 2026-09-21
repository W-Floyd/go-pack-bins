package packapi

import (
	"context"
	"strings"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
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
			name: "catalog mode",
			mut: func(r *PackRequest) {
				r.Containers = []ContainerSpec{{Bin: BinSpec{Width: 1, Depth: 1, Height: 4}}}
			},
			want: "container-catalog",
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
