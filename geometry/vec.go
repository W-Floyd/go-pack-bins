// Package geometry provides geometric primitives shared by the d2 and d3 packages.
package geometry

import "math"

// Vec2 is a 2-D vector or point.
type Vec2 struct{ X, Y float64 }

func (v Vec2) Add(u Vec2) Vec2      { return Vec2{v.X + u.X, v.Y + u.Y} }
func (v Vec2) Sub(u Vec2) Vec2      { return Vec2{v.X - u.X, v.Y - u.Y} }
func (v Vec2) Scale(s float64) Vec2 { return Vec2{v.X * s, v.Y * s} }
func (v Vec2) Dot(u Vec2) float64   { return v.X*u.X + v.Y*u.Y }
func (v Vec2) Len() float64         { return math.Sqrt(v.X*v.X + v.Y*v.Y) }

// Vec3 is a 3-D vector or point.
type Vec3 struct{ X, Y, Z float64 }

func (v Vec3) Add(u Vec3) Vec3      { return Vec3{v.X + u.X, v.Y + u.Y, v.Z + u.Z} }
func (v Vec3) Sub(u Vec3) Vec3      { return Vec3{v.X - u.X, v.Y - u.Y, v.Z - u.Z} }
func (v Vec3) Scale(s float64) Vec3 { return Vec3{v.X * s, v.Y * s, v.Z * s} }
func (v Vec3) Dot(u Vec3) float64   { return v.X*u.X + v.Y*u.Y + v.Z*u.Z }
func (v Vec3) Len() float64         { return math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z) }

func (v Vec3) Cross(u Vec3) Vec3 {
	return Vec3{
		v.Y*u.Z - v.Z*u.Y,
		v.Z*u.X - v.X*u.Z,
		v.X*u.Y - v.Y*u.X,
	}
}

func (v Vec3) Normalize() Vec3 {
	l := v.Len()
	if l == 0 {
		return Vec3{}
	}
	return v.Scale(1 / l)
}

// Mat3x3 is a row-major 3×3 matrix used for 3-D rotations.
type Mat3x3 [3][3]float64

// Identity3x3 returns the identity matrix.
func Identity3x3() Mat3x3 {
	return Mat3x3{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}
}

// MulVec multiplies the matrix by a column vector.
func (m Mat3x3) MulVec(v Vec3) Vec3 {
	return Vec3{
		m[0][0]*v.X + m[0][1]*v.Y + m[0][2]*v.Z,
		m[1][0]*v.X + m[1][1]*v.Y + m[1][2]*v.Z,
		m[2][0]*v.X + m[2][1]*v.Y + m[2][2]*v.Z,
	}
}

// Mul returns the product of two matrices.
func (m Mat3x3) Mul(n Mat3x3) Mat3x3 {
	var r Mat3x3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				r[i][j] += m[i][k] * n[k][j]
			}
		}
	}
	return r
}

// RotX returns a rotation matrix around the X axis by angle θ (radians).
func RotX(theta float64) Mat3x3 {
	c, s := math.Cos(theta), math.Sin(theta)
	return Mat3x3{{1, 0, 0}, {0, c, -s}, {0, s, c}}
}

// RotY returns a rotation matrix around the Y axis by angle θ (radians).
func RotY(theta float64) Mat3x3 {
	c, s := math.Cos(theta), math.Sin(theta)
	return Mat3x3{{c, 0, s}, {0, 1, 0}, {-s, 0, c}}
}

// RotZ returns a rotation matrix around the Z axis by angle θ (radians).
func RotZ(theta float64) Mat3x3 {
	c, s := math.Cos(theta), math.Sin(theta)
	return Mat3x3{{c, -s, 0}, {s, c, 0}, {0, 0, 1}}
}

// AxisAlignedOrientations returns all 24 distinct axis-aligned rotation
// matrices (multiples of 90° around the three coordinate axes).
func AxisAlignedOrientations() []Mat3x3 {
	half := math.Pi / 2
	bases := []Mat3x3{Identity3x3()}
	for _, rx := range []float64{0, half, math.Pi, -half} {
		for _, ry := range []float64{0, half, math.Pi, -half} {
			for _, rz := range []float64{0, half, math.Pi, -half} {
				m := RotX(rx).Mul(RotY(ry)).Mul(RotZ(rz))
				if !contained(bases, m) {
					bases = append(bases, m)
				}
			}
		}
	}
	return bases
}

func contained(ms []Mat3x3, m Mat3x3) bool {
	const eps = 1e-9
	for _, n := range ms {
		match := true
		for i := 0; i < 3 && match; i++ {
			for j := 0; j < 3 && match; j++ {
				if math.Abs(m[i][j]-n[i][j]) > eps {
					match = false
				}
			}
		}
		if match {
			return true
		}
	}
	return false
}
