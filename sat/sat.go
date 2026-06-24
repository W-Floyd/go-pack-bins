// Package sat provides an exact 2-D rectangular bin packer built on a Boolean
// satisfiability encoding (gophersat). Unlike the heuristic packers elsewhere in
// the library, it can *certify* that a bin count is optimal: it squeezes the
// candidate count k down to the point where k bins are satisfiable and k-1 bins
// are unsatisfiable (or k equals the area lower bound, which proves k-1
// impossible arithmetically).
//
// The SAT encoding (order-encoded coordinates, conditional non-overlap,
// symmetry breaking) follows Soh, Inoue, Tamura, Banbara & Nabeshima (2010) and
// the 2-D cutting-stock adaptation of Kieu, Hoang & To (2026). This is the only
// package permitted to import gophersat; the rest of the library stays
// dependency-free.
//
// Order-encoding requires integer coordinates, so item and bin dimensions must
// scale losslessly to integers. When they do not, Pack2D returns an error rather
// than rounding — a rounded grid could make a feasible k look infeasible and
// produce a falsely "optimal" certificate.
package sat

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
	"github.com/crillab/gophersat/solver"
)

// ErrNonIntegral is returned when item/bin dimensions cannot be scaled to
// integers within MaxScale, so the SAT order-encoding cannot represent them.
var ErrNonIntegral = errors.New("sat: dimensions are not commensurable to an integer grid")

// ErrItemTooLarge is returned when some item cannot fit the bin in any allowed
// orientation, so no number of bins can pack the instance.
var ErrItemTooLarge = errors.New("sat: an item does not fit the bin in any orientation")

// ErrGridTooLarge is returned (with a best-effort heuristic packing) when the
// scaled grid would produce a formula above MaxGridCells.
var ErrGridTooLarge = errors.New("sat: scaled grid exceeds the size cap")

// Default limits. MaxScale caps the integer scaling search. The two size caps
// bound the formula built per candidate k, so a large grid degrades to the
// heuristic packing instead of exhausting memory:
//   - MaxGridCells caps the variable count ≈ n·(W+H).
//   - MaxClauses caps the clause count, which is dominated by the O(n²·(W+H))
//     pairwise "left/below" position-link clauses (doubled again under rotation).
//     This is the term that actually blows up memory, so it is the binding cap.
//
// Calibration (see TestMemSweep): gophersat's peak heap is ≈300 bytes per clause,
// roughly constant across instance sizes. So MaxClauses = 2M ≈ 0.6 GB peak — a
// safe default. Scale linearly to size a different budget (e.g. 4 GB ≈ 13M clauses).
const (
	MaxScale     = 100000
	MaxGridCells = 1_000_000
	MaxClauses   = 2_000_000
	// maxScaledDim caps a single scaled bin dimension, bounding the normal-position
	// subset-sum scratch arrays and DP before the formula estimate runs.
	maxScaledDim = 200_000
)

// Options configures Pack2D.
type Options struct {
	// AllowRotation permits 90° rotation of items whose Item2D.AllowRotate is set.
	AllowRotation bool
	// SymmetryBreak enables the large-item / infeasible-orientation rules
	// (default behaviour when constructed via Pack2D is on).
	SymmetryBreak bool
	// MaxGridCells overrides the variable-count cap (0 = use MaxGridCells const).
	MaxGridCells int
	// MaxClauses overrides the clause-count cap (0 = use MaxClauses const). This is
	// the binding limit on memory; lower it on memory-constrained hosts.
	MaxClauses int
	// NonIncremental forces the legacy strategy that rebuilds a fresh formula for
	// each candidate bin count (binary search). The default (false) uses incremental
	// solving: one formula, bins disabled one at a time, learned clauses reused.
	NonIncremental bool
}

// Result is a Pack2D outcome plus its optimality certificate.
type Result struct {
	pack.Result
	// Optimal is true iff BinsUsed() is *proven* minimal: either k-1 bins were
	// shown UNSAT, or the count equals the area lower bound.
	Optimal bool
	// LowerBound is the best proven lower bound on the bin count.
	LowerBound int
	// UpperBound is the best feasible bin count found.
	UpperBound int
	// Proof is a short human-readable justification of the certificate.
	Proof string
}

// Pack2D packs items into W×H bins, minimising the bin count, and certifies
// optimality when it can. The context bounds the search between candidate counts;
// on cancellation it returns the best feasible packing with Optimal=false.
func Pack2D(ctx context.Context, items []*d2.Item2D, W, H float64, opts Options) (Result, error) {
	if len(items) == 0 {
		return Result{Optimal: true, Proof: "no items"}, nil
	}

	// Scale floats → integers losslessly, or fail.
	vals := []float64{W, H}
	for _, it := range items {
		vals = append(vals, it.W, it.H)
	}
	scale, ok := detectScale(vals)
	if !ok {
		return Result{}, ErrNonIntegral
	}
	iW, iH := scaleInt(W, scale), scaleInt(H, scale)
	sitems := make([]scaledItem, len(items))
	for i, it := range items {
		sitems[i] = scaledItem{id: it.ID(), w: scaleInt(it.W, scale), h: scaleInt(it.H, scale), rotate: it.AllowRotate}
		// Reject items that fit in no orientation.
		natFits := sitems[i].w <= iW && sitems[i].h <= iH
		rotFits := opts.AllowRotation && it.AllowRotate && sitems[i].h <= iW && sitems[i].w <= iH
		if !natFits && !rotFits {
			return Result{}, fmt.Errorf("%w: item %q", ErrItemTooLarge, it.ID())
		}
	}

	// Bounds: area lower bound, FFD upper bound.
	lb := areaLowerBound(items, W, H)
	ub, ubPlacements := ffdUpperBound(ctx, items, W, H)
	if ub < lb {
		ub = lb // numerical guard; FFD is always ≥ lb in exact arithmetic
	}

	// Pre-DP guard: cap raw dimensions so the normal-position computation (O(n·(W+H))
	// with W+1/H+1 bool scratch arrays) stays bounded even before the size estimate.
	if iW > maxScaledDim || iH > maxScaledDim {
		r := Result{Result: ubPlacements, LowerBound: lb, UpperBound: ub,
			Proof: "bin dimensions too large to encode; heuristic only"}
		return r, ErrGridTooLarge
	}

	// Normal-pattern position sets: coordinates need only range over reachable
	// subset sums of item widths/heights, which shrinks the formula vs the full grid.
	posX := normalPositions(sitems, iW, opts.AllowRotation)
	posY := normalPositions(sitems, iH, opts.AllowRotation)

	// Size guard: estimate the formula built per candidate k and bail out to the
	// heuristic packing before allocating, so a large instance degrades gracefully
	// instead of exhausting memory. The clause estimate is the binding term.
	gridCap, clauseCap := opts.MaxGridCells, opts.MaxClauses
	if gridCap == 0 {
		gridCap = MaxGridCells
	}
	if clauseCap == 0 {
		clauseCap = MaxClauses
	}
	estVars, estClauses := estimateFormula(len(items), len(posX), len(posY), ub, opts.AllowRotation)
	if estVars > int64(gridCap) || estClauses > int64(clauseCap) {
		r := Result{Result: ubPlacements, LowerBound: lb, UpperBound: ub,
			Proof: fmt.Sprintf("formula too large (~%d vars, ~%d clauses); heuristic only", estVars, estClauses)}
		return r, ErrGridTooLarge
	}

	// Search for the minimum feasible bin count. The incremental strategy builds the
	// formula once and disables bins one at a time, reusing learned clauses across
	// solves; the non-incremental one rebuilds a fresh formula per candidate k.
	var best []placement
	var bestK int
	var completed bool
	if opts.NonIncremental {
		best, bestK, completed = solveBinarySearch(ctx, iW, iH, sitems, lb, ub, opts, posX, posY)
	} else {
		best, bestK, completed = solveIncremental(ctx, iW, iH, sitems, lb, ub, opts, posX, posY)
	}

	if best == nil {
		// Fall back to the heuristic packing (e.g. cancelled before any solve).
		return Result{Result: ubPlacements, LowerBound: lb, UpperBound: ub, Optimal: false, Proof: "search incomplete; heuristic only"}, ctx.Err()
	}

	r := decodeResult(items, best, W, H, scale)
	r.LowerBound, r.UpperBound = lb, bestK
	if completed {
		r.Optimal = true
		if bestK == lb {
			r.Proof = fmt.Sprintf("meets area lower bound (%d bins)", lb)
		} else {
			r.Proof = fmt.Sprintf("%d bins SAT, %d bins UNSAT", bestK, bestK-1)
		}
	} else {
		r.Proof = "search incomplete (cancelled); best feasible returned"
	}
	return r, ctx.Err()
}

// solveIncremental finds the minimum feasible bin count by incremental SAT: it
// builds the formula once at the upper bound, then walks the bin count down,
// disabling one more bin per step by appending a unit clause ¬aVar[m] and
// re-solving. gophersat retains learned conflict clauses across these solves, so
// each step reuses the work of the previous one. Returns (placements, k, completed);
// completed is false only if ctx was cancelled mid-search (then k is the best found
// so far and the caller treats it as uncertified).
func solveIncremental(ctx context.Context, iW, iH int, items []scaledItem, lb, ub int, opts Options, posX, posY []int) ([]placement, int, bool) {
	e := newEnc(iW, iH, items, ub, opts.AllowRotation, opts.SymmetryBreak, posX, posY)
	s := solver.New(e.problem())
	if s.Solve() != solver.Sat {
		return nil, -1, true // upper bound infeasible (shouldn't happen — FFD achieved it)
	}
	best, bestK := e.decode(s.Model()), ub

	// Walk down: at step m we disable bin m (bins m+1..ub-1 already disabled), so the
	// active bins are 0..m-1, i.e. exactly m bins. The first UNSAT proves m infeasible,
	// certifying the previous (m+1) as optimal.
	for m := ub - 1; m >= lb; m-- {
		if ctx.Err() != nil {
			return best, bestK, false
		}
		s.AppendClause(solver.NewClause(solver.IntsToLits(int32(-e.aVar[m]))))
		if s.Solve() == solver.Sat {
			best, bestK = e.decode(s.Model()), m
		} else {
			break // m bins infeasible → bestK (= m+1) is optimal
		}
	}
	return best, bestK, true
}

// solveBinarySearch is the non-incremental strategy: binary search over the bin
// count, rebuilding a fresh formula for each probe. Slower on large formulas (it
// re-encodes repeatedly) but kept as a reference/escape hatch.
func solveBinarySearch(ctx context.Context, iW, iH int, items []scaledItem, lb, ub int, opts Options, posX, posY []int) ([]placement, int, bool) {
	var best []placement
	bestK := -1
	lo, hi := lb, ub
	completed := true
	for lo < hi {
		if ctx.Err() != nil {
			completed = false
			break
		}
		mid := (lo + hi) / 2
		e := newEnc(iW, iH, items, mid, opts.AllowRotation, opts.SymmetryBreak, posX, posY)
		if pl, sat := e.solve(); sat {
			best, bestK = pl, mid
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	// At convergence lo==hi is the optimum; ensure we have its placements.
	if completed && bestK != lo {
		if ctx.Err() != nil {
			completed = false
		} else {
			e := newEnc(iW, iH, items, lo, opts.AllowRotation, opts.SymmetryBreak, posX, posY)
			if pl, sat := e.solve(); sat {
				best, bestK = pl, lo
			}
		}
	}
	return best, bestK, completed
}

// detectScale finds the smallest integer s in [1, MaxScale] such that s·v is
// (within tolerance) an integer for every v.
func detectScale(vals []float64) (int, bool) {
	const eps = 1e-6
	for s := 1; s <= MaxScale; s++ {
		ok := true
		for _, v := range vals {
			sv := v * float64(s)
			if math.Abs(sv-math.Round(sv)) > eps {
				ok = false
				break
			}
		}
		if ok {
			return s, true
		}
	}
	return 0, false
}

func scaleInt(v float64, s int) int { return int(math.Round(v * float64(s))) }

// estimateFormula returns rough upper bounds on the variable and clause counts of
// the SAT encoding for n items whose coordinates range over pxN × pyN normal-pattern
// positions, with up to ub bins. Memory is dominated by the O(n²·(pxN+pyN)) pairwise
// position-link clauses (×2 under rotation), so this is what the size guard checks
// before building the formula. All terms are int64 to avoid overflow.
func estimateFormula(n, pxN, pyN, ub int, rotation bool) (vars, clauses int64) {
	pp := int64(pxN + pyN)
	N := int64(n)
	pairs := N * (N - 1) / 2
	rotF := int64(1)
	if rotation {
		rotF = 2
	}
	// vars: position (px/py) + assignment + bin-usage + 4 relation vars per pair.
	vars = N*pp + N*int64(ub) + int64(ub) + pairs*4
	// clauses: order axioms (≈n·(pxN+pyN)) + links (2 left + 2 below per pair, each
	// ≈pxN+pyN, ×rotF) + per-bin non-overlap (≤ub per pair) + usage links (≤n·ub).
	clauses = N*pp + pairs*2*pp*rotF + pairs*int64(ub) + N*int64(ub)
	return vars, clauses
}

func areaLowerBound(items []*d2.Item2D, W, H float64) int {
	var total float64
	for _, it := range items {
		total += it.W * it.H
	}
	return int(math.Ceil(total/(W*H) - 1e-9))
}

// ffdUpperBound packs with First-Fit-Decreasing (MaxRects) to get a feasible
// bin count and a fallback packing.
func ffdUpperBound(ctx context.Context, items []*d2.Item2D, W, H float64) (int, pack.Result) {
	factory := d2.NewFactory(W, H, d2.NewMaxRectsDefault)
	packItems := make([]pack.Item, len(items))
	for i, it := range items {
		packItems[i] = it
	}
	res, _ := offline.FirstFitDecreasing(factory).PackAllCtx(ctx, packItems)
	return res.BinsUsed(), res
}

// decodeResult turns scaled-integer placements into a pack.Result with d2
// placements at their real (unscaled) coordinates.
func decodeResult(items []*d2.Item2D, pls []placement, W, H float64, scale int) Result {
	used := map[int]bool{}
	for _, p := range pls {
		used[p.bin] = true
	}
	// Stable bin ids by index.
	binID := func(j int) string { return fmt.Sprintf("satbin-%d", j) }
	bins := make([]pack.Bin, 0, len(used))
	maxBin := 0
	for j := range used {
		if j > maxBin {
			maxBin = j
		}
	}
	for j := 0; j <= maxBin; j++ {
		if used[j] {
			bins = append(bins, d2.NewBin(binID(j), W, H, d2.NewMaxRectsDefault(W, H)))
		}
	}

	placements := make([]pack.Placement, len(items))
	inv := 1.0 / float64(scale)
	for _, p := range pls {
		placements[p.item] = d2.NewPlacement(binID(p.bin), items[p.item].ID(),
			float64(p.x)*inv, float64(p.y)*inv, float64(p.w)*inv, float64(p.h)*inv, p.rotated)
	}
	return Result{Result: pack.Result{Bins: bins, Placements: placements}}
}
