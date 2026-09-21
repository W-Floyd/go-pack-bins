package d3

// Exclusion zones: regions of a bin that items must avoid.
//
// Real containers are not empty boxes. A basement has a pipe crossing it
// mid-air, a door that must still open, a meter to leave clear; a trailer has a
// wheel arch. None of these are items to be packed — nothing rests on them and
// they occupy no usable volume — so they cannot be modelled by pre-placing a
// dummy item, which would both offer support and count toward utilisation.
//
// A zone is a pure geometric veto: no placement may overlap one. It grants no
// support, so an item cannot sit on a pipe, and it is not occupied volume, so
// bin utilisation and the Best/Worst-Fit selectors still measure real packing.

// Zone is an axis-aligned region of a bin that no item may occupy, in the bin's
// own coordinates.
type Zone struct {
	X, Y, Z, W, D, H float64
}

// Empty reports whether the zone has no volume, in which case it blocks nothing.
func (z Zone) Empty() bool { return z.W <= 0 || z.D <= 0 || z.H <= 0 }

// blocks reports whether a box placed at (x,y,z) with size (w,d,h) intersects
// the zone. Touching faces are allowed: a box may sit flush against a zone, the
// same rule placements already use against each other.
func (z Zone) blocks(x, y, zz, w, d, h float64) bool {
	if z.Empty() {
		return false
	}
	return overlap1D(x, x+w, z.X, z.X+z.W) > compactEps &&
		overlap1D(y, y+d, z.Y, z.Y+z.D) > compactEps &&
		overlap1D(zz, zz+h, z.Z, z.Z+z.H) > compactEps
}

// zoneSet is a strategy's exclusion zones. A nil set blocks nothing, so the
// gate costs one length check when the feature is unused.
type zoneSet []Zone

func (zs zoneSet) blocks(x, y, z, w, d, h float64) bool {
	for i := range zs {
		if zs[i].blocks(x, y, z, w, d, h) {
			return true
		}
	}
	return false
}

// WithZones makes a strategy refuse any placement overlapping one of the zones,
// returning it for chaining. Strategies that cannot enforce zones are returned
// unchanged — check ZoneAware first where a silent no-op would matter.
func WithZones(s PlacementStrategy3D, zones []Zone) PlacementStrategy3D {
	if len(zones) == 0 {
		return s
	}
	switch t := s.(type) {
	case *ExtremePoint:
		t.zones = zones
	case *BottomLeftFill:
		t.zones = zones
	case *Heightmap:
		t.zones = zones
	case *EmptyMaximalSpace:
		t.zones = zones
		carveZones(t, zones)
	case *FitPacker:
		t.zones = zones
		carveZones(t.EmptyMaximalSpace, zones)
	}
	return s
}

// carveZones removes the zones from a maximal-space strategy's free set. The
// space-based strategies only ever place at a space's corner, so a zone that is
// merely vetoed would still shadow every corner behind it — carving makes the
// reachable corners the ones that actually exist.
func carveZones(e *EmptyMaximalSpace, zones []Zone) {
	for _, z := range zones {
		if !z.Empty() {
			e.carve(box{z.X, z.Y, z.Z, z.W, z.D, z.H})
		}
	}
}

// ZoneAware reports whether a strategy enforces exclusion zones. LayerStack does
// not: it delegates placement to a 2-D bin per layer, which would need the zones
// projected onto each layer rather than tested directly.
func ZoneAware(s PlacementStrategy3D) bool {
	switch s.(type) {
	case *ExtremePoint, *EmptyMaximalSpace, *BottomLeftFill, *Heightmap, *FitPacker:
		return true
	}
	return false
}

// ZoneStrategy wraps a Factory3D-compatible constructor so every bin it opens
// enforces the zones.
func ZoneStrategy(ctor func(w, d, h float64) PlacementStrategy3D, zones []Zone) func(w, d, h float64) PlacementStrategy3D {
	if len(zones) == 0 {
		return ctor
	}
	return func(w, d, h float64) PlacementStrategy3D {
		return WithZones(ctor(w, d, h), zones)
	}
}

// ZonesOK reports whether no placement intrudes on a zone. Relocation post-passes
// move committed items, so a packing that was legal when built can stop being
// legal; this is the check they have to pass.
func ZonesOK(ps []*Placement3D, zones []Zone) bool {
	if len(zones) == 0 {
		return true
	}
	zs := zoneSet(zones)
	for _, p := range ps {
		if zs.blocks(p.X, p.Y, p.Z, p.W, p.D, p.H) {
			return false
		}
	}
	return true
}

// ZoneFreeVolume is the volume of a bin left usable once the zones are removed,
// for callers reporting capacity. Overlapping zones are counted once via a
// sampling-free inclusion of pairwise overlaps only, so it is exact for
// non-overlapping zones and an underestimate of free space when three or more
// zones share a region — the conservative direction for a capacity figure.
func ZoneFreeVolume(binW, binD, binH float64, zones []Zone) float64 {
	free := binW * binD * binH
	for i := range zones {
		free -= clampedZoneVolume(zones[i], binW, binD, binH)
	}
	for i := range zones {
		for j := i + 1; j < len(zones); j++ {
			free += pairOverlapVolume(zones[i], zones[j], binW, binD, binH)
		}
	}
	if free < 0 {
		return 0
	}
	return free
}

func clampedZoneVolume(z Zone, binW, binD, binH float64) float64 {
	w := overlap1D(z.X, z.X+z.W, 0, binW)
	d := overlap1D(z.Y, z.Y+z.D, 0, binD)
	h := overlap1D(z.Z, z.Z+z.H, 0, binH)
	if w <= 0 || d <= 0 || h <= 0 {
		return 0
	}
	return w * d * h
}

func pairOverlapVolume(a, b Zone, binW, binD, binH float64) float64 {
	lo := func(p, q float64) float64 {
		if p > q {
			return p
		}
		return q
	}
	hi := func(p, q float64) float64 {
		if p < q {
			return p
		}
		return q
	}
	return clampedZoneVolume(Zone{
		X: lo(a.X, b.X), Y: lo(a.Y, b.Y), Z: lo(a.Z, b.Z),
		W: hi(a.X+a.W, b.X+b.W) - lo(a.X, b.X),
		D: hi(a.Y+a.D, b.Y+b.D) - lo(a.Y, b.Y),
		H: hi(a.Z+a.H, b.Z+b.H) - lo(a.Z, b.Z),
	}, binW, binD, binH)
}

// Guard bundles the constraints a relocation post-pass must preserve. A nil
// Guard permits everything.
//
// Compaction, settling and the void refiner all move committed items, so each
// must re-check whatever the constructive gate enforced — a slide toward a wall
// can push a box into a pipe just as easily as onto something that cannot bear
// it.
type Guard struct {
	Bearing *BearingGuard
	Zones   []Zone
}

// OK reports whether a bin satisfies every guarded constraint.
func (g *Guard) OK(ps []*Placement3D) bool {
	if g == nil {
		return true
	}
	return g.Bearing.OK(ps) && ZonesOK(ps, g.Zones)
}

// Empty reports whether the guard constrains nothing, so callers can skip work.
func (g *Guard) Empty() bool { return g == nil || (g.Bearing == nil && len(g.Zones) == 0) }

// NewGuard builds a Guard, returning nil when there is nothing to guard so the
// unconstrained path stays a single nil check.
func NewGuard(bearing *BearingGuard, zones []Zone) *Guard {
	if bearing == nil && len(zones) == 0 {
		return nil
	}
	return &Guard{Bearing: bearing, Zones: zones}
}
