package d3

import "math"

// FitPacker is a maximal-space 3-D placement strategy that selects placements by
// a *fitness score* rather than by lowest landing. It reuses the Empty-Maximal-
// Space machinery (the same free-volume set, splitting and pruning as EMS) but,
// among every feasible (orientation × empty space) candidate, commits the one
// whose box touches the most surface — bin walls plus the faces of already-placed
// boxes. Maximising contact wedges items into corners and flush against their
// neighbours, which leaves the fewest sealed gaps; it is the standard high-fill
// constructive criterion for the container-loading problem (the "maximal contact"
// / fitness-number heuristic; see ATTRIBUTION.md).
//
// It always respects gravity: a candidate is only considered if it rests on the
// bin floor or on the top faces of placed boxes, so the contact criterion can
// never wedge a box high against a wall or the ceiling with empty air beneath
// it. The opt-in ContactSpec gate (Bottom fraction / NoFloating) tightens this
// further but is not required for grounding.
//
// Cost per insert is the EMS space-maintenance O(n²) plus an O(spaces × placed)
// scan to score candidates — still a single fast pass, no search. Feed it items
// largest-first (the offline decreasing wrappers) for best results.
type FitPacker struct {
	*EmptyMaximalSpace
}

// NewFitPacker creates a contact-maximising maximal-space strategy for a bin.
func NewFitPacker(w, d, h float64) *FitPacker {
	return &FitPacker{EmptyMaximalSpace: NewEmptyMaximalSpace(w, d, h)}
}

// NewFitStrategy matches Factory3D's strategy-constructor signature.
func NewFitStrategy(w, d, h float64) PlacementStrategy3D {
	return NewFitPacker(w, d, h)
}

// NewFitStrategyContact returns a Factory3D-compatible constructor applying the
// spec's hard support gates (Bottom fraction and NoFloating), like EMS. Lateral
// SideX/SideY targets are left to the separate compaction pass.
func NewFitStrategyContact(spec ContactSpec) func(w, d, h float64) PlacementStrategy3D {
	return func(w, d, h float64) PlacementStrategy3D {
		f := NewFitPacker(w, d, h)
		f.contact = spec
		return f
	}
}

// TryInsert places the item at the candidate corner that maximises contact area,
// breaking ties toward the lowest, back-most, left-most position (gravity + corner).
func (f *FitPacker) TryInsert(orientations [][3]float64) (rx, ry, rz, rw, rd, rh float64, ok bool) {
	bestSet := false
	var best box
	var bestScore float64

	for _, o := range orientations {
		w, d, h := o[0], o[1], o[2]
		if w > f.binW || d > f.binD || h > f.binH {
			continue
		}
		for _, s := range f.spaces {
			if w > s.w+compactEps || d > s.d+compactEps || h > s.h+compactEps {
				continue
			}
			x, y, z := s.x, s.y, s.z // back-bottom-left corner of the space
			if f.gated(x, y, z, w, d) {
				continue
			}
			// Gravity: never float. The maximal-space set contains overhanging
			// spaces whose floor rests only partly over a placed box (the rest
			// over empty air); the contact criterion would happily wedge a box
			// into such a space high against a wall or the ceiling, leaving the
			// floor empty ("climbing"). Require every box to rest on the bin
			// floor or on the top faces of placed boxes, independent of the
			// opt-in support gate above.
			if footprintSupport(f.placed, x, y, z, w, d) <= compactEps {
				continue
			}
			c := box{x, y, z, w, d, h}
			score := f.contactScore(c)
			if !bestSet || betterFit(c, score, best, bestScore) {
				best, bestScore, bestSet = c, score, true
			}
		}
	}

	if !bestSet {
		return 0, 0, 0, 0, 0, 0, false
	}
	f.commit(best)
	return best.x, best.y, best.z, best.w, best.d, best.h, true
}

// contactScore sums the area of candidate box c's faces that lie flush against a
// bin wall or against a placed box. Placed boxes never overlap, so summing the
// per-box touching-face areas is exact.
func (f *FitPacker) contactScore(c box) float64 {
	s := 0.0
	if c.x <= compactEps {
		s += c.d * c.h
	}
	if c.x+c.w >= f.binW-compactEps {
		s += c.d * c.h
	}
	if c.y <= compactEps {
		s += c.w * c.h
	}
	if c.y+c.d >= f.binD-compactEps {
		s += c.w * c.h
	}
	if c.z <= compactEps {
		s += c.w * c.d
	}
	if c.z+c.h >= f.binH-compactEps {
		s += c.w * c.d
	}
	for _, b := range f.placed {
		// Faces touch when one box's plane coincides with the other's and the
		// footprints overlap on the remaining two axes.
		if math.Abs(c.x+c.w-b.x) <= compactEps || math.Abs(b.x+b.w-c.x) <= compactEps {
			if oy, oz := overlap1D(c.y, c.y+c.d, b.y, b.y+b.d), overlap1D(c.z, c.z+c.h, b.z, b.z+b.h); oy > 0 && oz > 0 {
				s += oy * oz
			}
		}
		if math.Abs(c.y+c.d-b.y) <= compactEps || math.Abs(b.y+b.d-c.y) <= compactEps {
			if ox, oz := overlap1D(c.x, c.x+c.w, b.x, b.x+b.w), overlap1D(c.z, c.z+c.h, b.z, b.z+b.h); ox > 0 && oz > 0 {
				s += ox * oz
			}
		}
		if math.Abs(c.z+c.h-b.z) <= compactEps || math.Abs(b.z+b.h-c.z) <= compactEps {
			if ox, oy := overlap1D(c.x, c.x+c.w, b.x, b.x+b.w), overlap1D(c.y, c.y+c.d, b.y, b.y+b.d); ox > 0 && oy > 0 {
				s += ox * oy
			}
		}
	}
	return s
}

// betterFit prefers the higher contact score; ties resolve toward the lowest
// landing (z), then back-most (y), then left-most (x) — gravity then corner.
func betterFit(c box, score float64, best box, bestScore float64) bool {
	if math.Abs(score-bestScore) > compactEps {
		return score > bestScore
	}
	if math.Abs(c.z-best.z) > compactEps {
		return c.z < best.z
	}
	if math.Abs(c.y-best.y) > compactEps {
		return c.y < best.y
	}
	return c.x < best.x
}

var _ PlacementStrategy3D = (*FitPacker)(nil)
