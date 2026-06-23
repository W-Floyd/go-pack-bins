package d3

// ExtremePoint implements a 3-D box placement strategy based on the
// extreme-point method. An extreme point is any corner formed by the
// intersection of a box surface with the bin boundary or another placed item.
// Items are placed at the extreme point that minimises z (height), then y,
// then x (deepest-bottom-left-fill ordering).
// ContactSpec unifies vertical support and lateral anti-slosh as per-axis
// contact requirements on a box's faces. All fields are fractions in [0,1].
//
//   - Bottom is a HARD gate: at least this fraction of the −z (bottom) face must
//     rest on the floor or the tops of placed boxes. Enforced at placement time
//     because support always comes from below (already-placed) items.
//   - SideX / SideY are soft TARGETS on the x / y axes: lateral neighbours
//     usually arrive later, so these can't be gated. A positive target makes the
//     strategy prefer positions that press a box flush against existing
//     NEIGHBOURS, immobilising both and so reducing potential lateral motion
//     (slosh). Wall contact is deliberately excluded — a wall is not sticky, so a
//     box flush to one wall still slides toward the open far side and its total
//     free play is unchanged. A compaction pass then consolidates the gaps.
//   - NoFloating is a HARD boolean gate: every item must have some support
//     beneath it (rest on the floor or a box). It's the weaker form of Bottom
//     (Bottom>0 already implies it) for when you only want "nothing hangs in air".
type ContactSpec struct {
	Bottom     float64
	SideX      float64
	SideY      float64
	NoFloating bool
}

func (c ContactSpec) maximizesLateral() bool { return c.SideX > 0 || c.SideY > 0 }

type ExtremePoint struct {
	binW, binD, binH float64
	placed           []box
	usedVol          float64
	contact          ContactSpec

	grid  *boxGrid     // spatial index over placed for O(local) conflict tests
	epBuf [][3]float64 // reused candidate-point scratch (avoids per-insert realloc)
}

type box struct{ x, y, z, w, d, h float64 }

// NewExtremePoint creates an extreme-point placement strategy for a bin of the given dimensions.
func NewExtremePoint(binW, binD, binH float64) *ExtremePoint {
	return &ExtremePoint{binW: binW, binD: binD, binH: binH, grid: newBoxGrid(binW, binD, binH)}
}

// NewExtremePointStrategy is a convenience constructor matching Factory3D's signature.
func NewExtremePointStrategy(w, d, h float64) PlacementStrategy3D {
	return NewExtremePoint(w, d, h)
}

// NewExtremePointStrategyWithSupport returns a constructor enforcing a minimum
// bottom-support fraction. Shorthand for NewExtremePointStrategyContact with
// only ContactSpec.Bottom set.
func NewExtremePointStrategyWithSupport(frac float64) func(w, d, h float64) PlacementStrategy3D {
	return NewExtremePointStrategyContact(ContactSpec{Bottom: frac})
}

// NewExtremePointStrategyContact returns a Factory3D-compatible constructor that
// applies the given per-axis contact spec: a hard bottom-support gate plus,
// where SideX/SideY are set, a contact-maximizing placement preference
// (ties broken by lowest z, then y, then x).
func NewExtremePointStrategyContact(spec ContactSpec) func(w, d, h float64) PlacementStrategy3D {
	return func(w, d, h float64) PlacementStrategy3D {
		ep := NewExtremePoint(w, d, h)
		ep.contact = spec
		return ep
	}
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

// PeakHeight returns the highest top face (max z+h) of any placed box, i.e. the
// current stack height of the bin. Returns 0 for an empty bin.
func (ep *ExtremePoint) PeakHeight() float64 {
	peak := 0.0
	for _, b := range ep.placed {
		if top := b.z + b.h; top > peak {
			peak = top
		}
	}
	return peak
}

func (ep *ExtremePoint) TryInsert(orientations [][3]float64) (rx, ry, rz, rw, rd, rh float64, ok bool) {
	pts := ep.extremePoints()

	bestSet := false
	var best box

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
			if ep.contact.Bottom > 0 || ep.contact.NoFloating {
				sf := ep.supportFrac(x, y, z, w, d)
				if sf < ep.contact.Bottom {
					continue
				}
				if ep.contact.NoFloating && sf <= compactEps {
					continue // hanging in air
				}
			}
			c := box{x, y, z, w, d, h}
			if !bestSet || ep.preferred(c, best) {
				best = c
				bestSet = true
			}
		}
	}

	if !bestSet {
		return 0, 0, 0, 0, 0, 0, false
	}

	ep.addPlaced(box{best.x, best.y, best.z, best.w, best.d, best.h})
	return best.x, best.y, best.z, best.w, best.d, best.h, true
}

// addPlaced records a committed box in both the placed list and the spatial grid
// (keeping them in sync), and updates the used volume.
func (ep *ExtremePoint) addPlaced(b box) {
	ep.grid.insert(int32(len(ep.placed)), b)
	ep.placed = append(ep.placed, b)
	ep.usedVol += b.w * b.d * b.h
}

// Candidate is a feasible placement (one that already passes the support gate),
// exposed without committing so a joint multi-objective packer can score it. It
// carries the geometric features such a packer scores on.
type Candidate struct {
	X, Y, Z, W, D, H float64
	Support          float64 // bottom-face support fraction in [0,1]
	Lateral          float64 // weighted neighbour-contact score (anti-slosh) per the spec's SideX/SideY
}

// Candidates returns every feasible placement of an item (in any given
// orientation) at the current extreme points, without committing. Positions that
// fail the Bottom / NoFloating support gate are excluded — exactly the ones
// TryInsert would reject — so a caller can rank the rest under its own objective.
func (ep *ExtremePoint) Candidates(orientations [][3]float64) []Candidate {
	pts := ep.extremePoints()
	var out []Candidate
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
			sf := ep.supportFrac(x, y, z, w, d)
			if sf < ep.contact.Bottom {
				continue
			}
			if ep.contact.NoFloating && sf <= compactEps {
				continue
			}
			b := box{x, y, z, w, d, h}
			out = append(out, Candidate{X: x, Y: y, Z: z, W: w, D: d, H: h,
				Support: sf, Lateral: ep.lateralScore(b)})
		}
	}
	return out
}

// CommitCandidate places a candidate previously returned by Candidates, updating
// the strategy's occupied set and volume.
func (ep *ExtremePoint) CommitCandidate(c Candidate) {
	ep.addPlaced(box{c.X, c.Y, c.Z, c.W, c.D, c.H})
}

// extremePoints returns all candidate placement positions.
// The origin (0,0,0) is always a candidate.
func (ep *ExtremePoint) extremePoints() [][3]float64 {
	// Reuse the backing array across inserts. The result is fully consumed within
	// the calling TryInsert/Candidates before the next call rebuilds it, so the
	// reuse is safe and avoids regenerating an O(placed) slice every insert (the
	// packer's largest source of allocation).
	pts := append(ep.epBuf[:0], [3]float64{0, 0, 0})
	for _, b := range ep.placed {
		pts = append(pts,
			[3]float64{b.x + b.w, b.y, b.z},
			[3]float64{b.x, b.y + b.d, b.z},
			[3]float64{b.x, b.y, b.z + b.h},
		)
	}
	ep.epBuf = pts
	return pts
}

// supportFrac returns the fraction of the bottom face (x,y,z) w×d that is
// supported by the bin floor or the top faces of already-placed boxes.
// Because placed boxes never overlap, their top faces at the same z are also
// non-overlapping, so summing individual intersection areas is exact.
func (ep *ExtremePoint) supportFrac(x, y, z, w, d float64) float64 {
	if z == 0 {
		return 1.0
	}
	const eps = 1e-9
	footprint := w * d
	if footprint == 0 {
		return 1.0
	}
	supported := 0.0
	for _, b := range ep.placed {
		if b.z+b.h < z-eps || b.z+b.h > z+eps {
			continue
		}
		iw := min(x+w, b.x+b.w) - max(x, b.x)
		id := min(y+d, b.y+b.d) - max(y, b.y)
		if iw > 0 && id > 0 {
			supported += iw * id
		}
	}
	return supported / footprint
}

// conflicts reports whether placing a box at (x,y,z) with dimensions (w,d,h)
// overlaps any already-placed box. It delegates to the spatial grid, which
// examines only boxes near the query region; conflictsBrute is the linear
// reference it must agree with (asserted by the fuzz test).
func (ep *ExtremePoint) conflicts(x, y, z, w, d, h float64) bool {
	return ep.grid.conflict(x, y, z, w, d, h, ep.placed)
}

// conflictsBrute is the linear-scan overlap test the grid accelerates; retained
// as the equivalence oracle for the grid.
func (ep *ExtremePoint) conflictsBrute(x, y, z, w, d, h float64) bool {
	for _, b := range ep.placed {
		if x < b.x+b.w && x+w > b.x &&
			y < b.y+b.d && y+d > b.y &&
			z < b.z+b.h && z+h > b.z {
			return true
		}
	}
	return false
}

// preferred reports whether candidate c is preferred over the current best.
// With lateral contact targets, the higher weighted lateral-contact fraction
// wins (ties fall through); otherwise, and as the tie-break, lower z (height),
// then lower y (depth), then lower x (width).
func (ep *ExtremePoint) preferred(c, best box) bool {
	if ep.contact.maximizesLateral() {
		// More neighbour contact wins: pressing boxes face-to-face immobilises both,
		// which is what reduces potential lateral motion. Walls are excluded — a
		// wall is not sticky, so a box flush to one wall still slides toward the
		// open side (its total free play is unchanged by hugging the wall).
		cc, bc := ep.lateralScore(c), ep.lateralScore(best)
		if d := cc - bc; d > compactEps || d < -compactEps {
			return cc > bc
		}
	}
	if c.z != best.z {
		return c.z < best.z
	}
	if c.y != best.y {
		return c.y < best.y
	}
	return c.x < best.x
}

// lateralScore weights each axis's NEIGHBOUR-contact fraction by its target.
// Only contact with placed boxes counts — walls are excluded. A wall is not
// sticky: it bounds a box but the box can still slide along or away from it, and
// the per-box free play is the same wherever it sits in a channel, so wall
// contact does nothing to reduce slosh. Pressing two boxes face-to-face, by
// contrast, immobilises both — which is what actually lowers potential lateral
// motion. Higher score = more boxed-in by neighbours.
func (ep *ExtremePoint) lateralScore(b box) float64 {
	return ep.contact.SideX*ep.neighbourFrac(b, 0) + ep.contact.SideY*ep.neighbourFrac(b, 1)
}

// neighbourFrac is the fraction of b's two faces on the given axis (0=x, 1=y)
// pressed flush against placed boxes, in [0,1].
func (ep *ExtremePoint) neighbourFrac(b box, axis int) float64 {
	var faceArea float64
	if axis == 0 {
		faceArea = b.d * b.h
	} else {
		faceArea = b.w * b.h
	}
	if faceArea == 0 {
		return 0
	}
	return (ep.neighbourContact(b, axis, false) + ep.neighbourContact(b, axis, true)) / (2 * faceArea)
}

// neighbourContact is the area of b's low (high=false) or high (high=true) face
// on the axis that is flush against a placed box overlapping it in the other two
// axes. Walls contribute nothing.
func (ep *ExtremePoint) neighbourContact(b box, axis int, high bool) float64 {
	near := func(a, c float64) bool { return a-c < compactEps && a-c > -compactEps }
	var coord float64
	if axis == 0 {
		coord = b.x
		if high {
			coord = b.x + b.w
		}
	} else {
		coord = b.y
		if high {
			coord = b.y + b.d
		}
	}
	area := 0.0
	for _, q := range ep.placed {
		qFace := faceLow(q, axis)
		if !high {
			qFace = faceHigh(q, axis)
		}
		if !near(coord, qFace) {
			continue
		}
		if axis == 0 {
			area += overlap1D(b.y, b.y+b.d, q.y, q.y+q.d) * overlap1D(b.z, b.z+b.h, q.z, q.z+q.h)
		} else {
			area += overlap1D(b.x, b.x+b.w, q.x, q.x+q.w) * overlap1D(b.z, b.z+b.h, q.z, q.z+q.h)
		}
	}
	return area
}

func faceLow(b box, axis int) float64 {
	if axis == 0 {
		return b.x
	}
	return b.y
}

func faceHigh(b box, axis int) float64 {
	if axis == 0 {
		return b.x + b.w
	}
	return b.y + b.d
}

var _ PlacementStrategy3D = (*ExtremePoint)(nil)
