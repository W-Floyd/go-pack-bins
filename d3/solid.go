package d3

import (
	"errors"
	"math"

	"github.com/W-Floyd/go-pack-bins/geometry"
)

// Solid represents a closed, orientable 3-D manifold solid.
// Implementations must describe watertight surfaces (no boundary edges).
type Solid interface {
	// Triangles returns the triangulated surface mesh.
	// The returned slice must not be mutated by the caller.
	Triangles() []Triangle

	// AABB returns the axis-aligned bounding box of the solid.
	AABB() geometry.BBox3

	// Volume returns the signed volume (positive for outward normals consistent
	// with the right-hand rule; computed via the divergence theorem).
	Volume() float64

	// Contains reports whether world-space point p is strictly inside the solid.
	// Uses ray-casting along the +Z axis; result is undefined on the surface itself.
	Contains(p geometry.Vec3) bool

	// Voxelize returns a VoxelGrid at the given world-space cell size.
	// Results are cached keyed by cellSize to avoid repeated work.
	Voxelize(cellSize float64) *VoxelGrid

	// Transform returns a new Solid with the given rotation applied followed by
	// the given translation. The original solid is not modified.
	Transform(rot geometry.Mat3x3, translate geometry.Vec3) Solid
}

// Triangle is a single face of a triangulated mesh.
// Vertices are listed in counter-clockwise order when viewed from outside (outward normal).
type Triangle struct {
	A, B, C geometry.Vec3
}

// Normal returns the un-normalised face normal (cross product of edges).
func (t Triangle) Normal() geometry.Vec3 {
	ab := t.B.Sub(t.A)
	ac := t.C.Sub(t.A)
	return ab.Cross(ac)
}

// AABB returns the bounding box of the triangle.
func (t Triangle) AABB() geometry.BBox3 {
	min := geometry.Vec3{
		X: math.Min(t.A.X, math.Min(t.B.X, t.C.X)),
		Y: math.Min(t.A.Y, math.Min(t.B.Y, t.C.Y)),
		Z: math.Min(t.A.Z, math.Min(t.B.Z, t.C.Z)),
	}
	max := geometry.Vec3{
		X: math.Max(t.A.X, math.Max(t.B.X, t.C.X)),
		Y: math.Max(t.A.Y, math.Max(t.B.Y, t.C.Y)),
		Z: math.Max(t.A.Z, math.Max(t.B.Z, t.C.Z)),
	}
	return geometry.NewBBox3(min, max)
}

// MeshSolid is the canonical implementation of Solid backed by a triangle list.
// It validates closure (every edge appears exactly twice) on construction.
type MeshSolid struct {
	triangles []Triangle
	bbox      geometry.BBox3
	vol       float64
	voxCache  map[float64]*VoxelGrid
}

// NewMeshSolid constructs a MeshSolid.
// Returns ErrOpenMesh if boundary edges are detected (mesh is not closed).
func NewMeshSolid(triangles []Triangle) (*MeshSolid, error) {
	if err := validateClosed(triangles); err != nil {
		return nil, err
	}
	m := &MeshSolid{
		triangles: make([]Triangle, len(triangles)),
		voxCache:  make(map[float64]*VoxelGrid),
	}
	copy(m.triangles, triangles)
	m.bbox = computeBBox(m.triangles)
	m.vol = computeVolume(m.triangles)
	return m, nil
}

// ErrOpenMesh is returned when a mesh has boundary (non-manifold) edges.
var ErrOpenMesh = errors.New("d3: mesh is not closed (boundary edges detected)")

func (m *MeshSolid) Triangles() []Triangle    { return m.triangles }
func (m *MeshSolid) AABB() geometry.BBox3     { return m.bbox }
func (m *MeshSolid) Volume() float64          { return math.Abs(m.vol) }

func (m *MeshSolid) Contains(p geometry.Vec3) bool {
	return raycastContains(m.triangles, p)
}

func (m *MeshSolid) Voxelize(cellSize float64) *VoxelGrid {
	if g, ok := m.voxCache[cellSize]; ok {
		return g
	}
	g := voxelizeMesh(m.triangles, m.bbox, cellSize)
	m.voxCache[cellSize] = g
	return g
}

func (m *MeshSolid) Transform(rot geometry.Mat3x3, translate geometry.Vec3) Solid {
	ts := make([]Triangle, len(m.triangles))
	for i, t := range m.triangles {
		ts[i] = Triangle{
			A: rot.MulVec(t.A).Add(translate),
			B: rot.MulVec(t.B).Add(translate),
			C: rot.MulVec(t.C).Add(translate),
		}
	}
	return &MeshSolid{
		triangles: ts,
		bbox:      computeBBox(ts),
		vol:       m.vol,
		voxCache:  make(map[float64]*VoxelGrid),
	}
}

// BoxSolid is an axis-aligned box that implements Solid exactly (no voxelization needed).
type BoxSolid struct {
	bbox geometry.BBox3
}

// NewBoxSolid creates a BoxSolid with the given bounding box.
func NewBoxSolid(min, max geometry.Vec3) *BoxSolid {
	return &BoxSolid{bbox: geometry.NewBBox3(min, max)}
}

// NewBoxSolidWDH creates a BoxSolid with its min-corner at the origin.
func NewBoxSolidWDH(w, d, h float64) *BoxSolid {
	return NewBoxSolid(geometry.Vec3{}, geometry.Vec3{X: w, Y: d, Z: h})
}

func (b *BoxSolid) Triangles() []Triangle {
	min, max := b.bbox.Min, b.bbox.Max
	// 12 triangles for 6 faces.
	v := func(x, y, z float64) geometry.Vec3 { return geometry.Vec3{X: x, Y: y, Z: z} }
	return []Triangle{
		// Bottom (z=min)
		{A: v(min.X, min.Y, min.Z), B: v(max.X, min.Y, min.Z), C: v(max.X, max.Y, min.Z)},
		{A: v(min.X, min.Y, min.Z), B: v(max.X, max.Y, min.Z), C: v(min.X, max.Y, min.Z)},
		// Top (z=max)
		{A: v(min.X, min.Y, max.Z), B: v(max.X, max.Y, max.Z), C: v(max.X, min.Y, max.Z)},
		{A: v(min.X, min.Y, max.Z), B: v(min.X, max.Y, max.Z), C: v(max.X, max.Y, max.Z)},
		// Front (y=min)
		{A: v(min.X, min.Y, min.Z), B: v(min.X, min.Y, max.Z), C: v(max.X, min.Y, max.Z)},
		{A: v(min.X, min.Y, min.Z), B: v(max.X, min.Y, max.Z), C: v(max.X, min.Y, min.Z)},
		// Back (y=max)
		{A: v(min.X, max.Y, min.Z), B: v(max.X, max.Y, max.Z), C: v(min.X, max.Y, max.Z)},
		{A: v(min.X, max.Y, min.Z), B: v(max.X, max.Y, min.Z), C: v(max.X, max.Y, max.Z)},
		// Left (x=min)
		{A: v(min.X, min.Y, min.Z), B: v(min.X, max.Y, min.Z), C: v(min.X, max.Y, max.Z)},
		{A: v(min.X, min.Y, min.Z), B: v(min.X, max.Y, max.Z), C: v(min.X, min.Y, max.Z)},
		// Right (x=max)
		{A: v(max.X, min.Y, min.Z), B: v(max.X, max.Y, max.Z), C: v(max.X, max.Y, min.Z)},
		{A: v(max.X, min.Y, min.Z), B: v(max.X, min.Y, max.Z), C: v(max.X, max.Y, max.Z)},
	}
}

func (b *BoxSolid) AABB() geometry.BBox3    { return b.bbox }
func (b *BoxSolid) Volume() float64         { return b.bbox.Volume() }

func (b *BoxSolid) Contains(p geometry.Vec3) bool {
	return p.X > b.bbox.Min.X && p.X < b.bbox.Max.X &&
		p.Y > b.bbox.Min.Y && p.Y < b.bbox.Max.Y &&
		p.Z > b.bbox.Min.Z && p.Z < b.bbox.Max.Z
}

func (b *BoxSolid) Voxelize(cellSize float64) *VoxelGrid {
	return voxelizeMesh(b.Triangles(), b.bbox, cellSize)
}

func (b *BoxSolid) Transform(rot geometry.Mat3x3, translate geometry.Vec3) Solid {
	ts := b.Triangles()
	for i, t := range ts {
		ts[i] = Triangle{
			A: rot.MulVec(t.A).Add(translate),
			B: rot.MulVec(t.B).Add(translate),
			C: rot.MulVec(t.C).Add(translate),
		}
	}
	bbox := computeBBox(ts)
	return &MeshSolid{triangles: ts, bbox: bbox, vol: bbox.Volume(), voxCache: make(map[float64]*VoxelGrid)}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func computeBBox(ts []Triangle) geometry.BBox3 {
	if len(ts) == 0 {
		return geometry.BBox3{}
	}
	min := ts[0].A
	max := ts[0].A
	for _, t := range ts {
		for _, v := range [3]geometry.Vec3{t.A, t.B, t.C} {
			if v.X < min.X { min.X = v.X }
			if v.Y < min.Y { min.Y = v.Y }
			if v.Z < min.Z { min.Z = v.Z }
			if v.X > max.X { max.X = v.X }
			if v.Y > max.Y { max.Y = v.Y }
			if v.Z > max.Z { max.Z = v.Z }
		}
	}
	return geometry.NewBBox3(min, max)
}

// computeVolume uses the divergence theorem:  V = (1/6) Σ (a · (b × c)).
func computeVolume(ts []Triangle) float64 {
	var vol float64
	for _, t := range ts {
		vol += t.A.Dot(t.B.Cross(t.C))
	}
	return vol / 6
}

type edgeKey struct{ ax, ay, az, bx, by, bz float64 }

func validateClosed(ts []Triangle) error {
	edgeCount := make(map[edgeKey]int)
	for _, t := range ts {
		verts := [3]geometry.Vec3{t.A, t.B, t.C}
		for i := 0; i < 3; i++ {
			a, b := verts[i], verts[(i+1)%3]
			k := edgeKey{a.X, a.Y, a.Z, b.X, b.Y, b.Z}
			edgeCount[k]++
		}
	}
	for k, cnt := range edgeCount {
		rev := edgeKey{k.bx, k.by, k.bz, k.ax, k.ay, k.az}
		if edgeCount[rev] != cnt {
			return ErrOpenMesh
		}
	}
	return nil
}

// raycastContains tests point p against the mesh using a ray cast along +Z.
// Counts Möller–Trumbore intersections; odd count ⇒ inside.
func raycastContains(ts []Triangle, p geometry.Vec3) bool {
	count := 0
	for _, t := range ts {
		if rayTriangleIntersect(p, t) {
			count++
		}
	}
	return count%2 == 1
}

// rayTriangleIntersect tests whether the ray from p in the +Z direction
// intersects triangle t at a positive t value (Möller–Trumbore).
func rayTriangleIntersect(orig geometry.Vec3, t Triangle) bool {
	const eps = 1e-9
	dir := geometry.Vec3{Z: 1}
	edge1 := t.B.Sub(t.A)
	edge2 := t.C.Sub(t.A)
	h := dir.Cross(edge2)
	a := edge1.Dot(h)
	if a > -eps && a < eps {
		return false
	}
	f := 1.0 / a
	s := orig.Sub(t.A)
	u := f * s.Dot(h)
	if u < 0 || u > 1 {
		return false
	}
	q := s.Cross(edge1)
	v := f * dir.Dot(q)
	if v < 0 || u+v > 1 {
		return false
	}
	tVal := f * edge2.Dot(q)
	return tVal > eps
}
