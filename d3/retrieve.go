package d3

// Retrievability: every box must have a face you can actually get it out by.
//
// A packing can be perfectly legal and still useless. Bury a box in the middle
// of a solid block and it is unreachable without unpacking everything around
// it — the bin is a puzzle, not a store. This constraint requires each item to
// keep at least one face with enough free space in front of it to slide the item
// clear of its slot.
//
// It is a property of the *whole* configuration, not of a placement: putting a
// box down can take away the last exit of a box that was fine a moment ago. So
// like the load-bearing rule it is checked over the configuration rather than
// once at placement, and every relocation has to re-check it.

// Face is a direction an item can be withdrawn in.
type Face int

const (
	FaceXNeg Face = iota // toward x = 0
	FaceXPos             // toward x = binW
	FaceYNeg
	FaceYPos
	FaceZPos // straight up
)

// RetrievalSpec says what counts as being able to get an item out.
type RetrievalSpec struct {
	// Clearance is the free depth required in front of a face. Zero means the
	// item's own extent along that axis — room to pull it fully clear of its
	// slot, which is the least that can count as retrieving it.
	Clearance float64
	// OpenFaces names the bin's own faces that are openings — the end of a
	// trailer, the front of a shelving unit. When set, an item is retrievable
	// only by a clear run from one of its faces all the way out through one of
	// these, and Clearance is ignored: the run is however far the wall is.
	//
	// When empty the bin is treated as a room: it is enough to pull a box into
	// the space beside it, wherever that space is.
	//
	// Naming the openings is what makes this meaningful. An earlier version had
	// a plain "reach the boundary" flag, under which any box touching any wall
	// counted as being at an exit — so almost every packing passed.
	OpenFaces []Face
	// SidesOnly drops the top face. Lifting a box straight up is usually the
	// easiest way out, but not under a low ceiling or a fixed shelf.
	SidesOnly bool
}

// A zero RetrievalSpec is meaningful — own-extent clearance through any of the
// five faces — so whether the constraint applies at all is the caller's to say,
// not something derivable from the spec.

// faces returns the withdrawal directions this spec allows.
func (s RetrievalSpec) faces() []Face {
	if len(s.OpenFaces) > 0 {
		// Only the directions that lead out of the bin are worth trying.
		out := make([]Face, 0, len(s.OpenFaces))
		for _, f := range s.OpenFaces {
			if f == FaceZPos && s.SidesOnly {
				continue
			}
			out = append(out, f)
		}
		return out
	}
	f := []Face{FaceXNeg, FaceXPos, FaceYNeg, FaceYPos}
	if !s.SidesOnly {
		f = append(f, FaceZPos)
	}
	return f
}

// channel is the volume an item sweeps through when withdrawn via one face: its
// own cross-section, extruded outward. ok is false when the face gives no route
// at all — no room inside the bin, and not at an opening.
func channel(p *Placement3D, f Face, binW, binD, binH float64, spec RetrievalSpec) (x0, y0, z0, x1, y1, z1 float64, ok bool) {
	// depth returns how far the item must travel, and how far it may.
	depth := func(own, avail float64) (float64, bool) {
		if len(spec.OpenFaces) > 0 {
			// The caller named this direction as an opening, so the run is
			// however far the wall is — zero when the item is already at it.
			return avail, true
		}
		need := spec.Clearance
		if need <= 0 {
			need = own
		}
		if avail < need {
			return 0, false
		}
		return need, true
	}

	switch f {
	case FaceXNeg:
		d, good := depth(p.W, p.X)
		return p.X - d, p.Y, p.Z, p.X, p.Y + p.D, p.Z + p.H, good
	case FaceXPos:
		d, good := depth(p.W, binW-(p.X+p.W))
		return p.X + p.W, p.Y, p.Z, p.X + p.W + d, p.Y + p.D, p.Z + p.H, good
	case FaceYNeg:
		d, good := depth(p.D, p.Y)
		return p.X, p.Y - d, p.Z, p.X + p.W, p.Y, p.Z + p.H, good
	case FaceYPos:
		d, good := depth(p.D, binD-(p.Y+p.D))
		return p.X, p.Y + p.D, p.Z, p.X + p.W, p.Y + p.D + d, p.Z + p.H, good
	default: // FaceZPos
		d, good := depth(p.H, binH-(p.Z+p.H))
		return p.X, p.Y, p.Z + p.H, p.X + p.W, p.Y + p.D, p.Z + p.H + d, good
	}
}

// regionClear reports whether a box-shaped region holds no other item and no
// obstructing zone. The item being withdrawn is skipped by identity, so its own
// body never blocks its own exit. Named to stay clear of the clear() builtin,
// which a package-level func of that name would shadow for the whole package.
func regionClear(x0, y0, z0, x1, y1, z1 float64, self *Placement3D, others []*Placement3D, zones []Zone) bool {
	if x1-x0 <= compactEps || y1-y0 <= compactEps || z1-z0 <= compactEps {
		return true // a zero-depth channel is trivially clear: already at the exit
	}
	for _, o := range others {
		if o == self {
			continue
		}
		if overlap1D(x0, x1, o.X, o.X+o.W) > compactEps &&
			overlap1D(y0, y1, o.Y, o.Y+o.D) > compactEps &&
			overlap1D(z0, z1, o.Z, o.Z+o.H) > compactEps {
			return false
		}
	}
	for i := range zones {
		z := &zones[i]
		if z.Empty() || !z.blocksPath() {
			continue
		}
		if overlap1D(x0, x1, z.X, z.X+z.W) > compactEps &&
			overlap1D(y0, y1, z.Y, z.Y+z.D) > compactEps &&
			overlap1D(z0, z1, z.Z, z.Z+z.H) > compactEps {
			return false
		}
	}
	return true
}

// Retrievable reports whether an item has at least one face it can be withdrawn
// through.
func Retrievable(p *Placement3D, others []*Placement3D, zones []Zone,
	binW, binD, binH float64, spec RetrievalSpec) bool {

	for _, f := range spec.faces() {
		x0, y0, z0, x1, y1, z1, ok := channel(p, f, binW, binD, binH, spec)
		if !ok {
			continue
		}
		if regionClear(x0, y0, z0, x1, y1, z1, p, others, zones) {
			return true
		}
	}
	return false
}

// RetrievableAll reports whether every item in a bin can be withdrawn. This is
// the whole-configuration check: a placement that is fine on its own can seal
// another item in.
func RetrievableAll(ps []*Placement3D, zones []Zone, binW, binD, binH float64, spec RetrievalSpec) bool {
	for _, p := range ps {
		if !Retrievable(p, ps, zones, binW, binD, binH, spec) {
			return false
		}
	}
	return true
}

// Buried returns the items that cannot be withdrawn, for reporting which boxes a
// packing has sealed in.
func Buried(ps []*Placement3D, zones []Zone, binW, binD, binH float64, spec RetrievalSpec) []string {
	var out []string
	for _, p := range ps {
		if !Retrievable(p, ps, zones, binW, binD, binH, spec) {
			out = append(out, p.itemID)
		}
	}
	return out
}

// ── constructive gate ────────────────────────────────────────────────────────

// retrieveState is a strategy's retrievability bookkeeping. Unlike the bearing
// gate it needs no item identity — retrievability is pure geometry — so it works
// off the boxes the strategy already keeps. A nil state permits everything.
type retrieveState struct {
	spec             RetrievalSpec
	binW, binD, binH float64
	zones            []Zone
	boxes            []*Placement3D // geometry only; ids are unused here
}

func newRetrieveState(spec RetrievalSpec, w, d, h float64) *retrieveState {
	return &retrieveState{spec: spec, binW: w, binD: d, binH: h}
}

// allows reports whether placing a box here leaves every item retrievable,
// itself included.
//
// It re-checks only the candidate and the boxes the candidate could have sealed
// in — those it touches or overhangs. A box far away cannot have lost an exit to
// this placement, and checking every box on every candidate would make the gate
// quadratic in the bin's contents for each of the many candidates an item tries.
func (r *retrieveState) allows(x, y, z, w, d, h float64) bool {
	if r == nil {
		return true
	}
	cand := NewPlacement3D("", "", x, y, z, w, d, h)
	all := append(append(make([]*Placement3D, 0, len(r.boxes)+1), r.boxes...), cand)

	if !Retrievable(cand, all, r.zones, r.binW, r.binD, r.binH, r.spec) {
		return false
	}
	reach := r.maxReach()
	for _, o := range r.boxes {
		if !nearEnough(o, cand, reach) {
			continue
		}
		if !Retrievable(o, all, r.zones, r.binW, r.binD, r.binH, r.spec) {
			return false
		}
	}
	return true
}

func (r *retrieveState) commit(x, y, z, w, d, h float64) {
	if r == nil {
		return
	}
	r.boxes = append(r.boxes, NewPlacement3D("", "", x, y, z, w, d, h))
}

// maxReach is how far from a placement another box's exit could be affected: the
// clearance it would need, or the largest box dimension when that is its own
// extent.
func (r *retrieveState) maxReach() float64 {
	if r.spec.Clearance > 0 {
		return r.spec.Clearance
	}
	reach := 0.0
	for _, b := range r.boxes {
		for _, v := range [3]float64{b.W, b.D, b.H} {
			if v > reach {
				reach = v
			}
		}
	}
	return reach
}

// nearEnough reports whether b is close enough to c that c could have taken away
// one of its exits.
func nearEnough(b, c *Placement3D, reach float64) bool {
	return overlap1D(b.X-reach, b.X+b.W+reach, c.X, c.X+c.W) > 0 &&
		overlap1D(b.Y-reach, b.Y+b.D+reach, c.Y, c.Y+c.D) > 0 &&
		overlap1D(b.Z-reach, b.Z+b.H+reach, c.Z, c.Z+c.H) > 0
}

// WithRetrieval makes a strategy refuse any placement that would leave an item
// — the new one or an existing one — with no way out. Strategies that cannot
// enforce it are returned unchanged; check RetrievalAware first where a silent
// no-op would matter.
//
// The gate is a *local* check: it re-examines the candidate and its neighbours,
// not the whole bin, so a chain of placements could in principle seal something
// in at a distance. RetrievableAll over the finished packing is the authority,
// and packapi runs it.
func WithRetrieval(s PlacementStrategy3D, spec RetrievalSpec, zones []Zone, w, d, h float64) PlacementStrategy3D {
	st := newRetrieveState(spec, w, d, h)
	st.zones = zones
	switch t := s.(type) {
	case *ExtremePoint:
		t.retrieve = st
	case *BottomLeftFill:
		t.retrieve = st
	case *Heightmap:
		t.retrieve = st
	case *EmptyMaximalSpace:
		t.retrieve = st
	case *FitPacker:
		t.EmptyMaximalSpace.retrieve = st
	}
	return s
}

// RetrievalAware reports whether a strategy enforces the retrievability gate.
func RetrievalAware(s PlacementStrategy3D) bool {
	switch s.(type) {
	case *ExtremePoint, *EmptyMaximalSpace, *BottomLeftFill, *Heightmap, *FitPacker:
		return true
	}
	return false
}

// RetrievalStrategy wraps a Factory3D-compatible constructor so every bin it
// opens enforces the gate.
func RetrievalStrategy(ctor func(w, d, h float64) PlacementStrategy3D, spec RetrievalSpec,
	zones []Zone, enabled bool) func(w, d, h float64) PlacementStrategy3D {

	if !enabled {
		return ctor
	}
	return func(w, d, h float64) PlacementStrategy3D {
		return WithRetrieval(ctor(w, d, h), spec, zones, w, d, h)
	}
}
