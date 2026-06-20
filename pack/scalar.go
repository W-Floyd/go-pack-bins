package pack

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

// ── constraint constructors ───────────────────────────────────────────────────

// MaxAggregate returns a Constraint that rejects placement when the bin's
// accumulated total of name, plus the item's contribution, would exceed limit.
func MaxAggregate(name string, limit float64) Constraint {
	return ConstraintFunc(func(binAgg, itemScalars map[string]float64) bool {
		return binAgg[name]+itemScalars[name] <= limit
	})
}

// MinAggregate returns a Constraint that rejects placement when the bin's
// accumulated total of name plus the item's contribution would fall below floor.
func MinAggregate(name string, floor float64) Constraint {
	return ConstraintFunc(func(binAgg, itemScalars map[string]float64) bool {
		return binAgg[name]+itemScalars[name] >= floor
	})
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
// of name — bins that already contain high values are preferred.
func ColocateHigh(name string) Preference {
	return func(binAgg, _ map[string]float64) float64 { return binAgg[name] }
}

// ColocateLow returns a Preference that prefers bins with the lowest aggregate.
func ColocateLow(name string) Preference {
	return func(binAgg, _ map[string]float64) float64 { return -binAgg[name] }
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
