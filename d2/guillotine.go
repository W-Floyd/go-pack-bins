package d2

import "math"

// GuillotineSplit controls how a free rectangle is split after placement.
type GuillotineSplit int

const (
	// ShorterLeftover splits along the shorter remaining axis (default).
	ShorterLeftover GuillotineSplit = iota
	// LongerLeftover splits along the longer remaining axis.
	LongerLeftover
	// MinArea splits to minimise the area of the smaller leftover rectangle.
	MinArea
)

// Guillotine implements a guillotine bin-packing placement strategy.
// Each placement splits the chosen free rectangle into at most two sub-rectangles
// using a horizontal or vertical cut, preserving the "guillotine" constraint.
type Guillotine struct {
	binW, binH float64
	split      GuillotineSplit
	allowMerge bool
	free       []rect
	usedArea   float64
}

// NewGuillotine creates a Guillotine strategy for a bin of the given dimensions.
func NewGuillotine(binW, binH float64, split GuillotineSplit, merge bool) *Guillotine {
	return &Guillotine{
		binW:       binW,
		binH:       binH,
		split:      split,
		allowMerge: merge,
		free:       []rect{{0, 0, binW, binH}},
	}
}

// NewGuillotineDefault creates a Guillotine strategy using ShorterLeftover with merge.
func NewGuillotineDefault(w, h float64) PlacementStrategy2D {
	return NewGuillotine(w, h, ShorterLeftover, true)
}

func (g *Guillotine) Utilization() float64 {
	total := g.binW * g.binH
	if total == 0 {
		return 1
	}
	return g.usedArea / total
}

func (g *Guillotine) Remaining() float64 {
	return g.binW*g.binH - g.usedArea
}

func (g *Guillotine) TryInsert(w, h float64, allowRotate bool) (x, y float64, rotated bool, ok bool) {
	bestIdx := -1
	bestRotated := false
	bestArea := math.MaxFloat64

	for i, r := range g.free {
		if w <= r.w && h <= r.h {
			a := r.w * r.h
			if a < bestArea {
				bestArea = a
				bestIdx = i
				bestRotated = false
			}
		}
		if allowRotate && h <= r.w && w <= r.h {
			a := r.w * r.h
			if a < bestArea {
				bestArea = a
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

	r := g.free[bestIdx]
	px, py := r.x, r.y

	// Remove chosen rectangle and add at most two sub-rectangles (guillotine cut).
	g.free = append(g.free[:bestIdx], g.free[bestIdx+1:]...)
	leftovers := g.split2(r, placeW, placeH)
	g.free = append(g.free, leftovers...)

	if g.allowMerge {
		g.mergeFree()
	}

	g.usedArea += placeW * placeH
	return px, py, bestRotated, true
}

func (g *Guillotine) split2(r rect, pw, ph float64) []rect {
	dw := r.w - pw
	dh := r.h - ph

	var splitHoriz bool
	switch g.split {
	case ShorterLeftover:
		splitHoriz = dw <= dh
	case LongerLeftover:
		splitHoriz = dw > dh
	case MinArea:
		area1 := (r.w - pw) * r.h
		area2 := r.w * (r.h - ph)
		splitHoriz = area1 < area2
	}

	var result []rect
	if splitHoriz {
		// Top sub-rectangle spans full width.
		if dh > 0 {
			result = append(result, rect{r.x, r.y + ph, r.w, dh})
		}
		// Right sub-rectangle below the placed item.
		if dw > 0 {
			result = append(result, rect{r.x + pw, r.y, dw, ph})
		}
	} else {
		// Right sub-rectangle spans full height.
		if dw > 0 {
			result = append(result, rect{r.x + pw, r.y, dw, r.h})
		}
		// Top sub-rectangle to the left of the placed item.
		if dh > 0 {
			result = append(result, rect{r.x, r.y + ph, pw, dh})
		}
	}
	return result
}

// mergeFree merges pairs of free rectangles that share an edge and can be
// combined into a single larger rectangle (simple pairwise merge pass).
func (g *Guillotine) mergeFree() {
	for i := 0; i < len(g.free); i++ {
		for j := i + 1; j < len(g.free); j++ {
			a, b := g.free[i], g.free[j]
			// Same row, adjacent horizontally.
			if a.y == b.y && a.h == b.h {
				if a.x+a.w == b.x {
					g.free[i] = rect{a.x, a.y, a.w + b.w, a.h}
					g.free = append(g.free[:j], g.free[j+1:]...)
					j--
					continue
				}
				if b.x+b.w == a.x {
					g.free[i] = rect{b.x, a.y, a.w + b.w, a.h}
					g.free = append(g.free[:j], g.free[j+1:]...)
					j--
					continue
				}
			}
			// Same column, adjacent vertically.
			if a.x == b.x && a.w == b.w {
				if a.y+a.h == b.y {
					g.free[i] = rect{a.x, a.y, a.w, a.h + b.h}
					g.free = append(g.free[:j], g.free[j+1:]...)
					j--
					continue
				}
				if b.y+b.h == a.y {
					g.free[i] = rect{a.x, b.y, a.w, a.h + b.h}
					g.free = append(g.free[:j], g.free[j+1:]...)
					j--
					continue
				}
			}
		}
	}
}

// FreeRects returns the current list of free rectangles as [x, y, w, h] arrays.
func (g *Guillotine) FreeRects() [][4]float64 {
	out := make([][4]float64, len(g.free))
	for i, r := range g.free {
		out[i] = [4]float64{r.x, r.y, r.w, r.h}
	}
	return out
}

var _ PlacementStrategy2D = (*Guillotine)(nil)
