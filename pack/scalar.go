package pack

import "fmt"

// Scalar is implemented by items that carry named numeric properties
// (weight, value, fragility, etc.). Returning a nil or empty map is valid.
type Scalar interface {
	Scalars() map[string]float64
}

// Constraint is a hard rule evaluated before placing an item in a bin.
// binAgg holds the bin's current accumulated scalar totals; itemScalars holds
// the candidate item's scalars. Returns false if placement would violate the rule.
//
// Simple aggregate checks can be constructed with ConstraintFunc; stateful
// constraints (e.g. AllSame) implement the unexported statefulConstraint
// extension so ConstrainedBin and BinCompletion can apply and revert per-bin state.
type Constraint interface {
	Check(binAgg, itemScalars map[string]float64) bool
}

// ConstraintDescriber is an optional extension for Constraints that can
// produce a human-readable description of why a constraint failed.
type ConstraintDescriber interface {
	Describe(binAgg, itemScalars map[string]float64) string
}

// statefulConstraint is an optional extension for Constraints that maintain
// per-bin state stored in binAgg under reserved keys.
// apply is called after a successful placement; revert undoes it (for backtracking).
type StatefulConstraint interface {
	Constraint
	Apply(binAgg, itemScalars map[string]float64)
	Revert(binAgg, itemScalars map[string]float64)
}

// ConstraintFunc wraps a plain function as a stateless Constraint.
type ConstraintFunc func(binAgg, itemScalars map[string]float64) bool

func (f ConstraintFunc) Check(binAgg, itemScalars map[string]float64) bool {
	return f(binAgg, itemScalars)
}

// Preference scores how desirable it is to place item in bin (higher = better).
// Used by PreferenceFit to rank candidate bins.
type Preference func(binAgg, itemScalars map[string]float64) float64

// BinMetricer is an optional Bin extension that reports geometric metrics (as
// opposed to scalar accumulations) for preference scoring — e.g. the current
// stack height. ConstrainedBin merges these into the map passed to preferences,
// so a Preference can score on them via their reserved key.
type BinMetricer interface {
	Metrics() map[string]float64
}

// MetricPeakHeight is the reserved key under which a BinMetricer reports the
// current peak stack height of a bin (the highest top face of any placed item).
const MetricPeakHeight = "\x00m:peak_height"

// CGHeightNumeratorKey returns the reserved metric key under which a BinMetricer
// reports the mass-weighted vertical moment for a scalar — i.e. the running sum
// of (scalar value × vertical centre) over placed items. Dividing this by the
// bin's accumulated total of the same scalar yields the centre-of-gravity height.
func CGHeightNumeratorKey(scalar string) string { return "\x00m:cgz:" + scalar }

// ── constraint constructors ───────────────────────────────────────────────────

// maxAggConstraint enforces an upper bound on the accumulated total of a named scalar.
type maxAggConstraint struct {
	name  string
	limit float64
}

func (c maxAggConstraint) Check(binAgg, itemScalars map[string]float64) bool {
	return binAgg[c.name]+itemScalars[c.name] <= c.limit
}

func (c maxAggConstraint) Describe(binAgg, itemScalars map[string]float64) string {
	return fmt.Sprintf("item %s=%.6g would exceed max aggregate %.6g (bin already at %.6g)",
		c.name, itemScalars[c.name], c.limit, binAgg[c.name])
}

// MaxAggregate returns a Constraint that rejects placement when the bin's
// accumulated total of name, plus the item's contribution, would exceed limit.
func MaxAggregate(name string, limit float64) Constraint {
	return maxAggConstraint{name: name, limit: limit}
}

// minAggConstraint enforces a lower bound on the accumulated total of a named scalar.
type minAggConstraint struct {
	name  string
	floor float64
}

func (c minAggConstraint) Check(binAgg, itemScalars map[string]float64) bool {
	return binAgg[c.name]+itemScalars[c.name] >= c.floor
}

func (c minAggConstraint) Describe(binAgg, itemScalars map[string]float64) string {
	return fmt.Sprintf("item %s=%.6g would fall below min aggregate %.6g (bin already at %.6g)",
		c.name, itemScalars[c.name], c.floor, binAgg[c.name])
}

// MinAggregate returns a Constraint that rejects placement when the bin's
// accumulated total of name plus the item's contribution would fall below floor.
func MinAggregate(name string, floor float64) Constraint {
	return minAggConstraint{name: name, floor: floor}
}

// AllSame returns a Constraint that requires every item placed in a bin to
// carry the same value for the named scalar. The first item placed sets the
// reference; subsequent items are rejected if their value differs.
func AllSame(name string) Constraint {
	return allSameConstraint{name: name}
}

type allSameConstraint struct{ name string }

func (c allSameConstraint) refVal() string   { return "\x00as:" + c.name }
func (c allSameConstraint) refCount() string { return "\x00as:" + c.name + ":n" }

func (c allSameConstraint) Check(binAgg, itemScalars map[string]float64) bool {
	if binAgg[c.refCount()] == 0 {
		return true // bin is empty for this scalar — anything is allowed
	}
	return itemScalars[c.name] == binAgg[c.refVal()]
}

func (c allSameConstraint) Describe(binAgg, itemScalars map[string]float64) string {
	return fmt.Sprintf("item %s=%.6g does not match bin's established value %.6g (AllSame constraint)",
		c.name, itemScalars[c.name], binAgg[c.refVal()])
}

func (c allSameConstraint) Apply(binAgg, itemScalars map[string]float64) {
	if binAgg[c.refCount()] == 0 {
		binAgg[c.refVal()] = itemScalars[c.name]
	}
	binAgg[c.refCount()]++
}

func (c allSameConstraint) Revert(binAgg, itemScalars map[string]float64) {
	binAgg[c.refCount()]--
	if binAgg[c.refCount()] == 0 {
		delete(binAgg, c.refVal())
	}
}

var _ StatefulConstraint = allSameConstraint{}

// ── preference constructors ───────────────────────────────────────────────────

// ColocateHigh returns a Preference that scores bins by their current aggregate
// of name — bins that already contain high values are preferred. This concentrates
// a scalar: it fills the fullest bin first, then the next (e.g. "top off pallet 1
// before opening pallet 2").
func ColocateHigh(name string) Preference {
	return func(binAgg, _ map[string]float64) float64 { return binAgg[name] }
}

// ColocateLow returns a Preference that prefers bins with the lowest aggregate.
// This balances a scalar: each item goes to the emptiest bin, driving the bins
// toward roughly equal totals (e.g. even weight distribution across containers).
func ColocateLow(name string) Preference {
	return func(binAgg, _ map[string]float64) float64 { return -binAgg[name] }
}

// MinimizeHeight returns a Preference that prefers bins with the lowest current
// stack height, keeping loads flat. It scores on MetricPeakHeight, so it only
// has effect when packing into bins that implement BinMetricer (e.g. the 3-D
// extreme-point bin); for bins that don't report it, every bin scores 0.
//
// Wrap with Weighted to trade flatness off against other objectives.
func MinimizeHeight() Preference {
	return func(binAgg, _ map[string]float64) float64 { return -binAgg[MetricPeakHeight] }
}

// MinimizeCG returns a Preference that prefers bins with the lowest current
// centre-of-gravity height for the given mass scalar (e.g. "weight"), keeping
// loads bottom-heavy and stable. The CG height is the mass-weighted mean
// vertical centre of placed items: CGHeightNumeratorKey(massScalar) / massScalar.
//
// It only has effect with bins that report these metrics (the 3-D extreme-point
// bin); empty or mass-less bins score 0. Wrap with Weighted to trade stability
// off against other objectives.
func MinimizeCG(massScalar string) Preference {
	numKey := CGHeightNumeratorKey(massScalar)
	return func(binAgg, _ map[string]float64) float64 {
		mass := binAgg[massScalar]
		if mass == 0 {
			return 0
		}
		return -(binAgg[numKey] / mass)
	}
}

// Weighted scales a Preference's score by w. Because PreferenceFit sums all
// preferences, weights set the relative pull of competing objectives — e.g.
//
//	online.PreferenceFit(factory,
//		pack.Weighted(pack.ColocateLow("weight"), 2),  // balancing weight matters most
//		pack.ColocateHigh("value"))                    // grouping value is secondary
//
// A negative weight inverts a preference (Weighted(ColocateHigh(n), -1) behaves
// like ColocateLow(n)); a zero weight disables it.
func Weighted(p Preference, w float64) Preference {
	return func(binAgg, itemScalars map[string]float64) float64 {
		return w * p(binAgg, itemScalars)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// ScalarsOf extracts the scalar map from item if it implements Scalar.
func ScalarsOf(item Item) map[string]float64 {
	if s, ok := item.(Scalar); ok {
		return s.Scalars()
	}
	return nil
}

// ApplyConstraints calls Apply on any StatefulConstraint in cs.
func ApplyConstraints(cs []Constraint, binAgg, itemScalars map[string]float64) {
	for _, c := range cs {
		if sc, ok := c.(StatefulConstraint); ok {
			sc.Apply(binAgg, itemScalars)
		}
	}
}

// RevertConstraints calls Revert on any StatefulConstraint in cs.
func RevertConstraints(cs []Constraint, binAgg, itemScalars map[string]float64) {
	for _, c := range cs {
		if sc, ok := c.(StatefulConstraint); ok {
			sc.Revert(binAgg, itemScalars)
		}
	}
}
