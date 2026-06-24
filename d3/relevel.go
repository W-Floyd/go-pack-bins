package d3

import (
	"math"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// reGrid is a coarse fixed-resolution heightmap over a bin's base. It exists to
// re-level a block pack's ragged top region cheaply: the per-box Heightmap costs
// O(boxes) per query (and O(boxes²) per scan), which is fine for a handful of
// final-layer leftovers but not for the thousand-plus boxes that make up the top
// few units of a large single-bin (strip / stress) pack. The grid answers a
// resting-height query in O(footprint cells) regardless of how many items are
// already placed, so re-laying the whole top region stays roughly linear.
//
// Cells are conservative: an item claims every cell its footprint touches (its
// rest height is the max surface over those cells, and it raises all of them), so
// two items can never be assigned overlapping space. For the integer-sided item
// palettes used here a unit grid is exact; for arbitrary floats it slightly
// over-claims at cell boundaries, trading a sliver of packing density for the
// guarantee of no overlap.
type reGrid struct {
	nx, ny     int
	cx, cy     float64 // cell size on each axis
	binW, binD float64
	binH       float64
	h          []float64 // surface height per cell, row-major (gx*ny + gy)
}

// asPlacements widens a slice of *Placement3D to the pack.Placement interface so
// it can be appended to a result's placement list.
func asPlacements(ps []*Placement3D) []pack.Placement {
	out := make([]pack.Placement, len(ps))
	for i, p := range ps {
		out[i] = p
	}
	return out
}

// reGridMaxCells caps the grid resolution per axis so a very large base does not
// blow up the cell count; the cell size grows instead (slightly coarser packing).
const reGridMaxCells = 160

func newReGrid(binW, binD, binH, floor float64) *reGrid {
	nx := int(math.Ceil(binW))
	ny := int(math.Ceil(binD))
	if nx > reGridMaxCells {
		nx = reGridMaxCells
	}
	if ny > reGridMaxCells {
		ny = reGridMaxCells
	}
	if nx < 1 {
		nx = 1
	}
	if ny < 1 {
		ny = 1
	}
	g := &reGrid{
		nx: nx, ny: ny,
		cx: binW / float64(nx), cy: binD / float64(ny),
		binW: binW, binD: binD, binH: binH,
		h: make([]float64, nx*ny),
	}
	for i := range g.h {
		g.h[i] = floor
	}
	return g
}

// cellRange returns the inclusive cell index span that footprint [a, a+span)
// touches on an axis of n cells of size cs, clamped into [0, n).
func cellRange(a, span, cs float64, n int) (lo, hi int) {
	lo = int(math.Floor((a + blockEps) / cs))
	hi = int(math.Ceil((a+span-blockEps)/cs)) - 1
	if lo < 0 {
		lo = 0
	}
	if hi >= n {
		hi = n - 1
	}
	if hi < lo {
		hi = lo
	}
	return
}

// rest is the resting height of footprint (w×d) at (x,y): the highest surface over
// the cells it touches.
func (g *reGrid) rest(x, y, w, d float64) float64 {
	lx, hx := cellRange(x, w, g.cx, g.nx)
	ly, hy := cellRange(y, d, g.cy, g.ny)
	z := 0.0
	for gx := lx; gx <= hx; gx++ {
		row := gx * g.ny
		for gy := ly; gy <= hy; gy++ {
			if v := g.h[row+gy]; v > z {
				z = v
			}
		}
	}
	return z
}

// occupy raises every cell the footprint touches to top.
func (g *reGrid) occupy(x, y, w, d, top float64) {
	lx, hx := cellRange(x, w, g.cx, g.nx)
	ly, hy := cellRange(y, d, g.cy, g.ny)
	for gx := lx; gx <= hx; gx++ {
		row := gx * g.ny
		for gy := ly; gy <= hy; gy++ {
			if top > g.h[row+gy] {
				g.h[row+gy] = top
			}
		}
	}
}

// peak is the highest surface cell.
func (g *reGrid) peak() float64 {
	m := 0.0
	for _, v := range g.h {
		if v > m {
			m = v
		}
	}
	return m
}

// gapBelow is the total empty space under the peak plane — the flatness measure:
// peak*cells − Σ surface. The lower, the tighter the surface hugs its peak (a
// perfectly flat surface scores 0). Used to keep a re-level only when it genuinely
// flattens, comparing like for like at the same grid resolution.
func (g *reGrid) gapBelow() float64 {
	pk, sum := g.peak(), 0.0
	for _, v := range g.h {
		sum += v
	}
	return pk*float64(len(g.h)) - sum
}

// place finds the lowest-top resting placement of an item (over its orientations)
// on the current surface, scanning cell-origin anchors. It minimises the resulting
// top (then centre of gravity, via lowerTop), so an item drops into the lowest
// part of the surface laid as flat as it can be. ok is false if none fits.
func (g *reGrid) place(orients [][3]float64) (box, bool) {
	var best box
	found := false
	bestTop := math.Inf(1)
	for _, o := range orients {
		w, d, h := o[0], o[1], o[2]
		if w > g.binW+blockEps || d > g.binD+blockEps {
			continue
		}
		for gx := 0; gx < g.nx; gx++ {
			x := float64(gx) * g.cx
			if x+w > g.binW+blockEps {
				break
			}
			row := gx * g.ny
			for gy := 0; gy < g.ny; gy++ {
				y := float64(gy) * g.cy
				if y+d > g.binD+blockEps {
					break
				}
				// The corner cell lower-bounds rest (which is the max over the whole
				// footprint), so if corner+h already exceeds the best top found, the
				// full footprint scan can only be worse — skip it without paying for
				// rest. This is the prune that makes the per-cell scan cheap.
				if found && g.h[row+gy]+h > bestTop+blockEps {
					continue
				}
				z := g.rest(x, y, w, d)
				if z+h > g.binH+blockEps {
					continue
				}
				c := box{x, y, z, w, d, h}
				if !found || lowerTop(c, best) {
					best, found, bestTop = c, true, z+h
				}
			}
		}
	}
	return best, found
}
