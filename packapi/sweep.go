package packapi

import (
	"context"
	"math"
	"math/rand"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// This file adds a lower-bound-gated, multi-objective seed-sweep for 3-D auto. The
// order-search metaheuristics (rr/arr) save a bin on combinatorial instances the
// one-shot constructive packers strand, but they optimise only (unplaced, bins).
// Once the bin count is at its lower bound there is still quality to win — among
// equal-bin packings, compactness and lateral stability ("slosh") vary. This sweep
// re-packs the items in many shuffled orders and keeps the lexicographically best
// by (unplaced ↓, bins ↓, compactness ↑, lateral free-play ↓), so it returns the
// tightest / least-sloshy packing at the fewest bins, not merely the first one that
// meets the count.

// sweepEps guards float comparisons of coordinates/extents.
const sweepEps = 1e-9

// quality3D scores a 3-D packing for the multi-objective sweep. All fields are
// "smaller is better": negCompact is the negated mean compactness so that a higher
// compactness sorts earlier, and slosh is mean lateral free-play (lower = tighter).
type quality3D struct {
	unplaced   int
	bins       int
	negCompact float64
	slosh      float64
}

// better reports whether a is a strictly better packing than b under the
// lexicographic objective: fewest unplaced, then fewest bins, then most compact,
// then (only when slosh is being optimised) least lateral free-play.
func (a quality3D) better(b quality3D, useSlosh bool) bool {
	if a.unplaced != b.unplaced {
		return a.unplaced < b.unplaced
	}
	if a.bins != b.bins {
		return a.bins < b.bins
	}
	if math.Abs(a.negCompact-b.negCompact) > sweepEps {
		return a.negCompact < b.negCompact
	}
	if useSlosh {
		return a.slosh < b.slosh
	}
	return false
}

// scoreQuality3D computes a packing's multi-objective score. slosh is only
// evaluated when useSlosh is set (it is an O(items²)-per-bin scan, and is
// meaningless unless anti-slosh is requested).
func scoreQuality3D(r pack.Result, useSlosh bool) quality3D {
	q := quality3D{unplaced: len(r.Unplaced), bins: r.BinsUsed(), negCompact: -compactness3D(r)}
	if useSlosh {
		q.slosh = meanLateralFreePlay3D(r)
	}
	return q
}

// compactness3D is the mean over bins of packed volume ÷ bounding-box volume — how
// void-free each bin's occupied envelope is (100 = solid). It mirrors
// MeanCompactnessPct but works directly on a pack.Result's *d3.Placement3D
// placements, so the sweep can score a candidate without converting to the
// transport response type.
func compactness3D(r pack.Result) float64 {
	type acc struct{ mnX, mnY, mnZ, mxX, mxY, mxZ, packed float64 }
	bins := map[string]*acc{}
	for _, p := range r.Placements {
		b, ok := p.(*d3.Placement3D)
		if !ok {
			continue
		}
		a := bins[b.BinID()]
		if a == nil {
			a = &acc{mnX: b.X, mnY: b.Y, mnZ: b.Z, mxX: b.X + b.W, mxY: b.Y + b.D, mxZ: b.Z + b.H}
			bins[b.BinID()] = a
		} else {
			a.mnX, a.mnY, a.mnZ = math.Min(a.mnX, b.X), math.Min(a.mnY, b.Y), math.Min(a.mnZ, b.Z)
			a.mxX, a.mxY, a.mxZ = math.Max(a.mxX, b.X+b.W), math.Max(a.mxY, b.Y+b.D), math.Max(a.mxZ, b.Z+b.H)
		}
		a.packed += b.W * b.D * b.H
	}
	total, n := 0.0, 0
	for _, a := range bins {
		if bbox := (a.mxX - a.mnX) * (a.mxY - a.mnY) * (a.mxZ - a.mnZ); bbox > 0 {
			total += 100 * a.packed / bbox
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return total / float64(n)
}

// meanLateralFreePlay3D measures sloshiness: per item, how far it could still slide
// toward −X and −Y before hitting a neighbour or the bin wall (x=0 / y=0), averaged
// over all items. Lower = items are wedged tight against walls and each other =
// less room to shift in transit. It is the quantity the lateral anti-slosh
// compaction pass drives toward zero, so scoring it lets the sweep prefer
// arrangements that are already stable. O(items²) per bin — used only for small
// instances under an anti-slosh request.
func meanLateralFreePlay3D(r pack.Result) float64 {
	byBin := map[string][]*d3.Placement3D{}
	for _, p := range r.Placements {
		if b, ok := p.(*d3.Placement3D); ok {
			byBin[b.BinID()] = append(byBin[b.BinID()], b)
		}
	}
	ov := func(a0, a1, b0, b1 float64) bool { return a0 < b1-sweepEps && b0 < a1-sweepEps }
	total, cnt := 0.0, 0
	for _, boxes := range byBin {
		for _, b := range boxes {
			leftLimit, backLimit := 0.0, 0.0 // walls at 0
			for _, c := range boxes {
				if c == b {
					continue
				}
				// nearest face to the left that blocks a −X slide (overlap in Y and Z)
				if ov(b.Y, b.Y+b.D, c.Y, c.Y+c.D) && ov(b.Z, b.Z+b.H, c.Z, c.Z+c.H) {
					if right := c.X + c.W; right <= b.X+sweepEps && right > leftLimit {
						leftLimit = right
					}
				}
				// nearest face behind that blocks a −Y slide (overlap in X and Z)
				if ov(b.X, b.X+b.W, c.X, c.X+c.W) && ov(b.Z, b.Z+b.H, c.Z, c.Z+c.H) {
					if back := c.Y + c.D; back <= b.Y+sweepEps && back > backLimit {
						backLimit = back
					}
				}
			}
			total += (b.X - leftLimit) + (b.Y - backLimit)
			cnt++
		}
	}
	if cnt == 0 {
		return 0
	}
	return total / float64(cnt)
}

// qualitySweep3D re-packs items in many shuffled orders through factory (First-Fit)
// and returns the lexicographically best packing by (unplaced, bins, compactness,
// slosh). It is deterministic (seeds 0..maxTries-1) and honours ctx as a deadline.
// ok is false if no order produced any placement. Unlike a bin-count-only search it
// does not stop at the first min-bin packing: it keeps sweeping to tighten quality.
func qualitySweep3D(ctx context.Context, factory pack.BinFactory, items []pack.Item, useSlosh bool, maxTries int) (pack.Result, quality3D, bool) {
	var best pack.Result
	var bestQ quality3D
	have := false
	for seed := 0; seed < maxTries; seed++ {
		if seed%32 == 0 && ctx.Err() != nil {
			break
		}
		rng := rand.New(rand.NewSource(int64(seed)))
		order := append([]pack.Item(nil), items...)
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		r := packOrder3D(factory, order)
		q := scoreQuality3D(r, useSlosh)
		if !have || q.better(bestQ, useSlosh) {
			best, bestQ, have = r, q, true
		}
	}
	return best, bestQ, have
}

// packOrder3D packs one ordering through factory with First-Fit (mirrors the
// search's packOrdering) and returns the result.
func packOrder3D(factory pack.BinFactory, order []pack.Item) pack.Result {
	p := online.FirstFit(factory)
	for _, it := range order {
		p.Pack(it)
	}
	return p.Result()
}
