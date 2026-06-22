package d3

import "testing"

// fillExceptCenter builds the 26 unit cells of a 3×3×3 bin, leaving the centre
// cell (1,1,1) empty and fully enclosed.
func fillExceptCenter() []PlacedBox {
	var boxes []PlacedBox
	for k := 0; k < 3; k++ {
		for j := 0; j < 3; j++ {
			for i := 0; i < 3; i++ {
				if i == 1 && j == 1 && k == 1 {
					continue
				}
				boxes = append(boxes, PlacedBox{
					X: float64(i), Y: float64(j), Z: float64(k), W: 1, D: 1, H: 1,
				})
			}
		}
	}
	return boxes
}

func TestInternalVoidsEnclosed(t *testing.T) {
	voids := InternalVoids(3, 3, 3, fillExceptCenter())
	if len(voids) != 1 {
		t.Fatalf("want 1 enclosed void, got %d: %+v", len(voids), voids)
	}
	v := voids[0]
	if v.X != 1 || v.Y != 1 || v.Z != 1 || v.W != 1 || v.D != 1 || v.H != 1 {
		t.Errorf("void not the centre cell: %+v", v)
	}
}

func TestInternalVoidsOpenGapIgnored(t *testing.T) {
	// A 1×1×3 column with a box only on the floor: the space above reaches the
	// top face, so it is open, not an internal void.
	voids := InternalVoids(1, 1, 3, []PlacedBox{{X: 0, Y: 0, Z: 0, W: 1, D: 1, H: 1}})
	if len(voids) != 0 {
		t.Errorf("open gap should not be reported as a void, got %+v", voids)
	}
}

func TestInternalVoidsEmpty(t *testing.T) {
	if v := InternalVoids(2, 2, 2, nil); v != nil {
		t.Errorf("empty bin has no voids, got %+v", v)
	}
}

func TestInternalVoidsMerge(t *testing.T) {
	// Leave a 1×1×2 enclosed channel (cells (1,1,1) and (1,1,2)) inside a
	// 3×3×4 bin; the two cells must merge into a single box.
	var boxes []PlacedBox
	for k := 0; k < 4; k++ {
		for j := 0; j < 3; j++ {
			for i := 0; i < 3; i++ {
				if i == 1 && j == 1 && (k == 1 || k == 2) {
					continue
				}
				boxes = append(boxes, PlacedBox{X: float64(i), Y: float64(j), Z: float64(k), W: 1, D: 1, H: 1})
			}
		}
	}
	voids := InternalVoids(3, 3, 4, boxes)
	if len(voids) != 1 {
		t.Fatalf("want 1 merged void, got %d: %+v", len(voids), voids)
	}
	if v := voids[0]; v.H != 2 || v.W != 1 || v.D != 1 {
		t.Errorf("channel should merge to 1×1×2, got %+v", v)
	}
}
