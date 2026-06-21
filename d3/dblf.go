package d3

import "math"

// DeepBottomLeft implements a deepest-bottom-left-fill 3-D placement strategy.
// Among feasible corner positions it chooses the one furthest back (min y), then
// lowest (min z), then left-most (min x), and requires every item to rest on the
// floor or the top of a placed box (nothing floats). This back-to-front, bottom-up
// filling produces layered packings that are visibly distinct from the
// height-first extreme-point strategy — useful as a second 3-D contender so the
// "auto" race shows genuine spatial variety, not just different bin selection.
type DeepBottomLeft struct {
	binW, binD, binH float64
	placed           []box
	usedVol          float64
}

// NewDeepBottomLeft creates a DBLF strategy for a bin of the given dimensions.
func NewDeepBottomLeft(w, d, h float64) *DeepBottomLeft {
	return &DeepBottomLeft{binW: w, binD: d, binH: h}
}

// NewDeepBottomLeftStrategy matches Factory3D's strategy-constructor signature.
func NewDeepBottomLeftStrategy(w, d, h float64) PlacementStrategy3D {
	return NewDeepBottomLeft(w, d, h)
}

func (s *DeepBottomLeft) Utilization() float64 {
	total := s.binW * s.binD * s.binH
	if total == 0 {
		return 1
	}
	return s.usedVol / total
}

func (s *DeepBottomLeft) Remaining() float64 {
	return s.binW*s.binD*s.binH - s.usedVol
}

func (s *DeepBottomLeft) TryInsert(orientations [][3]float64) (rx, ry, rz, rw, rd, rh float64, ok bool) {
	pts := s.corners()
	bestSet := false
	var best box

	for _, o := range orientations {
		w, d, h := o[0], o[1], o[2]
		if w > s.binW || d > s.binD || h > s.binH {
			continue
		}
		for _, p := range pts {
			x, y, z := p[0], p[1], p[2]
			if x+w > s.binW+compactEps || y+d > s.binD+compactEps || z+h > s.binH+compactEps {
				continue
			}
			if s.conflicts(x, y, z, w, d, h) || !s.supported(x, y, z, w, d) {
				continue
			}
			c := box{x, y, z, w, d, h}
			if !bestSet || s.better(c, best) {
				best, bestSet = c, true
			}
		}
	}
	if !bestSet {
		return 0, 0, 0, 0, 0, 0, false
	}
	s.placed = append(s.placed, best)
	s.usedVol += best.w * best.d * best.h
	return best.x, best.y, best.z, best.w, best.d, best.h, true
}

// corners returns candidate positions: the origin plus, for each placed box, the
// points just past its +x, +y and +z faces.
func (s *DeepBottomLeft) corners() [][3]float64 {
	pts := [][3]float64{{0, 0, 0}}
	for _, b := range s.placed {
		pts = append(pts,
			[3]float64{b.x + b.w, b.y, b.z},
			[3]float64{b.x, b.y + b.d, b.z},
			[3]float64{b.x, b.y, b.z + b.h},
		)
	}
	return pts
}

// better ranks candidates: deepest (min y), then bottom (min z), then left (min x).
func (s *DeepBottomLeft) better(a, b box) bool {
	if a.y != b.y {
		return a.y < b.y
	}
	if a.z != b.z {
		return a.z < b.z
	}
	return a.x < b.x
}

func (s *DeepBottomLeft) conflicts(x, y, z, w, d, h float64) bool {
	for _, b := range s.placed {
		if overlap1D(x, x+w, b.x, b.x+b.w) > compactEps &&
			overlap1D(y, y+d, b.y, b.y+b.d) > compactEps &&
			overlap1D(z, z+h, b.z, b.z+b.h) > compactEps {
			return true
		}
	}
	return false
}

// supported reports whether the box would rest on the floor or the top face of a
// placed box (positive footprint overlap) — DBLF never leaves a box floating.
func (s *DeepBottomLeft) supported(x, y, z, w, d float64) bool {
	if z <= compactEps {
		return true
	}
	for _, b := range s.placed {
		if math.Abs(b.z+b.h-z) <= compactEps &&
			overlap1D(x, x+w, b.x, b.x+b.w) > compactEps &&
			overlap1D(y, y+d, b.y, b.y+b.d) > compactEps {
			return true
		}
	}
	return false
}

var _ PlacementStrategy3D = (*DeepBottomLeft)(nil)
