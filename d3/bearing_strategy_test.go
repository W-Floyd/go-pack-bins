package d3

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/pack"
)

const (
	wScalar = "weight"
	lScalar = "bearlimit"
)

func bearSpec() BearingSpec {
	return BearingSpec{WeightScalar: wScalar, LimitScalar: lScalar, DefaultLimit: NoLimit}
}

// bearItem is a unit-ish box with a weight and a bearing limit.
func bearItem(id string, w, d, h, weight, limit float64) *Item3D {
	return NewItem(id, w, d, h, false).
		WithScalar(wScalar, weight).
		WithScalar(lScalar, limit)
}

// strategyCtors are the strategies the constructive gate covers.
var strategyCtors = map[string]func(w, d, h float64) PlacementStrategy3D{
	"extremepoint": func(w, d, h float64) PlacementStrategy3D { return NewExtremePoint(w, d, h) },
	"ems":          func(w, d, h float64) PlacementStrategy3D { return NewEmptyMaximalSpace(w, d, h) },
	"blf":          func(w, d, h float64) PlacementStrategy3D { return NewBottomLeftFill(w, d, h) },
	"heightmap":    func(w, d, h float64) PlacementStrategy3D { return NewHeightmap(w, d, h) },
}

// canDivert records which strategies can route a placement around a box that
// cannot bear it. EMS cannot: it places only at the back-bottom-left corner of a
// free space, and once the floor is full the single remaining space spans the
// whole width with its corner over the fragile item — so no alternative position
// is in its candidate set at all. For EMS the gate degrades to refusal, which is
// safe but costs fill. See docs/plans/load-bearing-stacking.md §6.
var canDivert = map[string]bool{
	"extremepoint": true,
	"blf":          true,
	"heightmap":    true,
	"ems":          false,
}

// With the floor full, the only legal spot is atop the strong neighbour. The
// safety invariant — never land on something that cannot bear you — must hold
// for every strategy; diverting rather than refusing is the stronger property
// and only the corner-rich strategies manage it.
func TestBearingGateNeverLandsOnAFragileSupport(t *testing.T) {
	for name, ctor := range strategyCtors {
		t.Run(name, func(t *testing.T) {
			strat := WithBearing(ctor(2, 1, 2), bearSpec())
			if !Bearable(strat) {
				t.Fatalf("%s should be bearable", name)
			}
			bin := NewBin("b", 2, 1, 2, strat)

			// Fill the floor: fragile at x=0, strong at x=1.
			if _, err := bin.TryPlace(bearItem("fragile", 1, 1, 1, 1, 0)); err != nil {
				t.Fatalf("placing fragile: %v", err)
			}
			if _, err := bin.TryPlace(bearItem("strong", 1, 1, 1, 1, 100)); err != nil {
				t.Fatalf("placing strong: %v", err)
			}

			p, err := bin.TryPlace(bearItem("cargo", 1, 1, 1, 5, NoLimit))
			if err != nil {
				if canDivert[name] {
					t.Fatalf("%s should have diverted onto the strong item, but refused: %v", name, err)
				}
				return // refusal is the safe outcome for a corner-only strategy
			}
			if !canDivert[name] {
				t.Fatalf("%s placed cargo though it was expected to refuse; update canDivert", name)
			}
			cp := p.(*Placement3D)
			if cp.Z <= compactEps {
				t.Fatalf("cargo landed on the floor at z=%v; the floor was full", cp.Z)
			}
			if cp.X < 1-compactEps {
				t.Errorf("cargo stacked at x=%v — on the fragile item, which bears nothing", cp.X)
			}
		})
	}
}

// With no room beside it, the heavy item must be refused rather than crush.
func TestBearingGateRefusesWhenOnlyStackingIsPossible(t *testing.T) {
	for name, ctor := range strategyCtors {
		t.Run(name, func(t *testing.T) {
			strat := WithBearing(ctor(1, 1, 2), bearSpec())
			bin := NewBin("b", 1, 1, 2, strat)

			if _, err := bin.TryPlace(bearItem("weak", 1, 1, 1, 1, 2)); err != nil {
				t.Fatalf("placing weak: %v", err)
			}
			if _, err := bin.TryPlace(bearItem("heavy", 1, 1, 1, 10, NoLimit)); err == nil {
				t.Error("heavy item was placed on top of a weak one; expected refusal")
			}
		})
	}
}

// A light item may still stack on a limited one.
func TestBearingGateAllowsLightStack(t *testing.T) {
	for name, ctor := range strategyCtors {
		t.Run(name, func(t *testing.T) {
			strat := WithBearing(ctor(1, 1, 2), bearSpec())
			bin := NewBin("b", 1, 1, 2, strat)

			if _, err := bin.TryPlace(bearItem("base", 1, 1, 1, 1, 5)); err != nil {
				t.Fatalf("placing base: %v", err)
			}
			if _, err := bin.TryPlace(bearItem("light", 1, 1, 1, 3, NoLimit)); err != nil {
				t.Errorf("light item refused though within the base's limit: %v", err)
			}
		})
	}
}

// A zero limit means fragile: nothing may rest on it, however light.
func TestBearingFragileIsNeverBuried(t *testing.T) {
	for name, ctor := range strategyCtors {
		t.Run(name, func(t *testing.T) {
			strat := WithBearing(ctor(1, 1, 3), bearSpec())
			bin := NewBin("b", 1, 1, 3, strat)

			if _, err := bin.TryPlace(bearItem("fragile", 1, 1, 1, 1, 0)); err != nil {
				t.Fatalf("placing fragile: %v", err)
			}
			if _, err := bin.TryPlace(bearItem("feather", 1, 1, 1, 0.001, NoLimit)); err == nil {
				t.Error("something was stacked on a fragile item")
			}
		})
	}
}

// Crushing must be detected through a stack, not just directly above: a box two
// levels up still loads the base.
func TestBearingGateChecksThroughTheStack(t *testing.T) {
	for name, ctor := range strategyCtors {
		t.Run(name, func(t *testing.T) {
			strat := WithBearing(ctor(1, 1, 3), bearSpec())
			bin := NewBin("b", 1, 1, 3, strat)

			// base bears at most 5; middle weighs 2, so only 3 more may go on top.
			if _, err := bin.TryPlace(bearItem("base", 1, 1, 1, 1, 5)); err != nil {
				t.Fatalf("placing base: %v", err)
			}
			if _, err := bin.TryPlace(bearItem("middle", 1, 1, 1, 2, NoLimit)); err != nil {
				t.Fatalf("placing middle: %v", err)
			}
			if _, err := bin.TryPlace(bearItem("top", 1, 1, 1, 4, NoLimit)); err == nil {
				t.Error("top item placed; base would carry 6 against a limit of 5")
			}
		})
	}
}

// Whatever the constructive gate admits must satisfy the whole-configuration
// validator. This is the property that keeps the gate and the post-pass
// validator from disagreeing.
func TestBearingPackingValidates(t *testing.T) {
	items := []pack.Item{
		bearItem("a", 1, 1, 1, 5, 4),
		bearItem("b", 1, 1, 1, 3, 2),
		bearItem("c", 1, 1, 1, 2, 10),
		bearItem("d", 1, 1, 1, 6, 0),
		bearItem("e", 1, 1, 1, 1, NoLimit),
		bearItem("f", 2, 1, 1, 4, 3),
	}
	scalars := map[string]map[string]float64{}
	for _, it := range items {
		scalars[it.ID()] = pack.ScalarsOf(it)
	}

	for name, ctor := range strategyCtors {
		t.Run(name, func(t *testing.T) {
			bin := NewBin("b", 3, 1, 3, WithBearing(ctor(3, 1, 3), bearSpec()))
			var placed []*Placement3D
			for _, it := range items {
				if p, err := bin.TryPlace(it); err == nil {
					placed = append(placed, p.(*Placement3D))
				}
			}
			if len(placed) == 0 {
				t.Fatal("nothing was placed")
			}
			if !BearingOK(BearBoxesOf(placed, scalars, bearSpec())) {
				t.Errorf("gate admitted a configuration the validator rejects (%d placed)", len(placed))
			}
		})
	}
}

// Bearing off must leave placement byte-identical: the whole point of the
// nil-state design is that the default path is unchanged.
func TestBearingDisabledIsUnchanged(t *testing.T) {
	items := []pack.Item{
		bearItem("a", 2, 1, 1, 5, 1),
		bearItem("b", 1, 1, 2, 3, 1),
		bearItem("c", 1, 2, 1, 2, 1),
		bearItem("d", 2, 2, 1, 6, 1),
	}

	for name, ctor := range strategyCtors {
		t.Run(name, func(t *testing.T) {
			run := func(strat PlacementStrategy3D) []Placement3D {
				bin := NewBin("b", 4, 4, 4, strat)
				var out []Placement3D
				for _, it := range items {
					if p, err := bin.TryPlace(it); err == nil {
						out = append(out, *p.(*Placement3D))
					}
				}
				return out
			}

			plain := run(ctor(4, 4, 4))
			// A disabled spec must be a no-op even though it goes through WithBearing.
			off := run(WithBearing(ctor(4, 4, 4), BearingSpec{}))

			if len(plain) != len(off) {
				t.Fatalf("disabled bearing changed the placement count: %d vs %d", len(plain), len(off))
			}
			for i := range plain {
				if plain[i] != off[i] {
					t.Errorf("placement %d differs: %+v vs %+v", i, plain[i], off[i])
				}
			}
		})
	}
}

// Items carrying no limit scalar fall back to DefaultLimit, and that default is
// fragile unless the caller says otherwise — a mis-named scalar must fail closed.
func TestBearingDefaultLimitFailsClosed(t *testing.T) {
	spec := BearingSpec{WeightScalar: wScalar, LimitScalar: "typo"} // DefaultLimit 0
	bin := NewBin("b", 1, 1, 2, WithBearing(NewExtremePoint(1, 1, 2), spec))

	if _, err := bin.TryPlace(bearItem("base", 1, 1, 1, 1, 100)); err != nil {
		t.Fatalf("placing base: %v", err)
	}
	if _, err := bin.TryPlace(bearItem("top", 1, 1, 1, 1, 100)); err == nil {
		t.Error("a mis-named limit scalar allowed stacking; it must fail closed")
	}
}

// A strategy driven directly, bypassing Bin3D.TryPlace, never has a pending item
// set — it must refuse rather than treat items as weightless.
func TestBearingWithoutPendingItemRefuses(t *testing.T) {
	ep := NewExtremePoint(2, 2, 2)
	WithBearing(ep, bearSpec())
	if _, _, _, _, _, _, ok := ep.TryInsert([][3]float64{{1, 1, 1}}); ok {
		t.Error("placed an item with no pending weight set; must fail closed")
	}
}

// Occupy seeds an obstruction that bears anything and weighs nothing, so items
// may rest on it — the fixture case, not a re-seeded packing.
func TestBearingOccupyIsAnInfiniteBearer(t *testing.T) {
	ems := NewEmptyMaximalSpace(2, 1, 3)
	WithBearing(ems, bearSpec())
	ems.Occupy(0, 0, 0, 2, 1, 1)

	bin := NewBin("b", 2, 1, 3, ems)
	if _, err := bin.TryPlace(bearItem("heavy", 1, 1, 1, 1e6, NoLimit)); err != nil {
		t.Errorf("could not rest on an occupied fixture: %v", err)
	}
}

func TestBearableExcludesUncoveredStrategies(t *testing.T) {
	if Bearable(NewLayerStack(2, 2, 2)) {
		t.Error("LayerStack reported bearable; it has no bearing gate")
	}
	if !Bearable(NewExtremePoint(2, 2, 2)) {
		t.Error("ExtremePoint should be bearable")
	}
}
