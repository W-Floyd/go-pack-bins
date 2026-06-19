package d2

import "math"

// MaxRectsHeuristic selects which free rectangle to use when placing an item.
type MaxRectsHeuristic int

const (
	// BSSF — Best Short Side Fit (default, best empirical performance).
	BSSF MaxRectsHeuristic = iota
	// BLSF — Best Long Side Fit.
	BLSF
	// BAF — Best Area Fit.
	BAF
	// BottomLeft — choose the rectangle that places the item lowest (then leftmost).
	BottomLeft
)

// MaxRects implements the MAXRECTS algorithm by Jukka Jylänki (2010).
// It maintains a list of maximal free rectangles and places items using a
// configurable heuristic.
type MaxRects struct {
	binW, binH float64
	heuristic  MaxRectsHeuristic
	free       []rect  // maximal free rectangles
	usedArea   float64
}

type rect struct{ x, y, w, h float64 }

// NewMaxRects creates a MaxRects strategy for a bin of the given dimensions.
func NewMaxRects(binW, binH float64, h MaxRectsHeuristic) *MaxRects {
	return &MaxRects{
		binW:      binW,
		binH:      binH,
		heuristic: h,
		free:      []rect{{0, 0, binW, binH}},
	}
}

// NewMaxRectsDefault creates a MaxRects strategy using BSSF.
func NewMaxRectsDefault(w, h float64) PlacementStrategy2D {
	return NewMaxRects(w, h, BSSF)
}

func (m *MaxRects) Utilization() float64 {
	total := m.binW * m.binH
	if total == 0 {
		return 1
	}
	return m.usedArea / total
}

func (m *MaxRects) Remaining() float64 {
	return m.binW*m.binH - m.usedArea
}

func (m *MaxRects) TryInsert(w, h float64, allowRotate bool) (x, y float64, rotated bool, ok bool) {
	bestIdx := -1
	bestRotated := false
	bestScore := [2]float64{math.MaxFloat64, math.MaxFloat64}

	for i, r := range m.free {
		if w <= r.w && h <= r.h {
			s := m.score(r, w, h)
			if less(s, bestScore) {
				bestScore = s
				bestIdx = i
				bestRotated = false
			}
		}
		if allowRotate && h <= r.w && w <= r.h {
			s := m.score(r, h, w)
			if less(s, bestScore) {
				bestScore = s
				bestIdx = i
				bestRotated = true
			}
		}
	}

	if bestIdx < 0 {
		return 0, 0, false, false
	}

	placeW, placeH := w, h
	if bestRotated {
		placeW, placeH = h, w
	}
	r := m.free[bestIdx]
	placed := rect{r.x, r.y, placeW, placeH}
	m.splitFree(placed)
	m.pruneFree()
	m.usedArea += placeW * placeH
	return r.x, r.y, bestRotated, true
}

func (m *MaxRects) score(r rect, w, h float64) [2]float64 {
	switch m.heuristic {
	case BSSF:
		shortFit := math.Min(r.w-w, r.h-h)
		longFit := math.Max(r.w-w, r.h-h)
		return [2]float64{shortFit, longFit}
	case BLSF:
		longFit := math.Max(r.w-w, r.h-h)
		shortFit := math.Min(r.w-w, r.h-h)
		return [2]float64{longFit, shortFit}
	case BAF:
		areaFit := r.w*r.h - w*h
		shortFit := math.Min(r.w-w, r.h-h)
		return [2]float64{areaFit, shortFit}
	case BottomLeft:
		return [2]float64{r.y, r.x}
	default:
		return [2]float64{r.w*r.h - w*h, 0}
	}
}

func less(a, b [2]float64) bool {
	if a[0] != b[0] {
		return a[0] < b[0]
	}
	return a[1] < b[1]
}

// splitFree splits all free rectangles that overlap with placed.
func (m *MaxRects) splitFree(placed rect) {
	var next []rect
	for _, r := range m.free {
		if !overlaps(r, placed) {
			next = append(next, r)
			continue
		}
		// Left strip
		if placed.x > r.x {
			next = append(next, rect{r.x, r.y, placed.x - r.x, r.h})
		}
		// Right strip
		if placed.x+placed.w < r.x+r.w {
			next = append(next, rect{placed.x + placed.w, r.y, r.x + r.w - (placed.x + placed.w), r.h})
		}
		// Bottom strip
		if placed.y > r.y {
			next = append(next, rect{r.x, r.y, r.w, placed.y - r.y})
		}
		// Top strip
		if placed.y+placed.h < r.y+r.h {
			next = append(next, rect{r.x, placed.y + placed.h, r.w, r.y + r.h - (placed.y + placed.h)})
		}
	}
	m.free = next
}

// pruneFree removes any free rectangle that is completely contained in another.
func (m *MaxRects) pruneFree() {
	n := len(m.free)
	dominated := make([]bool, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i == j || dominated[j] {
				continue
			}
			if contains(m.free[j], m.free[i]) {
				dominated[i] = true
				break
			}
		}
	}
	kept := m.free[:0]
	for i, r := range m.free {
		if !dominated[i] {
			kept = append(kept, r)
		}
	}
	m.free = kept
}

func overlaps(a, b rect) bool {
	return a.x < b.x+b.w && a.x+a.w > b.x &&
		a.y < b.y+b.h && a.y+a.h > b.y
}

// contains reports whether b is entirely inside a.
func contains(a, b rect) bool {
	return b.x >= a.x && b.y >= a.y &&
		b.x+b.w <= a.x+a.w && b.y+b.h <= a.y+a.h
}

var _ PlacementStrategy2D = (*MaxRects)(nil)
