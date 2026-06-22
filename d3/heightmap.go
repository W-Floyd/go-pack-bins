package d3

import "math"

// Heightmap implements 3-D placement by the heightmap (a.k.a. skyline) method:
// it models the occupied volume as a top surface and drops each item onto the
// lowest spot it can rest. For a set of candidate (x,y) anchors — the bin origin
// plus the left and right edges of every placed box — it computes the item's
// resting height as the maximum top surface beneath its footprint, then chooses
// the placement with the lowest landing (ties broken back-most, then left-most).
//
// Unlike Bottom-Left-Fill, which tests discrete corner points and requires a
// supported corner, the heightmap lands an item on the highest obstacle under
// its whole footprint — so it can bridge across a gap (leaving a void beneath)
// when nothing else fits lower. It commits each placement in final position,
// which makes it suitable for incremental streaming, and it is the standard
// baseline in the online-3D-BPP literature.
type Heightmap struct {
	binW, binD, binH float64
	placed           []box
	usedVol          float64
	contact          ContactSpec
}

// NewHeightmap creates a heightmap strategy for a bin of the given dimensions.
func NewHeightmap(w, d, h float64) *Heightmap {
	return &Heightmap{binW: w, binD: d, binH: h}
}

// NewHeightmapStrategy matches Factory3D's strategy-constructor signature.
func NewHeightmapStrategy(w, d, h float64) PlacementStrategy3D {
	return NewHeightmap(w, d, h)
}

// NewHeightmapStrategyContact returns a Factory3D-compatible constructor that
// applies the spec's hard support gates (Bottom fraction and NoFloating).
// Lateral SideX/SideY targets are not used by the heightmap — those are handled
// by the separate compaction pass, exactly as for the BLF strategy.
func NewHeightmapStrategyContact(spec ContactSpec) func(w, d, h float64) PlacementStrategy3D {
	return func(w, d, h float64) PlacementStrategy3D {
		hm := NewHeightmap(w, d, h)
		hm.contact = spec
		return hm
	}
}

func (hm *Heightmap) Utilization() float64 {
	total := hm.binW * hm.binD * hm.binH
	if total == 0 {
		return 1
	}
	return hm.usedVol / total
}

func (hm *Heightmap) Remaining() float64 {
	return hm.binW*hm.binD*hm.binH - hm.usedVol
}

func (hm *Heightmap) TryInsert(orientations [][3]float64) (rx, ry, rz, rw, rd, rh float64, ok bool) {
	xs, ys := hm.anchors()
	bestSet := false
	var best box

	for _, o := range orientations {
		w, d, h := o[0], o[1], o[2]
		if w > hm.binW || d > hm.binD || h > hm.binH {
			continue
		}
		for _, x := range xs {
			if x+w > hm.binW+compactEps {
				continue
			}
			for _, y := range ys {
				if y+d > hm.binD+compactEps {
					continue
				}
				z := hm.restingHeight(x, y, w, d)
				if z+h > hm.binH+compactEps {
					continue
				}
				if hm.gated(x, y, z, w, d) {
					continue
				}
				c := box{x, y, z, w, d, h}
				if !bestSet || lowerLanding(c, best) {
					best, bestSet = c, true
				}
			}
		}
	}

	if !bestSet {
		return 0, 0, 0, 0, 0, 0, false
	}
	hm.placed = append(hm.placed, best)
	hm.usedVol += best.w * best.d * best.h
	return best.x, best.y, best.z, best.w, best.d, best.h, true
}

// anchors returns the candidate x and y coordinates to try: the bin origin plus
// the near and far edges of every placed box, clamped into the bin and
// deduplicated. Trying both edges lets items tuck against either side of an
// existing box rather than only past its far face.
func (hm *Heightmap) anchors() (xs, ys []float64) {
	xset := map[float64]struct{}{0: {}}
	yset := map[float64]struct{}{0: {}}
	for _, b := range hm.placed {
		for _, x := range [2]float64{b.x, b.x + b.w} {
			if x >= 0 && x < hm.binW {
				xset[x] = struct{}{}
			}
		}
		for _, y := range [2]float64{b.y, b.y + b.d} {
			if y >= 0 && y < hm.binD {
				yset[y] = struct{}{}
			}
		}
	}
	for x := range xset {
		xs = append(xs, x)
	}
	for y := range yset {
		ys = append(ys, y)
	}
	return xs, ys
}

// restingHeight is the height at which an item of footprint (w×d) at (x,y) comes
// to rest: the highest top face of any placed box its footprint overlaps, or the
// floor (0) if none. The item placed there cannot intersect any box, since it
// sits at or above every obstacle beneath it.
func (hm *Heightmap) restingHeight(x, y, w, d float64) float64 {
	z := 0.0
	for _, b := range hm.placed {
		if overlap1D(x, x+w, b.x, b.x+b.w) > compactEps &&
			overlap1D(y, y+d, b.y, b.y+b.d) > compactEps {
			if top := b.z + b.h; top > z {
				z = top
			}
		}
	}
	return z
}

// gated reports whether a placement fails the spec's hard support gates.
func (hm *Heightmap) gated(x, y, z, w, d float64) bool {
	if hm.contact.Bottom <= 0 && !hm.contact.NoFloating {
		return false
	}
	sf := footprintSupport(hm.placed, x, y, z, w, d)
	if sf < hm.contact.Bottom {
		return true
	}
	if hm.contact.NoFloating && sf <= compactEps {
		return true
	}
	return false
}

// lowerLanding prefers the lowest resting height (z), then back-most (y), then
// left-most (x) — gravity first, like the other strategies' tie-breaks.
func lowerLanding(c, best box) bool {
	if math.Abs(c.z-best.z) > compactEps {
		return c.z < best.z
	}
	if math.Abs(c.y-best.y) > compactEps {
		return c.y < best.y
	}
	return c.x < best.x
}

var _ PlacementStrategy3D = (*Heightmap)(nil)
