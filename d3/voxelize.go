package d3

import (
	"math"

	"github.com/wfloyd/go-pack-bins/geometry"
)

// VoxelGrid is a 3-D boolean occupancy grid stored in a bit-packed array.
// Voxel (ix, iy, iz) maps to bit (iz*NX*NY + iy*NX + ix) in the cells slice.
type VoxelGrid struct {
	NX, NY, NZ int
	CellSize   float64
	Origin     geometry.Vec3
	cells      []uint64
}

func newVoxelGrid(nx, ny, nz int, cellSize float64, origin geometry.Vec3) *VoxelGrid {
	total := nx * ny * nz
	return &VoxelGrid{
		NX:       nx,
		NY:       ny,
		NZ:       nz,
		CellSize: cellSize,
		Origin:   origin,
		cells:    make([]uint64, (total+63)/64),
	}
}

func (g *VoxelGrid) index(x, y, z int) (word, bit int) {
	flat := z*g.NX*g.NY + y*g.NX + x
	return flat / 64, flat % 64
}

// Get reports whether voxel (x, y, z) is occupied.
func (g *VoxelGrid) Get(x, y, z int) bool {
	if x < 0 || x >= g.NX || y < 0 || y >= g.NY || z < 0 || z >= g.NZ {
		return false
	}
	w, b := g.index(x, y, z)
	return g.cells[w]&(1<<uint(b)) != 0
}

// Set marks voxel (x, y, z) as occupied.
func (g *VoxelGrid) Set(x, y, z int) {
	if x < 0 || x >= g.NX || y < 0 || y >= g.NY || z < 0 || z >= g.NZ {
		return
	}
	w, b := g.index(x, y, z)
	g.cells[w] |= 1 << uint(b)
}

// OccupiedCount returns the number of set cells.
func (g *VoxelGrid) OccupiedCount() int {
	n := 0
	for _, w := range g.cells {
		n += popcount(w)
	}
	return n
}

// IntersectsAt reports whether grid other, when offset by (ox, oy, oz) voxels
// relative to g's origin, shares any occupied cell with g.
func (g *VoxelGrid) IntersectsAt(other *VoxelGrid, ox, oy, oz int) bool {
	for z := 0; z < other.NZ; z++ {
		gz := z + oz
		if gz < 0 || gz >= g.NZ {
			continue
		}
		for y := 0; y < other.NY; y++ {
			gy := y + oy
			if gy < 0 || gy >= g.NY {
				continue
			}
			for x := 0; x < other.NX; x++ {
				gx := x + ox
				if gx < 0 || gx >= g.NX {
					continue
				}
				if other.Get(x, y, z) && g.Get(gx, gy, gz) {
					return true
				}
			}
		}
	}
	return false
}

// ContainedInAt reports whether every occupied cell of other, when offset by
// (ox, oy, oz) relative to g, is also occupied in g. Used to check that a
// solid item is fully inside a solid bin (bin's interior voxels must be a superset).
func (g *VoxelGrid) ContainedInAt(other *VoxelGrid, ox, oy, oz int) bool {
	for z := 0; z < other.NZ; z++ {
		gz := z + oz
		for y := 0; y < other.NY; y++ {
			gy := y + oy
			for x := 0; x < other.NX; x++ {
				if !other.Get(x, y, z) {
					continue
				}
				gx := x + ox
				if gx < 0 || gx >= g.NX || gy < 0 || gy >= g.NY || gz < 0 || gz >= g.NZ {
					return false
				}
				if !g.Get(gx, gy, gz) {
					return false
				}
			}
		}
	}
	return true
}

// voxelizeMesh rasterises the interior of a closed triangle mesh into a VoxelGrid.
// It uses a scanline approach: for each (x,y) column it casts a ray in +Z and
// fills cells between each pair of consecutive intersections.
func voxelizeMesh(ts []Triangle, bbox geometry.BBox3, cellSize float64) *VoxelGrid {
	nx := int(math.Ceil(bbox.W()/cellSize)) + 1
	ny := int(math.Ceil(bbox.D()/cellSize)) + 1
	nz := int(math.Ceil(bbox.H()/cellSize)) + 1
	g := newVoxelGrid(nx, ny, nz, cellSize, bbox.Min)

	for iy := 0; iy < ny; iy++ {
		for ix := 0; ix < nx; ix++ {
			wx := bbox.Min.X + (float64(ix)+0.5)*cellSize
			wy := bbox.Min.Y + (float64(iy)+0.5)*cellSize
			// Collect all z intersections of the column ray with the mesh.
			var hits []float64
			orig := geometry.Vec3{X: wx, Y: wy, Z: bbox.Min.Z - 1}
			for _, t := range ts {
				if tz, ok := columnRayTriangle(orig, t); ok {
					hits = append(hits, tz)
				}
			}
			sortFloat64s(hits)
			hits = deduplicateHits(hits)
			// Fill interior voxels between pairs of intersection points.
			for i := 0; i+1 < len(hits); i += 2 {
				zLo := hits[i]
				zHi := hits[i+1]
				izLo := int(math.Floor((zLo - bbox.Min.Z) / cellSize))
				izHi := int(math.Ceil((zHi-bbox.Min.Z)/cellSize)) - 1
				for iz := max0(izLo); iz <= min1(izHi, nz-1); iz++ {
					g.Set(ix, iy, iz)
				}
			}
		}
	}
	return g
}

// columnRayTriangle tests intersection of a +Z ray from orig with triangle t,
// returning the world-space z coordinate of the hit.
func columnRayTriangle(orig geometry.Vec3, t Triangle) (float64, bool) {
	const eps = 1e-9
	dir := geometry.Vec3{Z: 1}
	edge1 := t.B.Sub(t.A)
	edge2 := t.C.Sub(t.A)
	h := dir.Cross(edge2)
	a := edge1.Dot(h)
	if math.Abs(a) < eps {
		return 0, false
	}
	f := 1.0 / a
	s := orig.Sub(t.A)
	u := f * s.Dot(h)
	if u < 0 || u > 1 {
		return 0, false
	}
	q := s.Cross(edge1)
	v := f * dir.Dot(q)
	if v < 0 || u+v > 1 {
		return 0, false
	}
	tVal := f * edge2.Dot(q)
	if tVal < 0 {
		return 0, false
	}
	return orig.Z + tVal, true
}

// ─── small utilities ─────────────────────────────────────────────────────────

func popcount(x uint64) int {
	n := 0
	for x != 0 {
		n += int(x & 1)
		x >>= 1
	}
	return n
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func min1(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// deduplicateHits removes z values that are within eps of the previous one.
// This prevents double-counting when a ray passes through a shared mesh edge.
func deduplicateHits(hits []float64) []float64 {
	const eps = 1e-9
	if len(hits) == 0 {
		return hits
	}
	out := hits[:1]
	for i := 1; i < len(hits); i++ {
		if hits[i]-out[len(out)-1] > eps {
			out = append(out, hits[i])
		}
	}
	return out
}

func sortFloat64s(s []float64) {
	// insertion sort – lists are tiny (handful of intersections per column)
	for i := 1; i < len(s); i++ {
		v := s[i]
		j := i - 1
		for j >= 0 && s[j] > v {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = v
	}
}
