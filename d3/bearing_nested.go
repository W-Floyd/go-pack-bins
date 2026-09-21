package d3

import "math"

// Load paths through containers.
//
// The flat rule in bearing.go asks "how much rests on this box". That is the
// wrong question for a box with things inside it. A carton whose contents reach
// its top face does not carry load on its own lid: the load passes through the
// contents. Four strong posts, one per corner, can carry a heavy box above while
// the carton itself only ever rates for what actually sits on its structure.
//
// So a load arriving on a container's top face is decomposed *per contact patch*
// by physical layout, not by a whole-container "is it full" flag:
//
//   - the part of the patch over contents that reach the top passes into those
//     contents, and is checked against their limits, recursively;
//   - the remainder rests on the container's own structure, and is checked
//     against the container's limits.
//
// Downward propagation is unaffected: whatever enters a container leaves its
// underside, however it split on the way through. The decomposition changes
// *which limits are checked*, not how much load reaches the floor.
//
// Source: the layer-from-floor load-bearing schemes of Ratcliff & Bischoff
// (1998) and Bischoff (2006) applied recursively through nested containers.
// This remains a static crush model — see docs/plans/load-bearing-stacking.md.

// patch is a load spread over a rectangle of some box's top face.
type patch struct {
	x0, y0, x1, y1 float64
	load           float64
}

func (p patch) area() float64 {
	w, d := p.x1-p.x0, p.y1-p.y0
	if w <= 0 || d <= 0 {
		return 0
	}
	return w * d
}

// intersect returns the overlap of a patch with a box's footprint, and whether
// it is non-empty. The load is apportioned by area, which is the same
// contact-area apportionment the flat rule uses between multiple supporters.
func (p patch) intersect(b *BearBox) (patch, bool) {
	out := patch{
		x0: math.Max(p.x0, b.X), y0: math.Max(p.y0, b.Y),
		x1: math.Min(p.x1, b.X+b.W), y1: math.Min(p.y1, b.Y+b.D),
	}
	pa := p.area()
	if pa <= 0 {
		return out, false
	}
	a := out.area()
	if a <= compactEps {
		return out, false
	}
	out.load = p.load * a / pa
	return out, true
}

// levelState accumulates, for one frame of boxes, the load that rests on each
// box's own structure and the load that passes through it downward.
type levelState struct {
	boxes []BearBox
	// structural is what each box's own limits must carry: the part of every
	// arriving patch that did not pass into contents reaching its top face.
	structural []float64
	// peak is the highest pressure any single patch applied to a box's own
	// structure. Peak rather than sum: two patches do not merge into one more
	// concentrated patch.
	peak []float64
	// through is everything that arrived on a box's top face, structural and
	// transmitted alike. It is what the box sheds downward along with its own
	// weight.
	through []float64
	// pending collects, per container, the patches routed into its contents.
	// They are settled once the whole frame has been walked: a container may
	// receive several patches, and its contents' limits are cumulative, so
	// settling per arriving patch would check each in isolation.
	pending map[int][]targeted
	// failed is set once any limit is exceeded, so the walk can stop early.
	failed bool
}

// LoadPathOK reports whether a configuration satisfies every box's weight and
// pressure limits, resolving load paths through nested contents.
//
// With no box carrying Contents this is exactly the flat rule, which is why
// BearingOK delegates to it: one rule, not two that can drift.
func LoadPathOK(bs []BearBox) bool {
	st := newLevelState(bs)
	st.settle(nil)
	return !st.failed
}

// targeted is a patch already routed to a specific box in a frame. The parent
// resolves which contents a patch lands on, so the child frame must not re-match
// it against every box — that would apply the same load several times over.
type targeted struct {
	idx int
	p   patch
}

func newLevelState(bs []BearBox) *levelState {
	return &levelState{
		boxes:      bs,
		structural: make([]float64, len(bs)),
		peak:       make([]float64, len(bs)),
		through:    make([]float64, len(bs)),
		pending:    map[int][]targeted{},
	}
}

// settle applies any load entering this frame from above, then walks the boxes
// top-down so each sheds its accumulated load onto whatever supports it.
//
// External patches land on boxes whose top face is at this frame's ceiling, and
// the top-down order visits those first, so every box has received everything
// destined for it before it sheds.
func (st *levelState) settle(external []targeted) {
	for _, t := range external {
		if st.failed {
			return
		}
		st.apply(t.idx, t.p)
	}

	for _, i := range topDown(st.boxes) {
		if st.failed {
			return
		}
		b := &st.boxes[i]
		shed := laden(b) + st.through[i]
		if shed <= 0 {
			continue
		}
		sup := supportersOf(st.boxes, b.X, b.Y, b.Z, b.W, b.D)
		total := 0.0
		for _, s := range sup {
			total += s.area
		}
		if total <= 0 {
			continue // floor, or floating: the load leaves this frame
		}
		for _, s := range sup {
			o := &st.boxes[s.idx]
			st.apply(s.idx, patch{
				x0: math.Max(b.X, o.X), y0: math.Max(b.Y, o.Y),
				x1: math.Min(b.X+b.W, o.X+o.W), y1: math.Min(b.Y+b.D, o.Y+o.D),
				load: shed * s.area / total,
			})
		}
	}

	st.settleContents()
}

// settleContents resolves every container's interior, once the whole frame has
// been walked and each container has received all the load destined for it.
//
// Containers with no load arriving are settled too: their contents still bear
// each other's weight, and a fragile item can be crushed from inside.
func (st *levelState) settleContents() {
	for i := range st.boxes {
		if st.failed {
			return
		}
		b := &st.boxes[i]
		if len(b.Contents) == 0 {
			continue
		}
		sub := newLevelState(b.Contents)
		sub.settle(st.pending[i])
		if sub.failed {
			st.failed = true
			return
		}
	}
}

// apply routes one patch onto box i, splitting it between the box's contents and
// its own structure, and checks the limits that bind on each.
func (st *levelState) apply(i int, p patch) {
	if st.failed || p.load <= 0 {
		return
	}
	b := &st.boxes[i]
	st.through[i] += p.load

	own := p
	if !b.Rigid && len(b.Contents) > 0 {
		// Contents that reach this box's top face carry load through; the
		// remainder of the patch rests on the box's own structure.
		inner, throughArea := routeIntoContents(b, p)
		st.pending[i] = append(st.pending[i], inner...)
		pa := p.area()
		remaining := pa - throughArea
		if remaining <= compactEps {
			return // the whole patch transferred into the contents
		}
		own.load = p.load * remaining / pa
		// The remainder is spread over the uncovered part of the patch, so its
		// pressure is computed against that smaller area.
		st.charge(i, own.load, remaining)
		return
	}
	st.charge(i, own.load, p.area())
}

// charge books load onto a box's own structure and checks both limits.
func (st *levelState) charge(i int, load, area float64) {
	b := &st.boxes[i]
	st.structural[i] += load
	if st.structural[i] > b.Limit+bearEps {
		st.failed = true
		return
	}
	if area > compactEps {
		if p := load / area; p > st.peak[i] {
			st.peak[i] = p
			if p > b.PressureLimit+bearEps {
				st.failed = true
			}
		}
	}
}

// routeIntoContents splits a patch across the contents that reach the box's top
// face, returning the per-content patches and the total area they cover. A
// content that does not reach the top carries nothing: the lid spans the gap,
// so that load rests on the container instead.
func routeIntoContents(b *BearBox, p patch) ([]targeted, float64) {
	top := topOf(b)
	var out []targeted
	covered := 0.0
	for i := range b.Contents {
		c := &b.Contents[i]
		if math.Abs(topOf(c)-top) > compactEps {
			continue // not flush with the lid
		}
		q, ok := p.intersect(c)
		if !ok {
			continue
		}
		out = append(out, targeted{idx: i, p: q})
		covered += q.area()
	}
	return out, covered
}

func topOf(b *BearBox) float64 { return b.Z + b.H }

// laden is a box's weight including everything inside it, recursively. Weight is
// the box's own tare, so a carton must shed its contents' weight too — deriving
// it here rather than asking callers to pre-add it, since a caller that forgets
// produces a packing that silently under-loads everything beneath.
func laden(b *BearBox) float64 {
	w := b.Weight
	for i := range b.Contents {
		w += laden(&b.Contents[i])
	}
	return w
}
