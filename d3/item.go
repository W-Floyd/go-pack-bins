// Package d3 provides 3-D box items, bins, and manifold solid support.
package d3

import "github.com/wfloyd/go-pack-bins/pack"

// Item3D is a 3-D box-shaped item.
// AllowRotate permits trying all 6 axis-aligned face orientations during placement.
type Item3D struct {
	id          string
	W, D, H     float64 // natural dimensions: width, depth, height
	AllowRotate bool
}

// NewItem creates a box item with the given dimensions.
func NewItem(id string, w, d, h float64, allowRotate bool) *Item3D {
	return &Item3D{id: id, W: w, D: d, H: h, AllowRotate: allowRotate}
}

func (i *Item3D) ID() string      { return i.id }
func (i *Item3D) Volume() float64 { return i.W * i.D * i.H }
func (i *Item3D) Dimensions() int { return 3 }

// Orientations returns the distinct (w, d, h) triplets obtainable by rotating
// the item around axis-aligned axes. Returns 1 if AllowRotate is false, else up to 6.
func (i *Item3D) Orientations() [][3]float64 {
	if !i.AllowRotate {
		return [][3]float64{{i.W, i.D, i.H}}
	}
	seen := map[[3]float64]bool{}
	dims := [3]float64{i.W, i.D, i.H}
	perms := [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	var result [][3]float64
	for _, p := range perms {
		k := [3]float64{dims[p[0]], dims[p[1]], dims[p[2]]}
		if !seen[k] {
			seen[k] = true
			result = append(result, k)
		}
	}
	return result
}

var _ pack.Item = (*Item3D)(nil)
