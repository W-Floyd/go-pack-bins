package d3

import "sort"

// VoidBox is an axis-aligned empty cuboid, in the same coordinate convention as
// placements (X along width, Y along depth, Z along height; W/D/H the extents).
type VoidBox struct {
	X, Y, Z float64
	W, D, H float64
}

// PlacedBox is one occupied region inside a bin (a placed item's bounding box).
type PlacedBox struct {
	X, Y, Z float64
	W, D, H float64
}

// InternalVoids returns the empty cuboids inside a binW×binD×binH container that
// are sealed off from every container wall by the given placed boxes — the
// trapped internal voids a packing inspector cares about (as opposed to open
// gaps that still reach a face).
//
// It is exact, not sampled: the box faces induce a coordinate-compressed grid in
// which every cell is wholly occupied or wholly empty, so there is no resolution
// loss. The empty cells are flood-filled inward from the six bin faces; whatever
// the fill cannot reach is enclosed, and those cells are merged greedily back
// into maximal cuboids. Cost is governed by the number of distinct box-face
// coordinates, not by any voxel resolution, so it is cheap for typical packings.
func InternalVoids(binW, binD, binH float64, boxes []PlacedBox) []VoidBox {
	if binW <= 0 || binD <= 0 || binH <= 0 {
		return nil
	}

	// Clamp boxes to the bin and collect the cut planes along each axis.
	clamp := func(v, hi float64) float64 {
		if v < 0 {
			return 0
		}
		if v > hi {
			return hi
		}
		return v
	}
	xset := map[float64]bool{0: true, binW: true}
	yset := map[float64]bool{0: true, binD: true}
	zset := map[float64]bool{0: true, binH: true}
	type clamped struct{ x0, x1, y0, y1, z0, z1 float64 }
	cb := make([]clamped, 0, len(boxes))
	for _, b := range boxes {
		c := clamped{
			x0: clamp(b.X, binW), x1: clamp(b.X+b.W, binW),
			y0: clamp(b.Y, binD), y1: clamp(b.Y+b.D, binD),
			z0: clamp(b.Z, binH), z1: clamp(b.Z+b.H, binH),
		}
		if c.x1 <= c.x0 || c.y1 <= c.y0 || c.z1 <= c.z0 {
			continue // degenerate after clamping
		}
		xset[c.x0], xset[c.x1] = true, true
		yset[c.y0], yset[c.y1] = true, true
		zset[c.z0], zset[c.z1] = true, true
		cb = append(cb, c)
	}

	xs, xIdx := sortedAxis(xset)
	ys, yIdx := sortedAxis(yset)
	zs, zIdx := sortedAxis(zset)
	nx, ny, nz := len(xs)-1, len(ys)-1, len(zs)-1
	if nx <= 0 || ny <= 0 || nz <= 0 {
		return nil
	}
	at := func(i, j, k int) int { return i + nx*(j+ny*k) }
	occ := make([]bool, nx*ny*nz)

	// Each box spans an exact range of cells (its faces are grid planes).
	for _, c := range cb {
		for k := zIdx[c.z0]; k < zIdx[c.z1]; k++ {
			for j := yIdx[c.y0]; j < yIdx[c.y1]; j++ {
				for i := xIdx[c.x0]; i < xIdx[c.x1]; i++ {
					occ[at(i, j, k)] = true
				}
			}
		}
	}

	// Flood-fill empty cells inward from the six faces (6-connectivity).
	reached := make([]bool, nx*ny*nz)
	stack := make([]int, 0, nx*ny+ny*nz+nx*nz)
	seed := func(i, j, k int) {
		if i < 0 || i >= nx || j < 0 || j >= ny || k < 0 || k >= nz {
			return
		}
		idx := at(i, j, k)
		if !occ[idx] && !reached[idx] {
			reached[idx] = true
			stack = append(stack, idx)
		}
	}
	for k := 0; k < nz; k++ {
		for j := 0; j < ny; j++ {
			seed(0, j, k)
			seed(nx-1, j, k)
		}
	}
	for k := 0; k < nz; k++ {
		for i := 0; i < nx; i++ {
			seed(i, 0, k)
			seed(i, ny-1, k)
		}
	}
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			seed(i, j, 0)
			seed(i, j, nz-1)
		}
	}
	for len(stack) > 0 {
		idx := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		i := idx % nx
		j := (idx / nx) % ny
		k := idx / (nx * ny)
		seed(i-1, j, k)
		seed(i+1, j, k)
		seed(i, j-1, k)
		seed(i, j+1, k)
		seed(i, j, k-1)
		seed(i, j, k+1)
	}

	// Enclosed = empty and unreached.
	enclosed := make([]bool, nx*ny*nz)
	any := false
	for idx := range enclosed {
		if !occ[idx] && !reached[idx] {
			enclosed[idx] = true
			any = true
		}
	}
	if !any {
		return nil
	}

	// Greedy merge: grow each unused seed cell along i, then j, then k.
	used := make([]bool, nx*ny*nz)
	var out []VoidBox
	for k := 0; k < nz; k++ {
		for j := 0; j < ny; j++ {
			for i := 0; i < nx; i++ {
				if !enclosed[at(i, j, k)] || used[at(i, j, k)] {
					continue
				}
				i1 := i
				for i1+1 < nx && enclosed[at(i1+1, j, k)] && !used[at(i1+1, j, k)] {
					i1++
				}
				j1 := j
				growJ := true
				for growJ && j1+1 < ny {
					for ii := i; ii <= i1; ii++ {
						if c := at(ii, j1+1, k); !enclosed[c] || used[c] {
							growJ = false
							break
						}
					}
					if growJ {
						j1++
					}
				}
				k1 := k
				growK := true
				for growK && k1+1 < nz {
					for jj := j; jj <= j1 && growK; jj++ {
						for ii := i; ii <= i1; ii++ {
							if c := at(ii, jj, k1+1); !enclosed[c] || used[c] {
								growK = false
								break
							}
						}
					}
					if growK {
						k1++
					}
				}
				for kk := k; kk <= k1; kk++ {
					for jj := j; jj <= j1; jj++ {
						for ii := i; ii <= i1; ii++ {
							used[at(ii, jj, kk)] = true
						}
					}
				}
				out = append(out, VoidBox{
					X: xs[i], Y: ys[j], Z: zs[k],
					W: xs[i1+1] - xs[i], D: ys[j1+1] - ys[j], H: zs[k1+1] - zs[k],
				})
			}
		}
	}
	return out
}

// sortedAxis returns the cut planes in ascending order plus a value→index map
// (exact, since lookups reuse the same clamped float values that were inserted).
func sortedAxis(set map[float64]bool) ([]float64, map[float64]int) {
	vals := make([]float64, 0, len(set))
	for v := range set {
		vals = append(vals, v)
	}
	sort.Float64s(vals)
	idx := make(map[float64]int, len(vals))
	for i, v := range vals {
		idx[v] = i
	}
	return vals, idx
}
