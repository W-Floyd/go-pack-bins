package packapi

import (
	"errors"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/joint"
	"github.com/W-Floyd/go-pack-bins/knapsack"
	"github.com/W-Floyd/go-pack-bins/meta"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
	"github.com/W-Floyd/go-pack-bins/strip"
)

// solveStrip3D minimises the strip height for the fixed base footprint (bw×bd),
// building its own open-height container; it cannot apply scalar constraints and
// rejects a request with any. The achieved height is reported via solveMeta.extent.
func solveStrip3D(sc *solveCtx) (pack.Result, solveMeta, error) {
	if len(sc.req.Constraints) > 0 {
		return pack.Result{}, solveMeta{}, errors.New("strip: open-height packing cannot apply constraints; choose another algorithm")
	}
	items := make([]*d3.Item3D, 0, len(sc.items))
	for _, it := range sc.items {
		if i3, ok := it.(*d3.Item3D); ok {
			items = append(items, i3)
		}
	}
	r := strip.Pack3D(items, sc.bw, sc.bd)
	return r.Result, solveMeta{extent: r.Height}, nil
}

// solveKnapsack3D packs the most valuable subset into a single container. It
// takes the best (highest total value) of greedy one-at-a-time placement and
// dense block/column building, since fusing same-size items into layers/columns
// often fits more — and so more value. The block/column packers ignore scalar
// constraints, so they are only tried when the request has none; with
// constraints, only the (constrained-factory) greedy path runs. Value is the
// "value" scalar, defaulting to volume. Rejected items are reported.
func solveKnapsack3D(sc *solveCtx) (pack.Result, solveMeta, error) {
	best := knapsack.Pack(sc.ctx, sc.items, sc.factory.Open(), knapsack.Options{})
	if len(sc.req.Constraints) == 0 {
		for _, p := range []pack.CtxOfflinePacker{
			d3.NewBlockPacker(sc.bw, sc.bd, sc.bh),
			d3.NewColumnPacker(sc.bw, sc.bd, sc.bh),
		} {
			if cand := knapsack.PackWith(sc.ctx, sc.items, p, knapsack.Options{}); cand.TotalValue > best.TotalValue {
				best = cand
			}
		}
	}
	return best.Result, solveMeta{totalValue: best.TotalValue, rejected: best.Rejected}, nil
}

// 3-D algorithm dispatch. Unlike 1-D/2-D, the post-pass differs per algorithm —
// the self-managing/search packers run the void-refiner (when requested) while the
// sequential ones get lateral anti-slosh compaction — so each solver applies its
// own finish (finishRefine3D / finishCompact3D); pack3D itself does no post-pass.

// finishRefine3D runs the optional void-refiner (the former respond() path), used
// by the self-managing and order-search 3-D packers.
func finishRefine3D(sc *solveCtx, r pack.Result) pack.Result {
	if sc.req.RefineVoids {
		refineResult3D(sc.ctx, r, sc.req)
	}
	return r
}

// finishCompact3D runs the lateral anti-slosh compaction (when contact requests it),
// used by the sequential/strategy packers.
func finishCompact3D(sc *solveCtx, r pack.Result) pack.Result {
	if dx, dy, any := sc.req.Contact.lateralAxes(); any {
		compactResult3D(r, sc.bw, sc.bd, sc.bh, dx, dy, sc.req.Contact.Bottom)
	}
	return r
}

// fatal reports whether err is a real failure (not the tolerated partial-pack
// ErrItemTooLarge).
func fatal(err error) bool { return err != nil && !errors.Is(err, pack.ErrItemTooLarge) }

// compact3D wraps a packer that uses sc.factory and gets lateral compaction.
func compact3D(run func(sc *solveCtx) (pack.Result, error)) solveFn {
	return func(sc *solveCtx) (pack.Result, solveMeta, error) {
		r, e := run(sc)
		if fatal(e) {
			return r, solveMeta{}, e
		}
		return finishCompact3D(sc, r), solveMeta{}, nil
	}
}

// selfManaged3D wraps a self-managing packer (own geometry/bins) that gets the
// void-refiner post-pass; settle, if set, drops items left floating first.
func selfManaged3D(run func(sc *solveCtx) (pack.Result, error), settle bool) solveFn {
	return func(sc *solveCtx) (pack.Result, solveMeta, error) {
		r, e := run(sc)
		if fatal(e) {
			return r, solveMeta{}, e
		}
		if settle {
			settleResult3D(r)
		}
		return finishRefine3D(sc, r), solveMeta{}, nil
	}
}

// decoder3D builds the order-search decode factory (EMS by default; req.Decoder
// overrides). search algos decode candidate orderings through it, not sc.factory.
func decoder3D(sc *solveCtx) pack.BinFactory {
	spec := d3.ContactSpec{Bottom: sc.req.Contact.Bottom, NoFloating: sc.req.Contact.NoFloating}
	return constrainedFactory(d3.NewFactory(sc.bw, sc.bd, sc.bh, searchDecoder3D(sc.req, spec)), sc.req.Constraints)
}

// searchOpts3D builds the ruin-and-recreate options, optionally driving the search
// through a cheap extreme-point surrogate (search_fast_decode) while still returning
// an EMS-decoded best ordering.
func searchOpts3D(sc *solveCtx) offline.SearchOptions {
	sopts := sc.req.searchOptions(sc.ctx)
	if sc.req.Decoder == "" && sc.req.optInt("search_fast_decode", 1) >= 1 {
		spec := d3.ContactSpec{Bottom: sc.req.Contact.Bottom, NoFloating: sc.req.Contact.NoFloating}
		sopts.DecodeFactory = constrainedFactory(d3.NewFactory(sc.bw, sc.bd, sc.bh, d3.NewExtremePointStrategyContact(spec)), sc.req.Constraints)
	}
	// Stop the bin-count search as soon as it proves the volume lower bound — no
	// budget is then spent failing to beat an already-optimal count.
	sopts.TargetBins = offline.LowerBoundVolume(sc.items, sc.bw*sc.bd*sc.bh)
	return sopts
}

// sweepMaxItems gates the multi-objective seed-sweep escalation to the small,
// combinatorial regime where one-shot constructive packers strand bins or leave
// loose, sloshy arrangements — and where re-packing thousands of shuffled orders is
// cheap. Larger instances stay with the constructive race / order-search.
const sweepMaxItems = 80

// sweepRefine3D tries to improve a constructive packing on the full quality
// objective (fewest unplaced → fewest bins → most compact → least lateral slosh) by
// re-packing the items in many shuffled orders, and returns the better of the two
// (with the winning packer name). It only runs for small instances; slosh is part
// of the objective only when an anti-slosh contact target is set. Never a
// regression: the base packing is kept unless a swept one scores strictly better.
func sweepRefine3D(sc *solveCtx, base pack.Result, baseName string) (pack.Result, string) {
	n := len(sc.items)
	if n == 0 || n > sweepMaxItems {
		return base, baseName
	}
	_, _, useSlosh := sc.req.Contact.lateralAxes()
	const maxTries = 4000
	swept, sq, ok := qualitySweep3D(sc.ctx, decoder3D(sc), sc.items, useSlosh, maxTries)
	if ok && sq.better(scoreQuality3D(base, useSlosh), useSlosh) {
		return swept, "sweep"
	}
	return base, baseName
}

func init() {
	reg := func(algo string, fn solveFn) { registerSolve("3d", algo, fn) }

	// Sequential / strategy packers over sc.factory (strategy set by strat3DFor).
	firstFit := compact3D(func(sc *solveCtx) (pack.Result, error) {
		return runOnline(sc.ctx, online.FirstFit(sc.factory), sc.items)
	})
	for _, a := range []string{"ff", "blf", "ems", "fit", "heightmap"} {
		reg(a, firstFit)
	}
	reg("nf", compact3D(func(sc *solveCtx) (pack.Result, error) {
		return runOnline(sc.ctx, online.NextFit(sc.factory), sc.items)
	}))
	reg("bf", compact3D(func(sc *solveCtx) (pack.Result, error) {
		return runOnline(sc.ctx, online.BestFit(sc.factory), sc.items)
	}))
	reg("wf", compact3D(func(sc *solveCtx) (pack.Result, error) {
		return runOnline(sc.ctx, online.WorstFit(sc.factory), sc.items)
	}))
	reg("ffd", compact3D(func(sc *solveCtx) (pack.Result, error) {
		return packAllCtx(sc.ctx, offline.FirstFitDecreasing(sc.factory), sc.items)
	}))
	reg("bfd", compact3D(func(sc *solveCtx) (pack.Result, error) {
		return packAllCtx(sc.ctx, offline.BestFitDecreasing(sc.factory), sc.items)
	}))
	reg("nfd", compact3D(func(sc *solveCtx) (pack.Result, error) {
		return packAllCtx(sc.ctx, offline.NextFitDecreasing(sc.factory), sc.items)
	}))
	reg("layer", compact3D(func(sc *solveCtx) (pack.Result, error) {
		r, e := packAllCtx(sc.ctx, offline.New("Layer", offline.DecreasingLayerHeight, online.FirstFit(sc.factory)), sc.items)
		if !fatal(e) {
			settleResult3D(r) // the layered packer can float items above short cells
		}
		return r, e
	}))

	// Self-managing packers (own geometry/bins; ignore the factory).
	reg("blocks", selfManaged3D(func(sc *solveCtx) (pack.Result, error) {
		bp := d3.NewBlockPacker(sc.bw, sc.bd, sc.bh)
		if n := sc.req.optInt("block_max_stack", d3.MaxBlockSubs); n > 0 {
			bp.WithMaxStack(n)
		}
		return bp.PackAllCtx(sc.ctx, sc.items)
	}, true))
	reg("columns", selfManaged3D(func(sc *solveCtx) (pack.Result, error) {
		return d3.NewColumnPacker(sc.bw, sc.bd, sc.bh).PackAllCtx(sc.ctx, sc.items)
	}, true))
	reg("assemble", selfManaged3D(func(sc *solveCtx) (pack.Result, error) {
		return d3.NewAssembler(sc.bw, sc.bd, sc.bh).PackAllCtx(sc.ctx, sc.items)
	}, false))
	reg("laff", selfManaged3D(func(sc *solveCtx) (pack.Result, error) { return d3.LAFF(sc.items, sc.bw, sc.bd, sc.bh) }, false))
	reg("brute", selfManaged3D(func(sc *solveCtx) (pack.Result, error) {
		return offline.BruteForce(sc.ctx, sc.items, sc.factory, sc.req.bruteForceOptions(sc.ctx, shapeKey3D))
	}, false))
	reg("joint", selfManaged3D(func(sc *solveCtx) (pack.Result, error) {
		prefs, weights := buildPreferences(sc.req.Preferences)
		jf := joint.New(sc.bw, sc.bd, sc.bh, d3.ContactSpec{
			Bottom: sc.req.Contact.Bottom, SideX: sc.req.Contact.SideX, SideY: sc.req.Contact.SideY, NoFloating: sc.req.Contact.NoFloating,
		}, prefs, weights, buildConstraints(sc.req.Constraints))
		return jf.PackAllCtx(sc.ctx, sc.items)
	}, false))

	// Order-search metaheuristics: decode orderings through the EMS decoder factory.
	reg("beam", selfManaged3D(func(sc *solveCtx) (pack.Result, error) {
		return offline.BeamSearch(sc.ctx, sc.items, decoder3D(sc), sc.req.beamOptions(sc.ctx)), nil
	}, false))
	reg("rr", selfManaged3D(func(sc *solveCtx) (pack.Result, error) {
		return offline.RuinRecreate(sc.ctx, sc.items, decoder3D(sc), searchOpts3D(sc)), nil
	}, false))
	reg("arr", selfManaged3D(func(sc *solveCtx) (pack.Result, error) {
		return offline.AdaptiveRuinRecreate(sc.ctx, sc.items, decoder3D(sc), searchOpts3D(sc)), nil
	}, false))
	reg("grasp", selfManaged3D(func(sc *solveCtx) (pack.Result, error) {
		return offline.GRASP(sc.ctx, sc.items, decoder3D(sc), searchOpts3D(sc)), nil
	}, false))

	// GBPP / lex: no post-pass; carry their extra result metadata.
	reg("gbpp", solveGBPP)
	reg("lex", solveLex)
	reg("strip", solveStrip3D)
	reg("knapsack", solveKnapsack3D)

	// auto: mirror autoCandidates so Pack and StreamPack pick the same winner.
	reg("auto", compact3D2(func(sc *solveCtx) (pack.Result, string, error) {
		gateSpec := d3.ContactSpec{Bottom: sc.req.Contact.Bottom, NoFloating: sc.req.Contact.NoFloating}
		stratF := func(algo string) pack.BinFactory {
			return constrainedFactory(d3.NewFactory(sc.bw, sc.bd, sc.bh, strat3DFor(algo, gateSpec)), sc.req.Constraints)
		}
		cands := []pack.OfflinePacker{
			offline.FirstFitDecreasing(sc.factory),
			offline.BestFitDecreasing(sc.factory),
			offline.NextFitDecreasing(sc.factory),
			offline.FirstFitDecreasing(stratF("blf")),
			offline.FirstFitDecreasing(stratF("ems")),
			offline.FirstFitDecreasing(stratF("fit")),
			offline.FirstFitDecreasing(stratF("heightmap")),
			offline.New("Layer", offline.DecreasingLayerHeight, online.FirstFit(stratF("layer"))),
		}
		if len(sc.req.Constraints) == 0 {
			cands = append(cands, d3.NewBlockPacker(sc.bw, sc.bd, sc.bh), d3.NewAssembler(sc.bw, sc.bd, sc.bh), d3.NewLAFFPacker(sc.bw, sc.bd, sc.bh))
		}
		p := meta.BestOf(cands...)
		r, e := packAllCtx(sc.ctx, p, sc.items)
		if fatal(e) {
			return r, p.Winner(), e
		}
		// On small combinatorial instances, escalate to the multi-objective seed-sweep:
		// the constructive race optimises bins, but a shuffled re-pack can match the bin
		// count while tightening compactness and reducing slosh.
		r, winner := sweepRefine3D(sc, r, p.Winner())
		return r, winner, e
	}))
}

// compact3D2 is compact3D for a solver that also yields a winning-packer name.
func compact3D2(run func(sc *solveCtx) (pack.Result, string, error)) solveFn {
	return func(sc *solveCtx) (pack.Result, solveMeta, error) {
		r, best, e := run(sc)
		if fatal(e) {
			return r, solveMeta{}, e
		}
		return finishCompact3D(sc, r), solveMeta{bestPacker: best}, nil
	}
}
