package d3

import (
	"context"
	"math"
	"sort"
)

// RefineVoids is a void-guided post-pass that tightens an existing 3-D packing in
// place. Per bin it repeatedly lifts the removable "top-layer" items (those with
// nothing resting on them) and re-drops them into the lowest free space with the
// EMS placer — EMS's lowest-feasible space is exactly the deepest void an item
// fits, so re-dropping pulls the top down into voids. When that alone makes no
// progress it *widens*: it speculatively lifts an item adjoining a sealed void
// (together with the sub-stack resting on it, so nothing is left floating),
// merging the item's space into the void, and re-drops — opening sealed pockets.
//
// Every candidate move is kept only if it *strictly* lowers the bin's objective
// J = (peak height, ΣZ) lexicographically; otherwise it is rolled back. Because
// ΣZ is bounded below and strictly decreases on each accepted move, the pass
// terminates (no cycling). Re-drops go through EMS with NoFloating, so the result
// never overlaps and never floats regardless of how the base packer placed
// things. Items re-orient only within orients[id] (which encodes each item's
// rotation permission); an item absent from the map keeps its placed dimensions.
//
// It is a bounded local search, not a solver: opts caps the rounds, the bin size
// it will touch, and the widening fan; ctx cancels it between bins and rounds.
// Returns whether any item moved.
func RefineVoids(ctx context.Context, ps []*Placement3D, orients map[string][][3]float64, w, d, h float64, spec ContactSpec, opts RefineOptions) bool {
	opts = opts.withDefaults()

	byBin := map[string][]*Placement3D{}
	var binOrder []string
	for _, p := range ps {
		if _, seen := byBin[p.binID]; !seen {
			binOrder = append(binOrder, p.binID)
		}
		byBin[p.binID] = append(byBin[p.binID], p)
	}

	moved := false
	for _, id := range binOrder {
		if err := ctx.Err(); err != nil {
			return moved
		}
		bin := byBin[id]
		if len(bin) > opts.MaxBinItems {
			continue // cost cap: re-dropping a huge bin is too expensive for a post-pass
		}
		if refineBin(ctx, bin, orients, w, d, h, spec, opts) {
			moved = true
		}
	}
	return moved
}

// RefineOptions bounds the refiner's cost. Zero fields take sensible defaults.
type RefineOptions struct {
	MaxRounds   int // passes over each bin (default 4)
	MaxBinItems int // skip bins with more items than this (default 2000)
	WidenVoids  int // sealed voids attempted per widening pass (default 8)
	WidenAdj    int // adjoining items tried per void (default 4)
	MaxSubtree  int // skip widening an item whose support sub-stack exceeds this (default 8)
}

func (o RefineOptions) withDefaults() RefineOptions {
	if o.MaxRounds <= 0 {
		o.MaxRounds = 4
	}
	if o.MaxBinItems <= 0 {
		// Cost cap: per-item re-drop is ~O(k·local) with the extreme-point grid, so
		// a few-thousand-item bin refines in ~a second, but a large bin with much
		// slack (many accepted moves over several rounds) can still take minutes.
		// 2000 keeps the worst case bounded; raise it (and pass a ctx deadline) to
		// refine bigger bins.
		o.MaxBinItems = 2000
	}
	if o.WidenVoids <= 0 {
		o.WidenVoids = 8
	}
	if o.WidenAdj <= 0 {
		o.WidenAdj = 4
	}
	if o.MaxSubtree <= 0 {
		o.MaxSubtree = 8
	}
	return o
}

func refineBin(ctx context.Context, bin []*Placement3D, orients map[string][][3]float64, w, d, h float64, spec ContactSpec, opts RefineOptions) bool {
	rs := ContactSpec{Bottom: spec.Bottom, NoFloating: true} // refiner never floats
	moved := false

	// Gravity pre-pass: cheaply drop every item straight down onto whatever is
	// beneath it (Settle, O(k²), no lateral search). This both tightens the bin
	// for free and settles things the per-item refiner cannot — it only relocates
	// removable leaves, whereas Settle lowers a whole floating sub-stack. Skipped
	// when a bottom-support fraction is required, since a straight drop can land a
	// box on a partial support and violate that gate (the EP re-drop honours it).
	if spec.Bottom <= compactEps {
		if gravitySettle(bin) {
			moved = true
		}
	}

	for round := 0; round < opts.MaxRounds; round++ {
		if err := ctx.Err(); err != nil {
			return moved
		}
		// Direct fill: lower each removable item, highest first, into the lowest
		// free space it fits — pulling the top down into voids one item at a time
		// (a per-item move finds improvements a whole-layer re-drop misses).
		rem := removable(bin)
		sort.SliceStable(rem, func(a, b int) bool {
			ta, tb := bin[rem[a]].Z, bin[rem[b]].Z
			if math.Abs(ta-tb) > compactEps {
				return ta > tb
			}
			return bin[rem[a]].itemID < bin[rem[b]].itemID
		})
		improved := false
		for _, i := range rem {
			if err := ctx.Err(); err != nil {
				return moved
			}
			if tryLower(bin, i, orients, w, d, h, rs) {
				improved = true
			}
		}
		// Widening: if direct fill stalled, open a sealed void by lifting an
		// adjoining item's sub-stack and re-dropping.
		if !improved {
			improved = widen(ctx, bin, orients, w, d, h, rs, opts)
		}
		if !improved {
			break
		}
		moved = true
	}
	return moved
}

// tryLower re-drops item i alone into the lowest free space among the others and
// keeps the move only if it strictly improves the bin's (peak, ΣZ) objective.
// Lowering a removable item is always overlap-free and grounded (EMS with
// NoFloating); the J check guards against a re-orientation that would raise the
// peak. Returns whether it moved the item.
func tryLower(bin []*Placement3D, i int, orients map[string][][3]float64, w, d, h float64, rs ContactSpec) bool {
	// Extreme-point placement (grid-accelerated) finds the lowest feasible spot in
	// ~O(k·local), vs the maximal-space prune's O(k²) — this is what lets the
	// refiner scale to large bins. It commits the other boxes, then drops item i.
	ep := NewExtremePoint(w, d, h)
	ep.contact = rs
	var otherPeak, otherSumZ float64
	for j, p := range bin {
		if j == i {
			continue
		}
		ep.CommitCandidate(Candidate{X: p.X, Y: p.Y, Z: p.Z, W: p.W, D: p.D, H: p.H})
		if t := p.Z + p.H; t > otherPeak {
			otherPeak = t
		}
		otherSumZ += p.Z
	}
	x, y, z, pw, pd, ph, ok := ep.TryInsert(orientsOf(orients, bin[i]))
	if !ok {
		return false
	}
	old := bin[i]
	oldJ := [2]float64{math.Max(otherPeak, old.Z+old.H), otherSumZ + old.Z}
	newJ := [2]float64{math.Max(otherPeak, z+ph), otherSumZ + z}
	if !jLess(newJ, oldJ) {
		return false
	}
	bin[i].X, bin[i].Y, bin[i].Z = x, y, z
	bin[i].W, bin[i].D, bin[i].H = pw, pd, ph
	return true
}

// gravitySettle drops every item in the bin straight down onto the floor or the
// highest top beneath its footprint (the existing Settle), reporting whether any
// item actually moved. It is the cheap vertical pre-pass: O(k²), no lateral
// search, and it lowers whole sub-stacks the leaf-only refiner can't touch.
func gravitySettle(bin []*Placement3D) bool {
	old := make([]float64, len(bin))
	for i, p := range bin {
		old[i] = p.Z
	}
	Settle(bin)
	for i, p := range bin {
		if math.Abs(p.Z-old[i]) > compactEps {
			return true
		}
	}
	return false
}

// removable returns the indices of bin items with nothing resting on them (the
// leaves of the support graph) — the items that can be moved without dropping
// any other.
func removable(bin []*Placement3D) []int {
	var out []int
	for i, a := range bin {
		leaf := true
		for j, b := range bin {
			if i == j {
				continue
			}
			if restsOn(b, a) { // b sits on a's top face
				leaf = false
				break
			}
		}
		if leaf {
			out = append(out, i)
		}
	}
	return out
}

// restsOn reports whether b's bottom face lies on a's top face with overlapping
// footprint (b is supported by a).
func restsOn(b, a *Placement3D) bool {
	return math.Abs(b.Z-(a.Z+a.H)) <= compactEps &&
		overlap1D(a.X, a.X+a.W, b.X, b.X+b.W) > compactEps &&
		overlap1D(a.Y, a.Y+a.D, b.Y, b.Y+b.D) > compactEps
}

// liftAndRedrop removes the lift[] items from the bin, rebuilds the free space
// from the rest with EMS, and re-inserts the lifted items (largest first) at
// their lowest feasible positions. It applies the result only if the bin's
// (peak, ΣZ) objective strictly improves; otherwise the bin is left untouched.
// Returns whether it applied a change.
func liftAndRedrop(bin []*Placement3D, lift []int, orients map[string][][3]float64, w, d, h float64, rs ContactSpec) bool {
	if len(lift) == 0 {
		return false
	}
	lifted := make([]bool, len(bin))
	for _, i := range lift {
		lifted[i] = true
	}

	// Free space from the items that stay put (extreme-point, grid-accelerated).
	ep := NewExtremePoint(w, d, h)
	ep.contact = rs
	for i, p := range bin {
		if !lifted[i] {
			ep.CommitCandidate(Candidate{X: p.X, Y: p.Y, Z: p.Z, W: p.W, D: p.D, H: p.H})
		}
	}

	// Re-insert lifted items largest-volume-first (ties by id) for a dense,
	// deterministic re-drop.
	order := append([]int(nil), lift...)
	sort.SliceStable(order, func(a, b int) bool {
		pa, pb := bin[order[a]], bin[order[b]]
		va, vb := pa.W*pa.D*pa.H, pb.W*pb.D*pb.H
		if math.Abs(va-vb) > compactEps {
			return va > vb
		}
		return pa.itemID < pb.itemID
	})

	type pos struct{ x, y, z, w, d, h float64 }
	newPos := make(map[int]pos, len(order))
	for _, i := range order {
		x, y, z, pw, pd, ph, ok := ep.TryInsert(orientsOf(orients, bin[i]))
		if !ok {
			return false // a lifted item can't be re-placed — abort, bin unchanged
		}
		ep.CommitCandidate(Candidate{X: x, Y: y, Z: z, W: pw, D: pd, H: ph})
		newPos[i] = pos{x, y, z, pw, pd, ph}
	}

	// Hypothetical objective: kept items unchanged, lifted items at newPos.
	var before, after [2]float64
	for i, p := range bin {
		z, top := p.Z, p.Z+p.H
		if top > before[0] {
			before[0] = top
		}
		before[1] += z
		if np, ok := newPos[i]; ok {
			z, top = np.z, np.z+np.h
		}
		if top > after[0] {
			after[0] = top
		}
		after[1] += z
	}
	if !jLess(after, before) {
		return false
	}

	for i, np := range newPos {
		bin[i].X, bin[i].Y, bin[i].Z = np.x, np.y, np.z
		bin[i].W, bin[i].D, bin[i].H = np.w, np.d, np.h
	}
	return true
}

// widen attempts to open a sealed void: for the largest sealed voids, it lifts an
// adjoining item together with the sub-stack resting on it (so nothing floats)
// plus the current top layer, and re-drops via liftAndRedrop (which keeps the
// move only if J improves). Returns whether a widening move was applied.
func widen(ctx context.Context, bin []*Placement3D, orients map[string][][3]float64, w, d, h float64, rs ContactSpec, opts RefineOptions) bool {
	boxes := make([]PlacedBox, len(bin))
	for i, p := range bin {
		boxes[i] = PlacedBox{X: p.X, Y: p.Y, Z: p.Z, W: p.W, D: p.D, H: p.H}
	}
	voids := InternalVoids(w, d, h, boxes)
	if len(voids) == 0 {
		return false
	}
	sort.SliceStable(voids, func(a, b int) bool {
		return voids[a].W*voids[a].D*voids[a].H > voids[b].W*voids[b].D*voids[b].H
	})

	top := removable(bin)
	for vi := 0; vi < len(voids) && vi < opts.WidenVoids; vi++ {
		if err := ctx.Err(); err != nil {
			return false
		}
		adj := adjoining(bin, voids[vi])
		sort.SliceStable(adj, func(a, b int) bool {
			pa, pb := bin[adj[a]], bin[adj[b]]
			return pa.W*pa.D*pa.H > pb.W*pb.D*pb.H
		})
		for ai := 0; ai < len(adj) && ai < opts.WidenAdj; ai++ {
			st := subtree(bin, adj[ai])
			if len(st) > opts.MaxSubtree {
				continue // lifting this would ruin too much
			}
			if liftAndRedrop(bin, union(top, st), orients, w, d, h, rs) {
				return true
			}
		}
	}
	return false
}

// adjoining returns the indices of bin items whose box touches a face of void V.
func adjoining(bin []*Placement3D, v VoidBox) []int {
	var out []int
	for i, p := range bin {
		if faceTouches(p, v) {
			out = append(out, i)
		}
	}
	return out
}

// faceTouches reports whether placement p shares a face with void v (a coincident
// bounding plane on one axis with positive overlap on the other two).
func faceTouches(p *Placement3D, v VoidBox) bool {
	near := func(a, b float64) bool { return math.Abs(a-b) <= compactEps }
	ox := overlap1D(p.X, p.X+p.W, v.X, v.X+v.W) > compactEps
	oy := overlap1D(p.Y, p.Y+p.D, v.Y, v.Y+v.D) > compactEps
	oz := overlap1D(p.Z, p.Z+p.H, v.Z, v.Z+v.H) > compactEps
	if oy && oz && (near(p.X+p.W, v.X) || near(v.X+v.W, p.X)) {
		return true
	}
	if ox && oz && (near(p.Y+p.D, v.Y) || near(v.Y+v.D, p.Y)) {
		return true
	}
	if ox && oy && (near(p.Z+p.H, v.Z) || near(v.Z+v.H, p.Z)) {
		return true
	}
	return false
}

// subtree returns aIdx plus every item transitively resting on it, so lifting the
// set leaves nothing floating.
func subtree(bin []*Placement3D, aIdx int) []int {
	inSet := map[int]bool{aIdx: true}
	queue := []int{aIdx}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		a := bin[cur]
		for j, b := range bin {
			if inSet[j] {
				continue
			}
			if restsOn(b, a) {
				inSet[j] = true
				queue = append(queue, j)
			}
		}
	}
	out := make([]int, 0, len(inSet))
	for i := range inSet {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// union returns the sorted, de-duplicated union of two index sets.
func union(a, b []int) []int {
	seen := make(map[int]bool, len(a)+len(b))
	for _, i := range a {
		seen[i] = true
	}
	for _, i := range b {
		seen[i] = true
	}
	out := make([]int, 0, len(seen))
	for i := range seen {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// jLess reports whether objective a is strictly better (smaller) than b under the
// lexicographic (peak height, then ΣZ) order.
func jLess(a, b [2]float64) bool {
	if math.Abs(a[0]-b[0]) > compactEps {
		return a[0] < b[0]
	}
	return a[1] < b[1]-compactEps
}

// orientsOf returns the item's allowed orientations for re-insertion, falling back
// to its current placed dimensions when the item is not in the map.
func orientsOf(orients map[string][][3]float64, p *Placement3D) [][3]float64 {
	if os, ok := orients[p.itemID]; ok && len(os) > 0 {
		return os
	}
	return [][3]float64{{p.W, p.D, p.H}}
}
