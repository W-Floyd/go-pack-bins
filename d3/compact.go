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

// Compact slides each placement toward the x=0 and/or y=0 walls (the lateral
// axes selected by doX/doY, leaving z untouched so vertical support is
// preserved) until it contacts a neighbor or the wall. This collapses the
// lateral gaps that let packed items "slosh". Placements are mutated in place;
// pass the items of a single bin.
func Compact(ps []*Placement3D, binW, binD, binH float64, doX, doY bool) {
	for pass := 0; pass < 8; pass++ {
		moved := false

		// Slide toward x=0: a box is blocked by neighbors that overlap it in y and
		// z and sit to its left. Process left-to-right so blockers settle first.
		if doX {
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].X < ps[j].X })
			for _, p := range ps {
				best := 0.0
				for _, q := range ps {
					if q == p {
						continue
					}
					if overlap1D(p.Y, p.Y+p.D, q.Y, q.Y+q.D) > compactEps &&
						overlap1D(p.Z, p.Z+p.H, q.Z, q.Z+q.H) > compactEps &&
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

		// Slide toward y=0: blocked by neighbors overlapping in x and z to its front.
		if doY {
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].Y < ps[j].Y })
			for _, p := range ps {
				best := 0.0
				for _, q := range ps {
					if q == p {
						continue
					}
					if overlap1D(p.X, p.X+p.W, q.X, q.X+q.W) > compactEps &&
						overlap1D(p.Z, p.Z+p.H, q.Z, q.Z+q.H) > compactEps &&
						q.Y+q.D <= p.Y+compactEps && q.Y+q.D > best {
						best = q.Y + q.D
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
