package pack

// ConstrainedBin wraps any Bin and enforces scalar Constraints.
// It accumulates scalar totals for all placed items so constraints and
// preferences can inspect the bin's current state.
type ConstrainedBin struct {
	Bin
	agg         map[string]float64
	constraints []Constraint
}

// NewConstrainedBin wraps bin so that TryPlace enforces all constraints.
func NewConstrainedBin(bin Bin, constraints []Constraint) *ConstrainedBin {
	return &ConstrainedBin{
		Bin:         bin,
		agg:         make(map[string]float64),
		constraints: constraints,
	}
}

// TryPlace checks every constraint before delegating to the wrapped bin.
// On success it accumulates the item's scalars and applies any stateful constraints.
func (c *ConstrainedBin) TryPlace(item Item) (Placement, bool) {
	itemScalars := ScalarsOf(item)
	for _, con := range c.constraints {
		if !con.Check(c.agg, itemScalars) {
			return nil, false
		}
	}
	p, ok := c.Bin.TryPlace(item)
	if ok {
		for k, v := range itemScalars {
			c.agg[k] += v
		}
		ApplyConstraints(c.constraints, c.agg, itemScalars)
	}
	return p, ok
}

// Aggregate returns the accumulated total of the named scalar across all placed items.
func (c *ConstrainedBin) Aggregate(name string) float64 { return c.agg[name] }

// Aggregates returns a snapshot of all accumulated scalar totals.
func (c *ConstrainedBin) Aggregates() map[string]float64 {
	out := make(map[string]float64, len(c.agg))
	for k, v := range c.agg {
		out[k] = v
	}
	return out
}

var _ Bin = (*ConstrainedBin)(nil)

// ConstrainedFactory wraps any BinFactory so every opened bin enforces constraints.
type ConstrainedFactory struct {
	BinFactory
	constraints []Constraint
}

// NewConstrainedFactory wraps factory. Every bin it opens will enforce constraints.
func NewConstrainedFactory(factory BinFactory, constraints ...Constraint) *ConstrainedFactory {
	return &ConstrainedFactory{BinFactory: factory, constraints: constraints}
}

func (f *ConstrainedFactory) Open() Bin {
	return NewConstrainedBin(f.BinFactory.Open(), f.constraints)
}

var _ BinFactory = (*ConstrainedFactory)(nil)
