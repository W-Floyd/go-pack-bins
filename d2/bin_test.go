package d2_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func TestBin2D_PlacesAndReportsTooLarge(t *testing.T) {
	bin := d2.NewBin("b", 100, 100, d2.NewMaxRectsDefault(100, 100))

	p, err := bin.TryPlace(d2.NewItem("a", 40, 30, false))
	if err != nil {
		t.Fatalf("place a: %v", err)
	}
	pl := p.(*d2.Placement2D)
	if pl.W != 40 || pl.H != 30 {
		t.Errorf("placed dims = %vx%v, want 40x30", pl.W, pl.H)
	}

	if _, err := bin.TryPlace(d2.NewItem("big", 200, 50, false)); err != pack.ErrItemTooLarge {
		t.Errorf("oversize err = %v, want ErrItemTooLarge", err)
	}
}

func TestBin2D_RotationEnablesFit(t *testing.T) {
	// A 90×20 item won't fit a 30-wide bin upright, but rotating to 20×90 does.
	bin := d2.NewBin("b", 30, 100, d2.NewMaxRectsDefault(30, 100))
	p, err := bin.TryPlace(d2.NewItem("r", 90, 20, true))
	if err != nil {
		t.Fatalf("place rotated: %v", err)
	}
	if pl := p.(*d2.Placement2D); !pl.Rotated {
		t.Errorf("expected item to be placed rotated")
	}
}
