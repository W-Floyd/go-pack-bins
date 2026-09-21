package d3

import "math"

// Access cost: how expensive it is to retrieve an item once it is packed.
//
// Packing usually optimises what fits. A store room optimises what you can get
// at: the things you reach for weekly belong at waist height by the door, and
// the things you touch once a year can go high at the back. That is a different
// objective from volume, and it is a *soft* one — a bad arrangement still packs.
//
// The cost of a position is a weighted sum of terms, so a caller can describe
// their own space: height off the floor, walking distance from the door,
// reaching past other things, the fixed cost of opening a sealed carton versus
// sliding a drawer, and interactions like "heavy and high is worse than either
// alone". The objective is to minimise the total over all items, each weighted
// by how often it is wanted.

// AccessCost describes what makes a position expensive to reach. Every term is
// a coefficient in the caller's own units; a zero AccessCost costs nothing
// everywhere, which disables the objective.
type AccessCost struct {
	// Fixed is the cost of opening this container at all, whatever is inside and
	// wherever it sits — a taped carton costs more than a drawer.
	Fixed float64
	// PerHeight is charged per unit of the item's height off the floor. Its base
	// is used rather than its centre: what you have to lift to is where it sits.
	PerHeight float64
	// PerDistance is charged per unit of horizontal distance from the access
	// point — the walk to the far corner of the room.
	PerDistance float64
	// AccessX, AccessY locate the point items are reached from (the doorway), in
	// the bin's own coordinates.
	AccessX, AccessY float64
	// PerScalar charges a coefficient per unit of a named item scalar, for costs
	// that depend on the item rather than the place: an awkward or fragile thing
	// is slower to handle wherever it is.
	PerScalar map[string]float64
	// PerHeightScalar charges coefficient × scalar × height. This is the
	// interaction the additive terms cannot express: lifting something heavy to
	// shoulder height is worse than the height and the weight separately suggest.
	PerHeightScalar map[string]float64
}

// Zero reports whether the cost is uniformly zero, so callers can skip the
// objective entirely.
func (a AccessCost) Zero() bool {
	return a.Fixed == 0 && a.PerHeight == 0 && a.PerDistance == 0 &&
		len(a.PerScalar) == 0 && len(a.PerHeightScalar) == 0
}

// At returns the cost of reaching an item of the given scalars placed with its
// min corner at (x,y,z) and footprint (w,d).
func (a AccessCost) At(x, y, z, w, d float64, scalars map[string]float64) float64 {
	cost := a.Fixed + a.PerHeight*z

	if a.PerDistance != 0 {
		// Measure to the item's nearest face rather than its centre: you reach
		// the front of a box, not through it.
		dx := axisGap(a.AccessX, x, x+w)
		dy := axisGap(a.AccessY, y, y+d)
		cost += a.PerDistance * math.Hypot(dx, dy)
	}
	for name, coeff := range a.PerScalar {
		cost += coeff * scalars[name]
	}
	for name, coeff := range a.PerHeightScalar {
		cost += coeff * scalars[name] * z
	}
	return cost
}

// axisGap is the distance from p to the interval [lo,hi], zero inside it.
func axisGap(p, lo, hi float64) float64 {
	switch {
	case p < lo:
		return lo - p
	case p > hi:
		return p - hi
	default:
		return 0
	}
}

// OfCandidate is At for a candidate placement.
func (a AccessCost) OfCandidate(c Candidate, scalars map[string]float64) float64 {
	return a.At(c.X, c.Y, c.Z, c.W, c.D, scalars)
}

// AccessFrequency is how often an item is wanted, read from a named scalar.
// Items with no value take dflt, so a caller can mark the few things they reach
// for often and leave the rest alone.
func AccessFrequency(scalars map[string]float64, name string, dflt float64) float64 {
	if name == "" {
		return dflt
	}
	if v, ok := scalars[name]; ok {
		return v
	}
	return dflt
}

// TotalAccessCost is the objective: the sum over placed items of how often each
// is wanted times what it costs to reach where it ended up.
//
// It is what a caller compares two packings by, and what the demo reports. The
// packer minimises it greedily rather than exactly — an exact solution is an
// assignment problem over positions that do not exist until earlier items are
// placed.
func TotalAccessCost(ps []*Placement3D, cost AccessCost, scalars map[string]map[string]float64,
	freqScalar string, freqDefault float64) float64 {

	var total float64
	for _, p := range ps {
		sc := scalars[p.itemID]
		total += AccessFrequency(sc, freqScalar, freqDefault) * cost.At(p.X, p.Y, p.Z, p.W, p.D, sc)
	}
	return total
}
