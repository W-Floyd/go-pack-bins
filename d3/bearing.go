package d3

// Load-bearing (static crush) model: every box may carry a bounded weight on its
// top face. Attribution: Bischoff (2006), EJOR 168(3); Junqueira, Morabito &
// Yamashita (2012), C&OR 39(1). This is a static crush check, not a dynamic
// stability model — see docs/plans/load-bearing-stacking.md §3.
//
// The rule: for every box i, the weight of everything resting on i transitively
// must not exceed its Limit. A box resting on several supporters splits its
// borne load across them in proportion to contact area — statically
// indeterminate in reality, so contact-area apportionment is the chosen model.
// The floor bears infinitely.

import (
	"math"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// BearBox is a placed box plus the two scalars the bearing rule needs. It is the
// common currency between the strategies (which work over the internal box type)
// and the relocation post-passes (which work over []*Placement3D), so the rule
// lives in exactly one place.
type BearBox struct {
	X, Y, Z, W, D, H float64
	Weight           float64 // this box's own weight
	Limit            float64 // max weight its top face may carry; see NoLimit
	// PressureLimit caps the weight per unit of contact area, independently of
	// Limit. A box may take 10kg spread over its whole top and still be crushed
	// by 3kg on a narrow foot, so the two checks are separate and both apply.
	// NoLimit means unrestricted.
	PressureLimit float64

	// Contents is the packing inside this box, expressed in the *same* frame as
	// the box itself rather than a local one, so overlap tests need no
	// conversion. Empty for an ordinary item.
	//
	// A box with contents transmits load through them where they reach its top
	// face: see LoadPathOK. The nesting is arbitrarily deep.
	Contents []BearBox
	// Rigid makes the box carry load on its own structure regardless of what is
	// inside it. Without it, load over contents that reach the top face passes
	// into those contents and is checked against *their* limits, not this box's.
	Rigid bool
}

// Load is the bearing analysis of one box: how much weight rests on it in total,
// and the highest pressure any single contact patch on its top face applies.
// Peak rather than mean, because a box fails if *any* patch over-loads it.
type Load struct {
	Weight   float64
	Pressure float64
}

// NoLimit is the Limit of a box with no bearing restriction. Callers that leave
// Limit at its zero value are declaring "nothing may rest on this" (fragile), so
// an absent limit has to be spelled out explicitly rather than defaulted.
const NoLimit = math.MaxFloat64

// supporter names a box directly beneath another and the contact area between
// their touching faces.
type supporter struct {
	idx  int
	area float64
}

// supportersOf returns the boxes in bs whose top face carries the given
// footprint, with the contact area of each. Empty when the footprint rests on
// the floor or floats. footprintSupport answers "how much of my base is held
// up", which is a different question — it aggregates the supporters away, and
// the bearing rule needs them individually.
func supportersOf(bs []BearBox, x, y, z, w, d float64) []supporter {
	if z <= compactEps {
		return nil // on the floor
	}
	var out []supporter
	for i := range bs {
		b := &bs[i]
		if math.Abs(b.Z+b.H-z) > compactEps {
			continue
		}
		iw := overlap1D(x, x+w, b.X, b.X+b.W)
		id := overlap1D(y, y+d, b.Y, b.Y+b.D)
		if iw > compactEps && id > compactEps {
			out = append(out, supporter{idx: i, area: iw * id})
		}
	}
	return out
}

// BorneLoads returns, for each box in bs, the total weight resting on it
// transitively. Boxes are processed top-down so that by the time a box sheds its
// load onto its supporters it has already received everything above it — this is
// what makes the pass single-shot rather than iterative.
//
// A box's shed load is its own weight plus everything it bears, divided across
// its supporters by contact-area fraction.
func BorneLoads(bs []BearBox) []Load {
	borne := make([]Load, len(bs))
	for _, i := range topDown(bs) {
		sup := supportersOf(bs, bs[i].X, bs[i].Y, bs[i].Z, bs[i].W, bs[i].D)
		if len(sup) == 0 {
			continue // floor, or floating: load leaves the system
		}
		total := 0.0
		for _, s := range sup {
			total += s.area
		}
		if total <= 0 {
			continue
		}
		shed := bs[i].Weight + borne[i].Weight
		for _, s := range sup {
			share := shed * s.area / total
			borne[s.idx].Weight += share
			// Pressure is per contact patch, so it is the share divided by the
			// area that share crosses — not by the supporter's whole top face.
			if p := share / s.area; p > borne[s.idx].Pressure {
				borne[s.idx].Pressure = p
			}
		}
	}
	return borne
}

// topDown returns indices of bs ordered by descending bottom face, so a box is
// always visited before anything it rests on. Ties (boxes at the same height
// cannot support one another) are ordered by index to keep the pass
// deterministic.
func topDown(bs []BearBox) []int {
	idx := make([]int, len(bs))
	for i := range idx {
		idx[i] = i
	}
	// Insertion sort: stacks are shallow and this keeps the pass allocation-free
	// beyond the index slice itself.
	for i := 1; i < len(idx); i++ {
		for j := i; j > 0; j-- {
			a, b := idx[j-1], idx[j]
			if bs[a].Z > bs[b].Z+compactEps || (bs[a].Z >= bs[b].Z-compactEps && a < b) {
				break
			}
			idx[j-1], idx[j] = idx[j], idx[j-1]
		}
	}
	return idx
}

// BearingOK reports whether no box in bs carries more than its Limit. This is
// the whole-configuration predicate: constructive gates check a candidate
// against it, and every relocation post-pass must re-check, since moving a box
// can both relieve a crush and create one.
// It delegates to LoadPathOK so nested and flat configurations are judged by one
// rule. With no box carrying Contents the two are equivalent, which the whole
// flat test suite exercises.
func BearingOK(bs []BearBox) bool { return LoadPathOK(bs) }

// bearEps tolerates float drift in the apportionment sums, so a stack that is
// exactly at its limit is not rejected by rounding. It is an absolute weight
// tolerance, deliberately coarser than compactEps (a length).
const bearEps = 1e-9

// CanBear reports whether placing cand on top of the already-placed bs keeps
// every box within its Limit. bs is assumed already feasible; only the load cand
// adds is propagated, so this is O(supporters × stack depth) rather than a full
// re-solve.
func CanBear(bs []BearBox, cand BearBox) bool {
	if cand.Weight <= 0 {
		return true // weightless candidate crushes nothing
	}
	borne := BorneLoads(bs)
	return addLoad(bs, borne, cand.X, cand.Y, cand.Z, cand.W, cand.D, cand.Weight)
}

// addLoad pushes load down from the footprint at (x,y,z,w,d) through the support
// chain, reporting whether every box en route stays within its Limit. borne is
// the pre-existing load and is not mutated.
func addLoad(bs []BearBox, borne []Load, x, y, z, w, d, load float64) bool {
	if load <= 0 {
		return true
	}
	sup := supportersOf(bs, x, y, z, w, d)
	total := 0.0
	for _, s := range sup {
		total += s.area
	}
	if total <= 0 {
		return true
	}
	for _, s := range sup {
		share := load * s.area / total
		b := &bs[s.idx]
		if borne[s.idx].Weight+share > b.Limit+bearEps {
			return false
		}
		// Pressure is checked against this patch alone. It is not added to the
		// pre-existing peak: two patches loading the same box do not combine into
		// one more concentrated patch.
		if share/s.area > b.PressureLimit+bearEps {
			return false
		}
		// The supporter passes its new share further down along with nothing
		// else: what it already bore was accounted for when borne was built.
		if !addLoad(bs, borne, b.X, b.Y, b.Z, b.W, b.D, share) {
			return false
		}
	}
	return true
}

// BearingSpec configures the load-bearing gate. Scalars are named by the caller
// rather than fixed under reserved keys, matching pack.MinimizeCG's convention
// for the mass scalar. A zero BearingSpec (empty WeightScalar) disables the gate
// entirely, so strategies built without one behave exactly as before.
type BearingSpec struct {
	// WeightScalar names the item scalar holding each item's weight. Empty
	// disables bearing.
	WeightScalar string
	// LimitScalar names the item scalar holding each item's bearing limit.
	LimitScalar string
	// PressureScalar names the item scalar holding each item's maximum pressure
	// (weight per unit contact area). Empty disables the pressure check.
	PressureScalar string
	// DefaultPressure applies to items carrying no PressureScalar value. Unlike
	// DefaultLimit it defaults to NoLimit when the scalar is unnamed, since a
	// pressure cap is an extra restriction rather than the primary rule.
	DefaultPressure float64
	// DefaultLimit applies to items carrying no LimitScalar value. It is
	// deliberately not defaulted to NoLimit: a zero value means "fragile", so a
	// caller who enables bearing but mis-names the limit scalar gets a packing
	// that refuses to stack rather than one that silently permits every crush.
	// Set it to NoLimit for opt-in limits.
	DefaultLimit float64
}

// Enabled reports whether the spec turns the bearing gate on.
func (s BearingSpec) Enabled() bool { return s.WeightScalar != "" }

// bearState is the per-strategy bearing bookkeeping: the placed boxes with their
// weights and limits, plus the weight/limit of the item currently being placed.
// A nil *bearState is the disabled case and every method tolerates it, so the
// strategies carry one pointer field and an untaken branch when bearing is off.
type bearState struct {
	spec  BearingSpec
	boxes []BearBox // parallel to the strategy's own placed slice

	pendWeight, pendLimit, pendPressure float64
	havePending                         bool
}

func newBearState(spec BearingSpec) *bearState {
	if !spec.Enabled() {
		return nil
	}
	return &bearState{spec: spec}
}

// setPendingItem records the weight and limit of the item about to be placed,
// read from its scalars under the spec's names.
func (b *bearState) setPendingItem(scalars map[string]float64) {
	if b == nil {
		return
	}
	b.pendWeight = scalars[b.spec.WeightScalar]
	limit, ok := scalars[b.spec.LimitScalar]
	if !ok {
		limit = b.spec.DefaultLimit
	}
	b.pendLimit = limit
	b.pendPressure = b.spec.pressureOf(scalars)
	b.havePending = true
}

// pressureOf resolves an item's pressure cap, or NoLimit when the spec names no
// pressure scalar.
func (s BearingSpec) pressureOf(scalars map[string]float64) float64 {
	if s.PressureScalar == "" {
		return NoLimit
	}
	if v, ok := scalars[s.PressureScalar]; ok {
		return v
	}
	if s.DefaultPressure == 0 {
		return NoLimit
	}
	return s.DefaultPressure
}

// allows reports whether placing the pending item at this footprint keeps every
// box below within its limit.
//
// With no pending item set it refuses. Bin3D.TryPlace always sets one before
// TryInsert; a caller driving a bearing-enabled strategy directly and skipping
// that step would otherwise have its items treated as weightless, which fails
// open — the wrong direction for a constraint whose job is to prevent damage.
func (b *bearState) allows(x, y, z, w, d, h float64) bool {
	if b == nil {
		return true
	}
	if !b.havePending {
		return false
	}
	return CanBear(b.boxes, BearBox{
		X: x, Y: y, Z: z, W: w, D: d, H: h,
		Weight: b.pendWeight, Limit: b.pendLimit, PressureLimit: b.pendPressure,
	})
}

// commit records a placed box, consuming the pending item's weight and limit.
func (b *bearState) commit(x, y, z, w, d, h float64) {
	if b == nil {
		return
	}
	b.boxes = append(b.boxes, BearBox{
		X: x, Y: y, Z: z, W: w, D: d, H: h,
		Weight: b.pendWeight, Limit: b.pendLimit, PressureLimit: b.pendPressure,
	})
	b.havePending = false
}

// occupy records a pre-existing obstruction — a box the strategy was seeded with
// rather than one it placed. It has no weight of its own and bears anything,
// which is right for a fixture (a pillar, a pre-loaded pallet) but wrong for
// re-seeding a real packing: weights must be restored explicitly for that.
func (b *bearState) occupy(x, y, z, w, d, h float64) {
	if b == nil {
		return
	}
	b.boxes = append(b.boxes, BearBox{X: x, Y: y, Z: z, W: w, D: d, H: h, Limit: NoLimit, PressureLimit: NoLimit})
}

// pendingBearer is the optional strategy extension through which Bin3D hands the
// item's scalars to a bearing-enabled strategy. Keeping it optional is what lets
// weight reach the placement decision without touching PlacementStrategy3D,
// which has six implementations and callers in three packages.
type pendingBearer interface {
	setPendingItem(scalars map[string]float64)
}

// WithBearing enables the load-bearing gate on a strategy, returning it for
// chaining. Strategies that do not support bearing are returned unchanged, so a
// caller must check Bearable first if silent no-op is unacceptable.
//
// A disabled spec is a no-op, which keeps "bearing off" byte-identical to a
// strategy built without it.
func WithBearing(s PlacementStrategy3D, spec BearingSpec) PlacementStrategy3D {
	if !spec.Enabled() {
		return s
	}
	switch t := s.(type) {
	case *ExtremePoint:
		t.bear = newBearState(spec)
	case *EmptyMaximalSpace:
		t.bear = newBearState(spec)
	case *BottomLeftFill:
		t.bear = newBearState(spec)
	case *Heightmap:
		t.bear = newBearState(spec)
	}
	return s
}

// Bearable reports whether a strategy enforces the load-bearing gate when given
// a BearingSpec. The strategies that do are exactly those whose placement runs
// through their own candidate loop; the self-managing packers (blocks, columns,
// assemble, LAFF) build placements directly and are not covered — see
// docs/plans/load-bearing-stacking.md §4.A.3.
func Bearable(s PlacementStrategy3D) bool {
	switch s.(type) {
	case *ExtremePoint, *EmptyMaximalSpace, *BottomLeftFill, *Heightmap:
		return true
	}
	return false
}

// BearingStrategy wraps a Factory3D-compatible strategy constructor so every bin
// it opens enforces the bearing gate.
func BearingStrategy(ctor func(w, d, h float64) PlacementStrategy3D, spec BearingSpec) func(w, d, h float64) PlacementStrategy3D {
	if !spec.Enabled() {
		return ctor
	}
	return func(w, d, h float64) PlacementStrategy3D {
		return WithBearing(ctor(w, d, h), spec)
	}
}

// BearBoxesOf converts placements into BearBoxes for whole-configuration
// validation, reading each item's weight and limit from scalars under the spec's
// names. Placements whose item is absent from items are skipped.
//
// This is how a caller checks a finished packing — including one produced by an
// algorithm the constructive gate does not cover, or mutated by a relocating
// post-pass.
func BearBoxesOf(ps []*Placement3D, scalars map[string]map[string]float64, spec BearingSpec) []BearBox {
	out := make([]BearBox, 0, len(ps))
	for _, p := range ps {
		sc, ok := scalars[p.itemID]
		if !ok {
			continue
		}
		limit, ok := sc[spec.LimitScalar]
		if !ok {
			limit = spec.DefaultLimit
		}
		out = append(out, BearBox{
			X: p.X, Y: p.Y, Z: p.Z, W: p.W, D: p.D, H: p.H,
			Weight: sc[spec.WeightScalar], Limit: limit,
			PressureLimit: spec.pressureOf(sc),
		})
	}
	return out
}

// BearingGuard re-validates a whole bin after a relocation. The post-passes
// (Compact, RefineVoids) move items that were placed under the constructive
// gate, and a move can create a crush the gate never saw — so every relocation
// has to be checked against the final configuration, not just the placement that
// produced it.
//
// A nil *BearingGuard permits everything, which is the disabled case.
type BearingGuard struct {
	spec    BearingSpec
	scalars map[string]map[string]float64
	buf     []BearBox // reused across checks; guarded calls are hot in Compact
}

// NewBearingGuard builds a guard from the item scalars, keyed by item ID.
// Returns nil (a permissive guard) when the spec is disabled.
func NewBearingGuard(spec BearingSpec, scalars map[string]map[string]float64) *BearingGuard {
	if !spec.Enabled() {
		return nil
	}
	return &BearingGuard{spec: spec, scalars: scalars}
}

// OK reports whether the bin satisfies every item's bearing limit.
func (g *BearingGuard) OK(ps []*Placement3D) bool {
	if g == nil {
		return true
	}
	g.buf = g.buf[:0]
	for _, p := range ps {
		sc, ok := g.scalars[p.itemID]
		if !ok {
			continue
		}
		limit, ok := sc[g.spec.LimitScalar]
		if !ok {
			limit = g.spec.DefaultLimit
		}
		g.buf = append(g.buf, BearBox{
			X: p.X, Y: p.Y, Z: p.Z, W: p.W, D: p.D, H: p.H,
			Weight: sc[g.spec.WeightScalar], Limit: limit,
			PressureLimit: g.spec.pressureOf(sc),
		})
	}
	return BearingOK(g.buf)
}

// ScalarsByID collects item scalars into the form BearingGuard and BearBoxesOf
// expect.
func ScalarsByID(items []pack.Item) map[string]map[string]float64 {
	out := make(map[string]map[string]float64, len(items))
	for _, it := range items {
		out[it.ID()] = pack.ScalarsOf(it)
	}
	return out
}

// SettleGuarded is Settle with a load-bearing guard. Settle drops every item at
// once, so there is no per-move accept step: either the whole drop holds or the
// bin is left as it was. A nil guard makes this exactly Settle.
func SettleGuarded(ps []*Placement3D, guard *BearingGuard) {
	if guard == nil {
		Settle(ps)
		return
	}
	old := make([]float64, len(ps))
	for i, p := range ps {
		old[i] = p.Z
	}
	Settle(ps)
	if !guard.OK(ps) {
		for i, p := range ps {
			p.Z = old[i]
		}
	}
}
