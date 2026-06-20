package online

import (
	"errors"
	"sort"

	"github.com/W-Floyd/go-pack-bins/pack"
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

func (s pfSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int, error) {
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
		p, err := bins[c.idx].TryPlace(item)
		if err == nil {
			return p, c.idx, nil
		}
		if !errors.Is(err, pack.ErrNoRoom) {
			return nil, -1, err // propagate permanent error
		}
		// ErrNoRoom: continue to next bin
	}
	return nil, -1, nil
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

// pfNormSelector scores like pfSelector but min-max normalizes each preference's
// raw value across the candidate bins to [0,1] before applying its weight and
// summing. This makes weights comparable across preferences whose raw scales
// differ wildly (e.g. utilization in [0,1] vs a weight total in the hundreds),
// so blending "pack tight" with "balance weight" behaves predictably.
type pfNormSelector struct {
	prefs   []pack.Preference
	weights []float64
}

func (s pfNormSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int, error) {
	itemScalars := pack.ScalarsOf(item)
	vol := item.Volume()

	var idxs []int
	var aggs []map[string]float64
	for i, b := range bins {
		if b.Remaining() < vol {
			continue
		}
		idxs = append(idxs, i)
		aggs = append(aggs, aggregatesOf(b))
	}

	scores := make([]float64, len(idxs))
	for pi, pref := range s.prefs {
		w := 1.0
		if pi < len(s.weights) {
			w = s.weights[pi]
		}
		// Raw value of this preference at each candidate, then min-max normalize.
		raw := make([]float64, len(idxs))
		min, max := 0.0, 0.0
		for k := range idxs {
			raw[k] = pref(aggs[k], itemScalars)
			if k == 0 || raw[k] < min {
				min = raw[k]
			}
			if k == 0 || raw[k] > max {
				max = raw[k]
			}
		}
		span := max - min
		for k := range idxs {
			norm := 0.0
			if span > 0 {
				norm = (raw[k] - min) / span
			}
			scores[k] += w * norm
		}
	}

	order := make([]int, len(idxs))
	for k := range order {
		order[k] = k
	}
	sort.SliceStable(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })

	for _, k := range order {
		p, err := bins[idxs[k]].TryPlace(item)
		if err == nil {
			return p, idxs[k], nil
		}
		if !errors.Is(err, pack.ErrNoRoom) {
			return nil, -1, err
		}
	}
	return nil, -1, nil
}

// PreferenceFitNorm is like PreferenceFit but min-max normalizes each preference
// across the candidate bins before weighting, so weights set the relative pull
// of preferences on different scales. weights is positional (weights[i] applies
// to prefs[i]); a missing/short entry defaults to 1.
func PreferenceFitNorm(factory pack.BinFactory, prefs []pack.Preference, weights []float64) *Packer {
	return NewPacker("PFN", pfNormSelector{prefs: prefs, weights: weights}, factory)
}
