package d3

import "math"

// boxGrid is a uniform spatial hash over a bin's volume: each cell holds the
// indices (into a caller-held placed slice) of the boxes overlapping it. An
// overlap query then examines only the boxes registered in the cells the query
// region spans, instead of every placed box — turning the extreme-point
// conflict test from O(placed) to O(local) and so the whole pack from O(k³) to
// roughly O(k²).
//
// The grid is an acceleration only: cell size affects speed, never correctness.
// Two boxes overlap iff their AABBs intersect, and any point in that
// intersection falls in a cell both boxes register in, so a query spanning the
// region's cells can never miss an overlapper (it may examine a few extra).
type boxGrid struct {
	csx, csy, csz float64 // cell size per axis
	ncx, ncy, ncz int     // cell count per axis
	cells         [][]int32
}

// newBoxGrid builds a grid for a w×d×h bin, sizing cells to ~4 units (clamped to
// 1..64 cells per axis) so typical items span one or two cells.
func newBoxGrid(w, d, h float64) *boxGrid {
	axis := func(dim float64) (int, float64) {
		n := int(math.Round(dim / 4))
		if n < 1 {
			n = 1
		}
		if n > 64 {
			n = 64
		}
		cs := dim / float64(n)
		if cs <= 0 {
			cs = 1
		}
		return n, cs
	}
	ncx, csx := axis(w)
	ncy, csy := axis(d)
	ncz, csz := axis(h)
	return &boxGrid{
		csx: csx, csy: csy, csz: csz,
		ncx: ncx, ncy: ncy, ncz: ncz,
		cells: make([][]int32, ncx*ncy*ncz),
	}
}

func clampIdx(v, cs float64, n int) int {
	i := int(v / cs)
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// insert registers box index idx in every cell the box spans.
func (g *boxGrid) insert(idx int32, b box) {
	x0, x1 := clampIdx(b.x, g.csx, g.ncx), clampIdx(b.x+b.w, g.csx, g.ncx)
	y0, y1 := clampIdx(b.y, g.csy, g.ncy), clampIdx(b.y+b.d, g.csy, g.ncy)
	z0, z1 := clampIdx(b.z, g.csz, g.ncz), clampIdx(b.z+b.h, g.csz, g.ncz)
	for iz := z0; iz <= z1; iz++ {
		for iy := y0; iy <= y1; iy++ {
			base := (iz*g.ncy + iy) * g.ncx
			for ix := x0; ix <= x1; ix++ {
				c := base + ix
				g.cells[c] = append(g.cells[c], idx)
			}
		}
	}
}

// conflict reports whether any placed box strictly overlaps the query box. The
// open-interval test matches ExtremePoint.conflictsBrute exactly, so the grid is
// a drop-in accelerator. A box spanning several query cells may be tested more
// than once; that is bounded by the span and cheaper than dedup bookkeeping.
func (g *boxGrid) conflict(x, y, z, w, d, h float64, placed []box) bool {
	x0, x1 := clampIdx(x, g.csx, g.ncx), clampIdx(x+w, g.csx, g.ncx)
	y0, y1 := clampIdx(y, g.csy, g.ncy), clampIdx(y+d, g.csy, g.ncy)
	z0, z1 := clampIdx(z, g.csz, g.ncz), clampIdx(z+h, g.csz, g.ncz)
	for iz := z0; iz <= z1; iz++ {
		for iy := y0; iy <= y1; iy++ {
			base := (iz*g.ncy + iy) * g.ncx
			for ix := x0; ix <= x1; ix++ {
				for _, bi := range g.cells[base+ix] {
					b := placed[bi]
					if x < b.x+b.w && x+w > b.x &&
						y < b.y+b.d && y+d > b.y &&
						z < b.z+b.h && z+h > b.z {
						return true
					}
				}
			}
		}
	}
	return false
}
