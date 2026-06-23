package d3

import "math"

// BottomLeftFill implements the Bottom-Left-Fill 3-D placement strategy. Among
// feasible corner positions it chooses the lowest (min z, gravity-first), then
// left-most (min x), then deepest (min y), and requires every item to rest on
// the floor or the top of a placed box (nothing floats). Filling the floor
// before stacking — and breaking ties toward the front-left — gives packings
// distinct from the extreme-point strategy, whose ties resolve toward the back
// (min y) instead. Useful as a second 3-D contender so the "auto" race shows
// genuine spatial variety, not just different bin selection.
type BottomLeftFill struct {
	binW, binD, binH float64
	placed           []box
	usedVol          float64

	grid   *boxGrid     // spatial index over placed for O(local) conflict/support
	cnrBuf [][3]float64 // reused candidate-corner scratch
}

// NewBottomLeftFill creates a BLF strategy for a bin of the given dimensions.
func NewBottomLeftFill(w, d, h float64) *BottomLeftFill {
	return &BottomLeftFill{binW: w, binD: d, binH: h, grid: newBoxGrid(w, d, h)}
}

// NewBottomLeftFillStrategy matches Factory3D's strategy-constructor signature.
func NewBottomLeftFillStrategy(w, d, h float64) PlacementStrategy3D {
	return NewBottomLeftFill(w, d, h)
}

func (s *BottomLeftFill) Utilization() float64 {
	total := s.binW * s.binD * s.binH
	if total == 0 {
		return 1
	}
	return s.usedVol / total
}

func (s *BottomLeftFill) Remaining() float64 {
	return s.binW*s.binD*s.binH - s.usedVol
}

func (s *BottomLeftFill) TryInsert(orientations [][3]float64) (rx, ry, rz, rw, rd, rh float64, ok bool) {
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
	s.grid.insert(int32(len(s.placed)), best)
	s.placed = append(s.placed, best)
	s.usedVol += best.w * best.d * best.h
	return best.x, best.y, best.z, best.w, best.d, best.h, true
}

// corners returns candidate positions: the origin plus, for each placed box, the
// points just past its +x, +y and +z faces. The backing array is reused across
// inserts (the result is consumed within the calling TryInsert before the next
// rebuild), avoiding an O(placed) allocation every insert.
func (s *BottomLeftFill) corners() [][3]float64 {
	pts := append(s.cnrBuf[:0], [3]float64{0, 0, 0})
	for _, b := range s.placed {
		pts = append(pts,
			[3]float64{b.x + b.w, b.y, b.z},
			[3]float64{b.x, b.y + b.d, b.z},
			[3]float64{b.x, b.y, b.z + b.h},
		)
	}
	s.cnrBuf = pts
	return pts
}

// better ranks candidates bottom-first (gravity): lowest z, then left-most x,
// then deepest y. With the support requirement this fills the floor before
// stacking, distinct from extreme-point's bottom-then-back (z, y, x) order.
func (s *BottomLeftFill) better(a, b box) bool {
	if a.z != b.z {
		return a.z < b.z
	}
	if a.x != b.x {
		return a.x < b.x
	}
	return a.y < b.y
}

func (s *BottomLeftFill) conflicts(x, y, z, w, d, h float64) bool {
	return s.grid.anyNear(x, y, z, w, d, h, s.placed, func(b box) bool {
		return overlap1D(x, x+w, b.x, b.x+b.w) > compactEps &&
			overlap1D(y, y+d, b.y, b.y+b.d) > compactEps &&
			overlap1D(z, z+h, b.z, b.z+b.h) > compactEps
	})
}

// supported reports whether the box would rest on the floor or the top face of a
// placed box (positive footprint overlap) — BLF never leaves a box floating. A
// supporting box's top face is at z, so it registers in the grid's z-cell at z;
// a thin (h=0) query there finds the candidates.
func (s *BottomLeftFill) supported(x, y, z, w, d float64) bool {
	if z <= compactEps {
		return true
	}
	return s.grid.anyNear(x, y, z, w, d, 0, s.placed, func(b box) bool {
		return math.Abs(b.z+b.h-z) <= compactEps &&
			overlap1D(x, x+w, b.x, b.x+b.w) > compactEps &&
			overlap1D(y, y+d, b.y, b.y+b.d) > compactEps
	})
}

var _ PlacementStrategy3D = (*BottomLeftFill)(nil)
