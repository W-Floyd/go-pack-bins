package d2

import "sort"

const compactEps = 1e-9

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

// Compact slides each placement toward the x=0 and/or y=0 walls (selected by
// doX/doY) until it contacts a neighbor or the wall, collapsing the gaps between
// packed rectangles. Placements are mutated in place; pass the items of a single bin.
func Compact(ps []*Placement2D, binW, binH float64, doX, doY bool) {
	for pass := 0; pass < 8; pass++ {
		moved := false

		if doX {
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].X < ps[j].X })
			for _, p := range ps {
				best := 0.0
				for _, q := range ps {
					if q == p {
						continue
					}
					if overlap1D(p.Y, p.Y+p.H, q.Y, q.Y+q.H) > compactEps &&
						q.X+q.W <= p.X+compactEps && q.X+q.W > best {
						best = q.X + q.W
					}
				}
				if best < p.X-compactEps {
					p.X = best
					moved = true
				}
			}
		}

		if doY {
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].Y < ps[j].Y })
			for _, p := range ps {
				best := 0.0
				for _, q := range ps {
					if q == p {
						continue
					}
					if overlap1D(p.X, p.X+p.W, q.X, q.X+q.W) > compactEps &&
						q.Y+q.H <= p.Y+compactEps && q.Y+q.H > best {
						best = q.Y + q.H
					}
				}
				if best < p.Y-compactEps {
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
