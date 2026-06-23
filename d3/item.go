// Package d3 provides 3-D box items, bins, and manifold solid support.
package d3

import "github.com/W-Floyd/go-pack-bins/pack"

// Item3D is a 3-D box-shaped item.
// AllowRotate permits trying all 6 axis-aligned face orientations during placement.
type Item3D struct {
	id          string
	W, D, H     float64 // natural dimensions: width, depth, height
	AllowRotate bool
	scalars     map[string]float64
	orient      [][3]float64 // distinct orientations, precomputed at construction
}

// NewItem creates a box item with the given dimensions.
func NewItem(id string, w, d, h float64, allowRotate bool) *Item3D {
	return &Item3D{id: id, W: w, D: d, H: h, AllowRotate: allowRotate,
		orient: computeOrientations(w, d, h, allowRotate)}
}

func (i *Item3D) ID() string      { return i.id }
func (i *Item3D) Volume() float64 { return i.W * i.D * i.H }
func (i *Item3D) Dimensions() int { return 3 }

// LayerHeight is the item's height when laid flat for layered packing: its
// smallest dimension if it may rotate, else its natural height. Used by
// offline.DecreasingLayerHeight to order items for the d3.LayerStack packer.
func (i *Item3D) LayerHeight() float64 {
	if !i.AllowRotate {
		return i.H
	}
	return min(i.W, i.D, i.H)
}

// Orientations returns the distinct (w, d, h) triplets obtainable by rotating
// the item around axis-aligned axes. Returns 1 if AllowRotate is false, else up
// to 6. The set is precomputed at construction and returned as a shared,
// read-only slice — callers must not mutate it. Precomputing (rather than lazy
// caching) keeps it safe to call concurrently on the same item, as meta.BestOf
// does when racing packers over a shared item set.
func (i *Item3D) Orientations() [][3]float64 {
	return i.orient
}

// computeOrientations builds the distinct axis-aligned orientations of a box.
func computeOrientations(w, d, h float64, allowRotate bool) [][3]float64 {
	if !allowRotate {
		return [][3]float64{{w, d, h}}
	}
	seen := map[[3]float64]bool{}
	dims := [3]float64{w, d, h}
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

// WithScalar attaches a named scalar value to the item and returns the item
// for chaining: d3.NewItem("box", 3, 3, 3, false).WithScalar("weight", 8.0)
func (i *Item3D) WithScalar(name string, value float64) *Item3D {
	if i.scalars == nil {
		i.scalars = make(map[string]float64)
	}
	i.scalars[name] = value
	return i
}

// Scalars returns a snapshot of all named scalar values on this item.
func (i *Item3D) Scalars() map[string]float64 {
	out := make(map[string]float64, len(i.scalars))
	for k, v := range i.scalars {
		out[k] = v
	}
	return out
}

var _ pack.Item = (*Item3D)(nil)
var _ pack.Scalar = (*Item3D)(nil)
