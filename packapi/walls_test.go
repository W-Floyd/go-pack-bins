package packapi

import (
	"context"
	"testing"
)

// A crate with a thick base and thin sides: the interior is neither the declared
// size nor centred within it.
func TestWallsCrateThickBaseThinSides(t *testing.T) {
	crate := BinSpec{Width: 20, Depth: 20, Height: 20,
		Walls: &WallSpec{Left: 1, Right: 1, Front: 1, Back: 1, Floor: 4, Lid: 0}}

	in := crate.Interior()
	if in.Width != 18 || in.Depth != 18 {
		t.Errorf("interior %v x %v, want 18 x 18 (1 of wall each side)", in.Width, in.Depth)
	}
	if in.Height != 16 {
		t.Errorf("interior height %v, want 16 (4 of base, open top)", in.Height)
	}
	x, y, z := crate.InteriorOffset()
	if x != 1 || y != 1 || z != 4 {
		t.Errorf("interior starts at (%v,%v,%v), want (1,1,4) — it is not centred", x, y, z)
	}
}

// A uniform wall is still expressible, and is the common case.
func TestWallsUniform(t *testing.T) {
	b := BinSpec{Width: 40, Depth: 30, Height: 30, Walls: UniformWalls(2)}
	in := b.Interior()
	if in.Width != 36 || in.Depth != 26 || in.Height != 26 {
		t.Errorf("interior = %v x %v x %v, want 36 x 26 x 26", in.Width, in.Depth, in.Height)
	}
	if x, y, z := b.InteriorOffset(); x != 2 || y != 2 || z != 2 {
		t.Errorf("offset = (%v,%v,%v), want (2,2,2)", x, y, z)
	}
}

// No walls declared means the whole box is usable — the existing behaviour, so
// every packing without walls is unchanged.
func TestWallsAbsentIsWholeBox(t *testing.T) {
	b := BinSpec{Width: 5, Depth: 6, Height: 7}
	if in := b.Interior(); in != (BinSpec{Width: 5, Depth: 6, Height: 7}) {
		t.Errorf("interior = %+v, want the declared size", in)
	}
	if x, y, z := b.InteriorOffset(); x != 0 || y != 0 || z != 0 {
		t.Errorf("offset = (%v,%v,%v), want the origin", x, y, z)
	}
	if !b.Hollow() {
		t.Error("a box with no walls should have a usable interior")
	}
}

// Walls thicker than the box leave nothing usable, reported as empty rather than
// negative.
func TestWallsThickerThanTheBox(t *testing.T) {
	b := BinSpec{Width: 4, Depth: 4, Height: 4, Walls: UniformWalls(3)}
	in := b.Interior()
	if in.Width != 0 || in.Depth != 0 || in.Height != 0 {
		t.Errorf("interior = %+v, want zero rather than negative", in)
	}
	if b.Hollow() {
		t.Error("a box whose walls meet in the middle is not hollow")
	}
}

// End to end: the goods go in the interior, but the carton still takes its
// declared size on the pallet. Quoting one number for both is what this exists
// to prevent.
func TestWallsNestedPacksInteriorOccupiesExterior(t *testing.T) {
	// Carton 10 across with 1 of wall: 8 usable inside, 10 used on the pallet.
	carton := BinSpec{Width: 10, Depth: 10, Height: 10, Walls: UniformWalls(1)}
	items := []ItemSpec{
		{ID: "a", Width: 4, Depth: 4, Height: 4},
		{ID: "b", Width: 4, Depth: 4, Height: 4},
	}
	req := NestedPackRequest{Mode: "3d", Items: items,
		Levels: []NestedLevelSpec{
			{Bin: carton, Algorithm: "ffd"},
			{Bin: BinSpec{Width: 20, Depth: 20, Height: 20}, Algorithm: "ffd"},
		}}
	resp, err := PackNestedCtx(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" {
		t.Fatalf("solve: %s", resp.Error)
	}

	// Nothing inside may exceed the interior.
	for _, p := range resp.Levels[0].Placements {
		if p.X+p.W > 8+1e-9 || p.Y+p.D > 8+1e-9 || p.Z+p.H > 8+1e-9 {
			t.Errorf("%s reaches %v,%v,%v — past the carton's 8-unit interior",
				p.ItemID, p.X+p.W, p.Y+p.D, p.Z+p.H)
		}
	}
	// The carton itself still occupies its declared size on the pallet.
	for _, p := range resp.Levels[1].Placements {
		if p.W != 10 || p.D != 10 || p.H != 10 {
			t.Errorf("carton is %vx%vx%v on the pallet, want its declared 10x10x10",
				p.W, p.D, p.H)
		}
	}
}
