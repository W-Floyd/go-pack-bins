// Package joint provides a 3-D packer that decides bin selection and placement
// position together under a single multi-objective score.
package joint

import (
	"fmt"
	"math"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

const eps = 1e-9

// JointFit chooses, for each item, the bin AND the placement position that jointly
// maximise one multi-objective score — rather than picking a bin first and a spot
// second. Each soft preference (scalar balance, centre-of-gravity, height, fill —
// anything expressible as a pack.Preference over bin aggregates) is evaluated on
// the PROJECTED post-placement aggregates of every candidate position, so the same
// objectives that drive bin selection also rank placements. An anti-slosh
// neighbour-contact term is blended in when the contact spec asks for it. Each
// objective is min-max normalised across the candidate set and weighted, exactly
// like online.PreferenceFitNorm, then summed.
//
// Hard rules filter candidates before scoring: the Bottom / NoFloating support
// gate (enforced by the extreme-point strategy) and any scalar Constraints. To
// give balancing preferences room to spread load, JointFit pre-opens K bins (K
// learned from a quick FFD) and opens an overflow bin only when an item fits none.
type JointFit struct {
	w, d, h     float64
	spec        d3.ContactSpec
	prefs       []pack.Preference
	weights     []float64
	constraints []pack.Constraint
	sloshWeight float64 // weight of the normalised neighbour-contact term (0 = off)
	observer    pack.PlaceObserver
	binSeq      int
}

// New builds a JointFit for w×d×h bins. spec carries the support gate and the
// SideX/SideY anti-slosh targets; prefs/weights are the soft objectives (parallel
// slices); constraints are hard scalar caps.
func New(w, d, h float64, spec d3.ContactSpec, prefs []pack.Preference, weights []float64, constraints []pack.Constraint) *JointFit {
	return &JointFit{
		w: w, d: d, h: h, spec: spec, prefs: prefs, weights: weights,
		constraints: constraints, sloshWeight: math.Max(spec.SideX, spec.SideY),
	}
}

func (j *JointFit) Name() string                  { return "joint" }
func (j *JointFit) Observe(fn pack.PlaceObserver)  { j.observer = fn }

var _ pack.OfflinePacker = (*JointFit)(nil)
var _ pack.Observable = (*JointFit)(nil)

// jbin is one open container: its bin, the strategy that enumerates placements,
// the accumulated scalar totals (for constraints and balance), and item count.
type jbin struct {
	bin   *d3.Bin3D
	ep    *d3.ExtremePoint
	sums  map[string]float64
	count int
}

func (j *JointFit) newBin() *jbin {
	j.binSeq++
	strat := d3.NewExtremePointStrategyContact(j.spec)(j.w, j.d, j.h)
	return &jbin{
		bin:  d3.NewBin(fmt.Sprintf("joint-bin-%d", j.binSeq), j.w, j.d, j.h, strat),
		ep:   strat.(*d3.ExtremePoint),
		sums: map[string]float64{},
	}
}

// learnK estimates a starting bin count with a quick First-Fit-Decreasing pass
// using the same support gate, so balancing preferences have several bins to
// spread across from the first item (overflow bins open on demand beyond it).
func (j *JointFit) learnK(items []pack.Item) int {
	factory := d3.NewFactory(j.w, j.d, j.h, d3.NewExtremePointStrategyContact(j.spec))
	r, _ := offline.FirstFitDecreasing(factory).PackAll(items)
	if k := r.BinsUsed(); k > 1 {
		return k
	}
	return 1
}

func (j *JointFit) binFeasible(b *jbin, itemScalars map[string]float64) bool {
	for _, c := range j.constraints {
		if !c.Check(b.sums, itemScalars) {
			return false
		}
	}
	return true
}

// project builds the bin aggregates as they would be AFTER placing the item at
// candidate c — scalar totals, item count, peak height, utilization and the
// per-scalar centre-of-gravity numerator — so a pack.Preference scores the
// resulting state of this specific placement.
func (j *JointFit) project(b *jbin, c d3.Candidate, itemScalars map[string]float64, itemVol, binVol float64) map[string]float64 {
	proj := make(map[string]float64, len(b.sums)+len(itemScalars)+4)
	for k, v := range b.sums {
		proj[k] = v
	}
	for k, v := range b.bin.Metrics() { // current peak + CG numerators
		proj[k] = v
	}
	for k, v := range itemScalars {
		proj[k] += v
	}
	proj[pack.MetricItemCount] = float64(b.count + 1)
	if top := c.Z + c.H; top > proj[pack.MetricPeakHeight] {
		proj[pack.MetricPeakHeight] = top
	}
	proj[pack.MetricUtilization] = (b.ep.Utilization()*binVol + itemVol) / binVol
	centerZ := c.Z + c.H/2
	for k, v := range itemScalars {
		proj[pack.CGHeightNumeratorKey(k)] += v * centerZ
	}
	return proj
}

type scored struct {
	b    *jbin
	c    d3.Candidate
	proj map[string]float64
}

// best returns the index of the candidate maximising the weighted sum of
// min-max-normalised objective scores (preferences on projected aggregates plus
// the neighbour-contact term). Ties keep the earliest candidate — the first
// (lowest-indexed) bin and, within it, the lowest extreme point.
func (j *JointFit) best(cands []scored, itemScalars map[string]float64) int {
	n := len(cands)
	if n == 1 {
		return 0
	}
	total := make([]float64, n)
	raw := make([]float64, n)

	accumulate := func(weight float64, value func(int) float64) {
		mn, mx := math.Inf(1), math.Inf(-1)
		for i := range cands {
			raw[i] = value(i)
			mn, mx = math.Min(mn, raw[i]), math.Max(mx, raw[i])
		}
		if mx-mn <= eps {
			return // no signal from this objective
		}
		for i := range cands {
			total[i] += weight * (raw[i] - mn) / (mx - mn)
		}
	}

	for pi := range j.prefs {
		pref, w := j.prefs[pi], j.weights[pi]
		accumulate(w, func(i int) float64 { return pref(cands[i].proj, itemScalars) })
	}
	if j.sloshWeight > 0 {
		accumulate(j.sloshWeight, func(i int) float64 { return cands[i].c.Lateral })
	}

	bestIdx := 0
	for i := 1; i < n; i++ {
		if total[i] > total[bestIdx]+eps {
			bestIdx = i
		}
	}
	return bestIdx
}

func (j *JointFit) PackAll(items []pack.Item) (pack.Result, error) {
	var result pack.Result
	if len(items) == 0 {
		return result, nil
	}
	binVol := j.w * j.d * j.h

	bins := make([]*jbin, 0)
	for i, k := 0, j.learnK(items); i < k; i++ {
		bins = append(bins, j.newBin())
	}

	var lastErr error
	for _, item := range items {
		i3, ok := item.(*d3.Item3D)
		if !ok {
			result.Unplaced = append(result.Unplaced, item.ID())
			continue
		}
		orients := i3.Orientations()
		itemScalars := pack.ScalarsOf(item)
		itemVol := item.Volume()

		gather := func(into []scored, pool []*jbin) []scored {
			for _, b := range pool {
				if !j.binFeasible(b, itemScalars) {
					continue
				}
				for _, c := range b.ep.Candidates(orients) {
					into = append(into, scored{b, c, j.project(b, c, itemScalars, itemVol, binVol)})
				}
			}
			return into
		}

		cands := gather(nil, bins)
		if len(cands) == 0 {
			// Nothing fit an open bin — try one overflow bin.
			nb := j.newBin()
			if cands = gather(cands, []*jbin{nb}); len(cands) > 0 {
				bins = append(bins, nb)
			}
		}
		if len(cands) == 0 {
			result.Unplaced = append(result.Unplaced, item.ID())
			result.SetPlacementError(item.ID(), pack.ErrItemTooLarge)
			lastErr = pack.ErrItemTooLarge
			continue
		}

		win := cands[j.best(cands, itemScalars)]
		p := win.b.bin.PlaceCandidate(item, win.c)
		for k, v := range itemScalars {
			win.b.sums[k] += v
		}
		win.b.count++
		pack.ApplyConstraints(j.constraints, win.b.sums, itemScalars)
		result.Placements = append(result.Placements, p)
		if j.observer != nil {
			j.observer(p)
		}
	}

	for _, b := range bins { // prune the pre-opened bins that stayed empty
		if b.count > 0 {
			result.Bins = append(result.Bins, b.bin)
		}
	}
	return result, lastErr
}
