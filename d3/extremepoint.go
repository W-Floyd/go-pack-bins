package d3

// ExtremePoint implements a 3-D box placement strategy based on the
// extreme-point method. An extreme point is any corner formed by the
// intersection of a box surface with the bin boundary or another placed item.
// Items are placed at the extreme point that minimises z (height), then y,
// then x (deepest-bottom-left-fill ordering).
type ExtremePoint struct {
	binW, binD, binH float64
	placed           []box
	usedVol          float64
}

type box struct{ x, y, z, w, d, h float64 }

// NewExtremePoint creates an extreme-point placement strategy for a bin of the given dimensions.
func NewExtremePoint(binW, binD, binH float64) *ExtremePoint {
	return &ExtremePoint{binW: binW, binD: binD, binH: binH}
}

// NewExtremePointStrategy is a convenience constructor matching Factory3D's signature.
func NewExtremePointStrategy(w, d, h float64) PlacementStrategy3D {
	return NewExtremePoint(w, d, h)
}

func (ep *ExtremePoint) Utilization() float64 {
	total := ep.binW * ep.binD * ep.binH
	if total == 0 {
		return 1
	}
	return ep.usedVol / total
}

func (ep *ExtremePoint) Remaining() float64 {
	return ep.binW*ep.binD*ep.binH - ep.usedVol
}

func (ep *ExtremePoint) TryInsert(orientations [][3]float64) (rx, ry, rz, rw, rd, rh float64, ok bool) {
	pts := ep.extremePoints()

	type candidate struct {
		x, y, z, w, d, h float64
	}
	bestSet := false
	var best candidate

	for _, o := range orientations {
		w, d, h := o[0], o[1], o[2]
		if w > ep.binW || d > ep.binD || h > ep.binH {
			continue
		}
		for _, p := range pts {
			x, y, z := p[0], p[1], p[2]
			if x+w > ep.binW || y+d > ep.binD || z+h > ep.binH {
				continue
			}
			if ep.conflicts(x, y, z, w, d, h) {
				continue
			}
			c := candidate{x, y, z, w, d, h}
			if !bestSet || better(c, best) {
				best = c
				bestSet = true
			}
		}
	}

	if !bestSet {
		return 0, 0, 0, 0, 0, 0, false
	}

	ep.placed = append(ep.placed, box{best.x, best.y, best.z, best.w, best.d, best.h})
	ep.usedVol += best.w * best.d * best.h
	return best.x, best.y, best.z, best.w, best.d, best.h, true
}

// extremePoints returns all candidate placement positions.
// The origin (0,0,0) is always a candidate.
func (ep *ExtremePoint) extremePoints() [][3]float64 {
	pts := [][3]float64{{0, 0, 0}}
	for _, b := range ep.placed {
		pts = append(pts,
			[3]float64{b.x + b.w, b.y, b.z},
			[3]float64{b.x, b.y + b.d, b.z},
			[3]float64{b.x, b.y, b.z + b.h},
		)
	}
	return pts
}

// conflicts reports whether placing a box at (x,y,z) with dimensions (w,d,h)
// overlaps any already-placed box.
func (ep *ExtremePoint) conflicts(x, y, z, w, d, h float64) bool {
	for _, b := range ep.placed {
		if x < b.x+b.w && x+w > b.x &&
			y < b.y+b.d && y+d > b.y &&
			z < b.z+b.h && z+h > b.z {
			return true
		}
	}
	return false
}

// better returns true if candidate c is preferred over current best.
// Priority: lower z (height), then lower y (depth), then lower x (width).
func better(c, best struct{ x, y, z, w, d, h float64 }) bool {
	if c.z != best.z {
		return c.z < best.z
	}
	if c.y != best.y {
		return c.y < best.y
	}
	return c.x < best.x
}

var _ PlacementStrategy3D = (*ExtremePoint)(nil)
