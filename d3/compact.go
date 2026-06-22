package d3

import "sort"

const compactEps = 1e-9

// overlap1D returns the length of overlap between intervals [a0,a1] and [b0,b1].
func overlap1D(a0, a1, b0, b1 float64) float64 {
	lo, hi := a0, a1
	if b0 > lo {
		lo = b0
	}
	if b1 < hi {
		hi = b1
	}
	if hi > lo {
		return hi - lo
	}
	return 0
}

// Settle applies a gravity drop to one bin's placements: in increasing-z order,
// each box is lowered until it rests on the floor or the highest top face beneath
// its footprint. This removes the floating gaps a layered packer can leave when a
// short item under a layer ceiling lets the next layer hang above empty space.
// Placements are mutated in place and must be non-overlapping (as the packers
// produce them); processing bottom-up keeps them non-overlapping — a box only ever
// drops onto a box that was already below it.
func Settle(ps []*Placement3D) {
	order := make([]*Placement3D, len(ps))
	copy(order, ps)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Z < order[j].Z })
	for i, b := range order {
		rest := 0.0
		for j := 0; j < i; j++ {
			a := order[j]
			if overlap1D(b.X, b.X+b.W, a.X, a.X+a.W) > compactEps &&
				overlap1D(b.Y, b.Y+b.D, a.Y, a.Y+a.D) > compactEps {
				if top := a.Z + a.H; top > rest && top <= b.Z+compactEps {
					rest = top // a sits below b and overlaps its footprint
				}
			}
		}
		b.Z = rest
	}
}

// Compact slides each placement toward the x=0 and/or y=0 walls (the lateral
// axes selected by doX/doY, leaving z untouched) until it contacts a neighbor or
// the wall, collapsing the gaps that let packed items "slosh". It is
// support-preserving: a box is never slid if another box rests on it (which
// would un-support that box), and an elevated box is never slid to a position
// where its bottom-face support would drop below max(minSupport, >0). So a load
// that was fully supported at placement stays fully supported. Placements are
// mutated in place; pass the items of a single bin.
func Compact(ps []*Placement3D, binW, binD, binH float64, doX, doY bool, minSupport float64) {
	thr := minSupport
	if thr < compactEps {
		thr = compactEps // never leave a box fully airborne, even with no explicit gate
	}
	// supportOK reports whether p's bottom face at (x,y) keeps fraction ≥ thr
	// resting on the floor or boxes whose top is flush with p.Z.
	supportOK := func(p *Placement3D, x, y float64) bool {
		if p.Z <= compactEps {
			return true // on the floor
		}
		area := p.W * p.D
		if area == 0 {
			return true
		}
		sup := 0.0
		for _, q := range ps {
			if q == p || q.Z+q.H < p.Z-compactEps || q.Z+q.H > p.Z+compactEps {
				continue
			}
			sup += overlap1D(x, x+p.W, q.X, q.X+q.W) * overlap1D(y, y+p.D, q.Y, q.Y+q.D)
		}
		return sup/area >= thr-compactEps
	}
	// hasRider reports whether another box rests on p's top face.
	hasRider := func(p *Placement3D) bool {
		for _, q := range ps {
			if q == p || q.Z < p.Z+p.H-compactEps || q.Z > p.Z+p.H+compactEps {
				continue
			}
			if overlap1D(p.X, p.X+p.W, q.X, q.X+q.W) > compactEps &&
				overlap1D(p.Y, p.Y+p.D, q.Y, q.Y+q.D) > compactEps {
				return true
			}
		}
		return false
	}

	for pass := 0; pass < 8; pass++ {
		moved := false

		if doX {
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].X < ps[j].X })
			for _, p := range ps {
				if hasRider(p) {
					continue
				}
				best := 0.0
				for _, q := range ps {
					if q != p &&
						overlap1D(p.Y, p.Y+p.D, q.Y, q.Y+q.D) > compactEps &&
						overlap1D(p.Z, p.Z+p.H, q.Z, q.Z+q.H) > compactEps &&
						q.X+q.W <= p.X+compactEps && q.X+q.W > best {
						best = q.X + q.W
					}
				}
				if best < p.X-compactEps && supportOK(p, best, p.Y) {
					p.X = best
					moved = true
				}
			}
		}

		if doY {
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].Y < ps[j].Y })
			for _, p := range ps {
				if hasRider(p) {
					continue
				}
				best := 0.0
				for _, q := range ps {
					if q != p &&
						overlap1D(p.X, p.X+p.W, q.X, q.X+q.W) > compactEps &&
						overlap1D(p.Z, p.Z+p.H, q.Z, q.Z+q.H) > compactEps &&
						q.Y+q.D <= p.Y+compactEps && q.Y+q.D > best {
						best = q.Y + q.D
					}
				}
				if best < p.Y-compactEps && supportOK(p, p.X, best) {
					p.Y = best
					moved = true
				}
			}
		}

		if !moved {
			break
		}
	}
}
