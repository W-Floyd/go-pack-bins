package d2

import (
	"math"
	"sort"
)

const skyEps = 1e-9

// Skyline implements the Skyline (bottom-left) 2-D placement strategy — the
// third member of the standard 2-D trio alongside MaxRects and Guillotine. It
// tracks the upper contour of placed items as a left-to-right sequence of
// horizontal segments covering [0, binW], and places each rectangle at the
// lowest feasible resting height, ties broken left-most. It is typically the
// fastest of the three with competitive packing density.
type Skyline struct {
	binW, binH float64
	segs       []skySeg // contour, sorted by x, contiguous, covering [0, binW]
	usedArea   float64
}

// skySeg is a horizontal contour segment spanning [x, x+w] at height y.
type skySeg struct{ x, w, y float64 }

// NewSkyline creates a Skyline strategy for a bin of the given dimensions.
func NewSkyline(binW, binH float64) *Skyline {
	return &Skyline{binW: binW, binH: binH, segs: []skySeg{{0, binW, 0}}}
}

// NewSkylineDefault matches Factory2D's strategy-constructor signature.
func NewSkylineDefault(w, h float64) PlacementStrategy2D { return NewSkyline(w, h) }

func (s *Skyline) Utilization() float64 {
	total := s.binW * s.binH
	if total == 0 {
		return 1
	}
	return s.usedArea / total
}

func (s *Skyline) Remaining() float64 { return s.binW*s.binH - s.usedArea }

// levelFor returns the resting height of a rectangle of width w anchored at the
// left edge of segment i: the maximum contour height across the span it covers.
// ok is false if the rectangle would run past the right wall.
func (s *Skyline) levelFor(i int, w float64) (y float64, ok bool) {
	x := s.segs[i].x
	if x+w > s.binW+skyEps {
		return 0, false
	}
	remaining := w
	for j := i; j < len(s.segs) && remaining > skyEps; j++ {
		if s.segs[j].y > y {
			y = s.segs[j].y
		}
		remaining -= s.segs[j].w
	}
	return y, true
}

func (s *Skyline) TryInsert(w, h float64, allowRotate bool) (x, y float64, rotated bool, ok bool) {
	bestY, bestX := math.MaxFloat64, math.MaxFloat64
	var bw, bh float64
	bestRot, found := false, false

	try := func(pw, ph float64, rot bool) {
		for i := range s.segs {
			ly, fits := s.levelFor(i, pw)
			if !fits || ly+ph > s.binH+skyEps {
				continue
			}
			px := s.segs[i].x
			if ly < bestY-skyEps || (math.Abs(ly-bestY) <= skyEps && px < bestX-skyEps) {
				bestY, bestX, bw, bh, bestRot, found = ly, px, pw, ph, rot, true
			}
		}
	}
	try(w, h, false)
	if allowRotate && w != h {
		try(h, w, true)
	}
	if !found {
		return 0, 0, false, false
	}
	s.place(bestX, bestY, bw, bh)
	s.usedArea += bw * bh
	return bestX, bestY, bestRot, true
}

// place raises the contour over [x, x+w] to height y+h, splitting the segments
// at the boundaries and merging equal-height neighbours.
func (s *Skyline) place(x, y, w, h float64) {
	x2 := x + w
	top := y + h
	var out []skySeg
	for _, seg := range s.segs {
		segEnd := seg.x + seg.w
		if segEnd <= x+skyEps || seg.x >= x2-skyEps {
			out = append(out, seg) // fully outside the placed span
			continue
		}
		if seg.x < x-skyEps { // left remainder keeps old height
			out = append(out, skySeg{seg.x, x - seg.x, seg.y})
		}
		if segEnd > x2+skyEps { // right remainder keeps old height
			out = append(out, skySeg{x2, segEnd - x2, seg.y})
		}
		// the covered middle is replaced by the raised segment, added once below
	}
	out = append(out, skySeg{x, w, top})
	sort.Slice(out, func(i, j int) bool { return out[i].x < out[j].x })
	s.segs = mergeSegs(out)
}

// mergeSegs coalesces adjacent segments of equal height.
func mergeSegs(segs []skySeg) []skySeg {
	if len(segs) == 0 {
		return segs
	}
	merged := segs[:1]
	for _, seg := range segs[1:] {
		last := &merged[len(merged)-1]
		if math.Abs(last.y-seg.y) <= skyEps {
			last.w += seg.w
		} else {
			merged = append(merged, seg)
		}
	}
	return merged
}

var _ PlacementStrategy2D = (*Skyline)(nil)
