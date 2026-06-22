package pack

import "fmt"

// Incompatible returns a Constraint that forbids a bin from holding items of two
// categories declared incompatible — the "manifest" rule from
// skjolber/3d-bin-container-packing (Apache-2.0; see ATTRIBUTION.md for the pinned commit): never co-pack e.g. lighters
// and dynamite. Each item's category is the value of the named scalar; an item
// without that scalar (value 0) is uncategorised and compatible with everything.
// pairs lists unordered category-value pairs that must never share a bin.
//
// Incompatibility is never a permanent rejection (an item can always go in its
// own bin), so a violating item is offered to another bin (ErrNoRoom).
func Incompatible(catScalar string, pairs ...[2]float64) Constraint {
	return &incompatibleConstraint{cat: catScalar, pairs: pairs}
}

type incompatibleConstraint struct {
	cat   string
	pairs [][2]float64
}

// presentKey is the reserved binAgg key holding the count of placed items whose
// category equals v. Stateful, mirroring AllSame's reserved-key approach.
func (c *incompatibleConstraint) presentKey(v float64) string {
	return fmt.Sprintf("\x00inc:%s:%g", c.cat, v)
}

func (c *incompatibleConstraint) present(binAgg map[string]float64, v float64) bool {
	return binAgg[c.presentKey(v)] > 0
}

func (c *incompatibleConstraint) Check(binAgg, itemScalars map[string]float64) bool {
	cv, ok := itemScalars[c.cat]
	if !ok || cv == 0 {
		return true // uncategorised — compatible with anything
	}
	for _, p := range c.pairs {
		switch cv {
		case p[0]:
			if c.present(binAgg, p[1]) {
				return false
			}
		case p[1]:
			if c.present(binAgg, p[0]) {
				return false
			}
		}
	}
	return true
}

func (c *incompatibleConstraint) Apply(binAgg, itemScalars map[string]float64) {
	if cv, ok := itemScalars[c.cat]; ok && cv != 0 {
		binAgg[c.presentKey(cv)]++
	}
}

func (c *incompatibleConstraint) Revert(binAgg, itemScalars map[string]float64) {
	if cv, ok := itemScalars[c.cat]; ok && cv != 0 {
		binAgg[c.presentKey(cv)]--
	}
}

func (c *incompatibleConstraint) Describe(binAgg, itemScalars map[string]float64) string {
	return fmt.Sprintf("item category %s=%.6g is incompatible with an item already in the bin",
		c.cat, itemScalars[c.cat])
}

var (
	_ Constraint          = (*incompatibleConstraint)(nil)
	_ StatefulConstraint  = (*incompatibleConstraint)(nil)
	_ ConstraintDescriber = (*incompatibleConstraint)(nil)
)
