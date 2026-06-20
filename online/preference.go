package online

import (
	"sort"

	"github.com/wfloyd/go-pack-bins/pack"
)

// pfSelector implements a preference-scored bin selector.
// Candidate bins (those where TryPlace would succeed geometrically, i.e.
// Remaining() >= item.Volume()) are scored by summing all Preference functions.
// The highest-scoring bin is tried first; ties are broken by bin order.
//
// If the bin implements an Aggregates() map (i.e. is a *pack.ConstrainedBin),
// the preference functions receive the live aggregate totals. Otherwise they
// receive a nil map.
type pfSelector struct {
	prefs []pack.Preference
}

type aggregator interface {
	Aggregates() map[string]float64
}

func aggregatesOf(bin pack.Bin) map[string]float64 {
	if a, ok := bin.(aggregator); ok {
		return a.Aggregates()
	}
	return nil
}

func (s pfSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int) {
	type cand struct {
		idx   int
		score float64
	}

	itemScalars := pack.ScalarsOf(item)
	vol := item.Volume()

	var candidates []cand
	for i, b := range bins {
		if b.Remaining() < vol {
			continue
		}
		score := 0.0
		if len(s.prefs) > 0 {
			agg := aggregatesOf(b)
			for _, pref := range s.prefs {
				score += pref(agg, itemScalars)
			}
		}
		candidates = append(candidates, cand{i, score})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	for _, c := range candidates {
		if p, ok := bins[c.idx].TryPlace(item); ok {
			return p, c.idx
		}
	}
	return nil, -1
}

// PreferenceFit returns a packer that selects bins by summing all supplied
// Preference functions and choosing the highest scorer. It degenerates to
// First Fit when no preferences are supplied.
//
// Pair with pack.NewConstrainedFactory to enforce hard constraints while
// steering placement with soft preferences.
func PreferenceFit(factory pack.BinFactory, prefs ...pack.Preference) *Packer {
	return NewPacker("PF", pfSelector{prefs: prefs}, factory)
}
