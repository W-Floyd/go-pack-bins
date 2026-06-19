package geometry

import "math"

// BBox2 is a 2-D axis-aligned bounding box.
type BBox2 struct {
	Min, Max Vec2
}

// NewBBox2 constructs a BBox2 from two corner points.
func NewBBox2(min, max Vec2) BBox2 { return BBox2{Min: min, Max: max} }

// W returns the width.
func (b BBox2) W() float64 { return b.Max.X - b.Min.X }

// H returns the height.
func (b BBox2) H() float64 { return b.Max.Y - b.Min.Y }

// Area returns W × H.
func (b BBox2) Area() float64 { return b.W() * b.H() }

// Overlaps reports whether two bounding boxes have a non-empty intersection.
func (b BBox2) Overlaps(o BBox2) bool {
	return b.Min.X < o.Max.X && b.Max.X > o.Min.X &&
		b.Min.Y < o.Max.Y && b.Max.Y > o.Min.Y
}

// Contains reports whether point p is inside (or on the boundary of) b.
func (b BBox2) Contains(p Vec2) bool {
	return p.X >= b.Min.X && p.X <= b.Max.X &&
		p.Y >= b.Min.Y && p.Y <= b.Max.Y
}

// BBox3 is a 3-D axis-aligned bounding box.
type BBox3 struct {
	Min, Max Vec3
}

// NewBBox3 constructs a BBox3 from two corner points.
func NewBBox3(min, max Vec3) BBox3 { return BBox3{Min: min, Max: max} }

// W returns the width (X extent).
func (b BBox3) W() float64 { return b.Max.X - b.Min.X }

// D returns the depth (Y extent).
func (b BBox3) D() float64 { return b.Max.Y - b.Min.Y }

// H returns the height (Z extent).
func (b BBox3) H() float64 { return b.Max.Z - b.Min.Z }

// Volume returns W × D × H.
func (b BBox3) Volume() float64 { return b.W() * b.D() * b.H() }

// Overlaps reports whether two bounding boxes have a non-empty intersection.
func (b BBox3) Overlaps(o BBox3) bool {
	return b.Min.X < o.Max.X && b.Max.X > o.Min.X &&
		b.Min.Y < o.Max.Y && b.Max.Y > o.Min.Y &&
		b.Min.Z < o.Max.Z && b.Max.Z > o.Min.Z
}

// Contains reports whether point p is inside (or on the boundary of) b.
func (b BBox3) Contains(p Vec3) bool {
	return p.X >= b.Min.X && p.X <= b.Max.X &&
		p.Y >= b.Min.Y && p.Y <= b.Max.Y &&
		p.Z >= b.Min.Z && p.Z <= b.Max.Z
}

// ContainsBox reports whether b fully encloses o.
func (b BBox3) ContainsBox(o BBox3) bool {
	return o.Min.X >= b.Min.X && o.Max.X <= b.Max.X &&
		o.Min.Y >= b.Min.Y && o.Max.Y <= b.Max.Y &&
		o.Min.Z >= b.Min.Z && o.Max.Z <= b.Max.Z
}

// Union returns the smallest BBox3 containing both b and o.
func (b BBox3) Union(o BBox3) BBox3 {
	return BBox3{
		Min: Vec3{
			X: math.Min(b.Min.X, o.Min.X),
			Y: math.Min(b.Min.Y, o.Min.Y),
			Z: math.Min(b.Min.Z, o.Min.Z),
		},
		Max: Vec3{
			X: math.Max(b.Max.X, o.Max.X),
			Y: math.Max(b.Max.Y, o.Max.Y),
			Z: math.Max(b.Max.Z, o.Max.Z),
		},
	}
}
