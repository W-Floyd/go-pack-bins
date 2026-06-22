package d3

import "github.com/W-Floyd/go-pack-bins/d2"

// LayerStack is a 3-D placement strategy that builds horizontal layers from the
// floor up. Each item is laid flat — its smallest dimension vertical if rotation
// is allowed, otherwise its natural height — to maximise floor coverage (the LAFF
// orientation). The first item placed in a layer fixes that layer's height; later
// items are packed into the layer's floor with the 2-D MaxRects engine until none
// fits, then a new layer opens directly above. When no further layer fits under
// the container ceiling the bin is full.
//
// Feed items tallest-flat-first (offline.DecreasingLayerHeight) so each layer's
// seed is its tallest member and everything packed beneath its ceiling is no
// taller. Unlike d3.LAFF — which solves a whole container in one call — LayerStack
// commits one placement per TryInsert through the ordinary online packer, so a
// layered solve streams its progress like FF/BLF/EMS do.
type LayerStack struct {
	binW, binD, binH float64
	z0               float64   // base height of the current (open) layer
	layerH           float64   // height of the current layer; 0 ⇒ none open yet
	floor            *d2.Bin2D // 2-D packer for the current layer's floor
	usedVol          float64
}

// NewLayerStack creates a layer-stacking strategy for a bin of the given dimensions.
func NewLayerStack(w, d, h float64) *LayerStack {
	return &LayerStack{binW: w, binD: d, binH: h}
}

// NewLayerStackStrategy matches Factory3D's strategy-constructor signature.
func NewLayerStackStrategy(w, d, h float64) PlacementStrategy3D {
	return NewLayerStack(w, d, h)
}

func (s *LayerStack) Utilization() float64 {
	total := s.binW * s.binD * s.binH
	if total == 0 {
		return 1
	}
	return s.usedVol / total
}

func (s *LayerStack) Remaining() float64 { return s.binW*s.binD*s.binH - s.usedVol }

func (s *LayerStack) TryInsert(orientations [][3]float64) (rx, ry, rz, rw, rd, rh float64, ok bool) {
	fw, fd, fh, rotateFoot := flatOrientation(orientations)
	if fh > s.binH+compactEps {
		return
	}

	// First choice: the current layer, if the item is short enough for it. d2's
	// TryPlace only mutates the floor on success, so a miss here leaves state intact.
	if s.floor != nil && fh <= s.layerH+compactEps {
		if pl, good := tryFloor(s.floor, fw, fd, rotateFoot); good {
			return s.commit(pl, fh)
		}
	}

	// Otherwise open a new layer directly above the current one. Try the placement
	// on a fresh floor first and only adopt it on success, so a footprint that
	// can't fit even an empty floor (an item only a non-flat orientation could
	// seat) leaves the strategy unchanged and bubbles up as unplaced.
	newZ0 := 0.0
	if s.floor != nil {
		newZ0 = s.z0 + s.layerH
	}
	if newZ0+fh > s.binH+compactEps {
		return // no vertical room for another layer
	}
	floor := d2.NewBin("layer", s.binW, s.binD, d2.NewMaxRectsDefault(s.binW, s.binD))
	pl, good := tryFloor(floor, fw, fd, rotateFoot)
	if !good {
		return
	}
	s.z0, s.layerH, s.floor = newZ0, fh, floor
	return s.commit(pl, fh)
}

// commit records the placed volume and maps the 2-D floor placement (X,Y are the
// floor position; W,H are the possibly in-plane-rotated footprint extents) into
// the 3-D result at the current layer height.
func (s *LayerStack) commit(pl *d2.Placement2D, fh float64) (rx, ry, rz, rw, rd, rh float64, ok bool) {
	s.usedVol += pl.W * pl.H * fh
	return pl.X, pl.Y, s.z0, pl.W, pl.H, fh, true
}

// tryFloor attempts to seat a footprint in a layer's 2-D floor, allowing in-plane
// rotation when the item may rotate.
func tryFloor(floor *d2.Bin2D, fw, fd float64, rotate bool) (*d2.Placement2D, bool) {
	p, err := floor.TryPlace(d2.NewItem("f", fw, fd, rotate))
	if err != nil {
		return nil, false
	}
	pl, ok := p.(*d2.Placement2D)
	return pl, ok
}

// flatOrientation picks the laid-flat orientation: the one whose vertical extent
// (h) is smallest, so the largest face becomes the footprint. rotateFoot reports
// whether the footprint may still rotate in-plane (i.e. the item is rotatable).
func flatOrientation(orientations [][3]float64) (fw, fd, fh float64, rotateFoot bool) {
	best := orientations[0]
	for _, o := range orientations[1:] {
		if o[2] < best[2] {
			best = o
		}
	}
	return best[0], best[1], best[2], len(orientations) > 1
}

var _ PlacementStrategy3D = (*LayerStack)(nil)
