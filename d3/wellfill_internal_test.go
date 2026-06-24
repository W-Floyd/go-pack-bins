package d3

import "testing"

// A 2×6 well (open to the top) is the only free floor; everything else is filled
// to z=4. A 6×2×4 item fits only rotated to 2×6×4. probeTop must drop it into the
// well at z=0 (top 4) rather than rest it on the surface (z=4, top 8) — proving
// the EMS well-probe + rotation + min-top/CG ranking work together.
func TestEMSProbeTop_FillsWellWithRotation(t *testing.T) {
	e := NewEmptyMaximalSpace(10, 10, 10)
	e.contact = ContactSpec{NoFloating: true}
	e.Occupy(2, 0, 0, 8, 10, 4) // right block
	e.Occupy(0, 6, 0, 2, 4, 4)  // left strip above y=6 → leaves a 2×6 well at (0,0)

	item := NewItem("x", 6, 2, 4, true) // rotatable; (2,6,4) fits the well
	c, ok := e.probeTop(item.Orientations())
	if !ok {
		t.Fatal("no placement found")
	}
	if c.z > 1e-9 || c.z+c.h > 4+1e-9 {
		t.Fatalf("placed at z=%.1f top=%.1f, want z=0 top=4 (in the well, not on the surface)", c.z, c.z+c.h)
	}
	if c.w != 2 || c.d != 6 {
		t.Fatalf("placed footprint %gx%g, want 2x6 (rotated into the well)", c.w, c.d)
	}
}
