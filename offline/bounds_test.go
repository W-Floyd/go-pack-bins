package offline

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func TestLowerBoundVolume_Basic(t *testing.T) {
	// Total length 25 into capacity-10 bins: ceil(25/10) = 3.
	items := []pack.Item{
		d1.NewItem("a", 6), d1.NewItem("b", 6), d1.NewItem("c", 6),
		d1.NewItem("d", 4), d1.NewItem("e", 3),
	}
	if got := LowerBoundVolume(items, 10); got != 3 {
		t.Fatalf("LowerBoundVolume = %d, want 3", got)
	}
}

func TestLowerBoundVolume_ExactMultipleNoOverCount(t *testing.T) {
	// Total exactly 20 into capacity 10 ⇒ exactly 2 (float noise must not push to 3).
	items := []pack.Item{d1.NewItem("a", 10), d1.NewItem("b", 10)}
	if got := LowerBoundVolume(items, 10); got != 2 {
		t.Fatalf("LowerBoundVolume = %d, want 2", got)
	}
}

func TestLowerBoundVolume_LooseVsActual(t *testing.T) {
	// Three items of length 6 into capacity 10: volume bound = ceil(18/10) = 2,
	// but no two 6s share a bin (12 > 10) so the optimum is 3. Confirms the volume
	// bound is a *valid* (never-exceeding) lower bound even when loose.
	items := []pack.Item{d1.NewItem("a", 6), d1.NewItem("b", 6), d1.NewItem("c", 6)}
	lb := LowerBoundVolume(items, 10)
	if lb != 2 {
		t.Fatalf("LowerBoundVolume = %d, want 2 (loose)", lb)
	}
	if lb > 3 {
		t.Fatalf("lower bound %d exceeds optimum 3", lb)
	}
}

func TestLowerBoundVolume_NonPositiveCapacity(t *testing.T) {
	items := []pack.Item{d1.NewItem("a", 5)}
	if got := LowerBoundVolume(items, 0); got != 0 {
		t.Fatalf("LowerBoundVolume(cap=0) = %d, want 0", got)
	}
}
