package sat

import (
	"sort"

	"github.com/crillab/gophersat/solver"
)

// scaledItem is an item with integer (scaled) dimensions.
type scaledItem struct {
	id     string
	w, h   int
	rotate bool // this item may be rotated 90°
}

// normalPositions returns the candidate edge positions on an axis of the given
// length: 0 and every subset sum of the items' widths that is ≤ limit (using each
// item's height too when rotation is allowed). This is the classic "normal
// patterns" reduction — in any packing every item can be pushed toward the origin
// until its edge sits at such a position, so restricting coordinates to this set
// loses no feasible packing while shrinking the grid from `limit` points to the
// (often far fewer) reachable sums. The result is sorted and includes 0.
func normalPositions(items []scaledItem, limit int, rotate bool) []int {
	reach := make([]bool, limit+1)
	reach[0] = true
	for _, it := range items {
		// 0/1 subset sum: process high→low so each item contributes at most once.
		for p := limit; p >= 0; p-- {
			if !reach[p] {
				continue
			}
			if w := p + it.w; w <= limit {
				reach[w] = true
			}
			if rotate && it.rotate {
				if h := p + it.h; h <= limit {
					reach[h] = true
				}
			}
		}
	}
	out := make([]int, 0, 16)
	for p := 0; p <= limit; p++ {
		if reach[p] {
			out = append(out, p)
		}
	}
	return out
}

// enc builds the SAT formula for "do these items fit in k bins of W×H?".
//
// Encoding (order-encoding of coordinates, after Soh et al. 2010 and the 2-D-CSSP
// adaptation in Kieu et al. 2026):
//   - sVar[c][j]      : item c is assigned to bin j (exactly-one per item).
//   - pxVar[c][i]     : x_c ≤ posX[i]; x_c ≤ posX[last] is always true.
//   - pyVar[c][i]     : y_c ≤ posY[i].
//   - lr[{a,b}]       : item a is left of b (x_a + w_a ≤ x_b).
//   - ud[{a,b}]       : item a is below b (y_a + h_a ≤ y_b).
//   - rVar[c]         : item c is rotated (only when rotation is enabled).
//
// Coordinates are restricted to the normal-pattern position sets posX/posY (see
// normalPositions), not the full integer grid, so the formula scales with the
// number of reachable positions rather than W·H.
//
// Symmetry: item c may only occupy bins 0..min(c,k-1) (bins are interchangeable,
// so WLOG bin j's lowest-indexed item is ≥ j). This both fixes item 0 → bin 0 and
// shrinks the assignment space. SB1 (large-item) fixes impossible left/below
// relations false; SB2 (same-size ordering) forbids a duplicate item from sitting
// strictly left of an earlier identical one; SB3 (infeasible-orientation) fixes
// rotation when only one orientation fits.
type enc struct {
	W, H       int
	items      []scaledItem
	k          int
	rotate     bool
	sym        bool
	posX, posY []int // normal-pattern positions per axis (sorted, include 0)

	nVars int
	cards []solver.CardConstr
	vTrue int

	sVar  [][]int
	pxVar [][]int
	pyVar [][]int
	rVar  []int
	aVar  []int // aVar[j]: bin j is used (for incremental bin-disabling)
	lr    map[[2]int]int
	ud    map[[2]int]int
}

func newEnc(W, H int, items []scaledItem, k int, rotate, sym bool, posX, posY []int) *enc {
	e := &enc{W: W, H: H, items: items, k: k, rotate: rotate, sym: sym, posX: posX, posY: posY, lr: map[[2]int]int{}, ud: map[[2]int]int{}}
	e.vTrue = e.newVar()
	e.add(e.vTrue) // force the TRUE constant true
	e.build()
	return e
}

func (e *enc) newVar() int { e.nVars++; return e.nVars }

// add records a clause, simplifying against the TRUE/FALSE constants:
// a clause containing TRUE (vTrue) is a tautology and dropped; FALSE (-vTrue)
// literals are removed; an emptied clause forces the formula UNSAT.
func (e *enc) add(lits ...int) {
	out := make([]int, 0, len(lits))
	for _, l := range lits {
		if l == e.vTrue {
			return // tautology
		}
		if l == -e.vTrue {
			continue // false literal
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		e.cards = append(e.cards, solver.AtLeast1(-e.vTrue)) // unsatisfiable
		return
	}
	e.cards = append(e.cards, solver.AtLeast1(out...))
}

// maxBin returns the highest bin index item c may occupy under the symmetry rule.
func (e *enc) maxBin(c int) int {
	m := c
	if m > e.k-1 {
		m = e.k - 1
	}
	return m
}

// posLE returns the order literal for "coord ≤ t" given the position set pos and
// per-item order vars v: FALSE if t is below the first position, TRUE if t reaches
// the last position (the coordinate can never exceed it). Otherwise it finds the
// largest position ≤ t and returns its order var — since the coordinate only takes
// values in pos, "coord ≤ t" ⟺ "coord ≤ that position".
func (e *enc) posLE(pos, v []int, t int) int {
	if t < pos[0] {
		return -e.vTrue
	}
	// idx = largest index with pos[idx] ≤ t.
	idx := sort.SearchInts(pos, t+1) - 1
	if idx >= len(pos)-1 {
		return e.vTrue
	}
	return v[idx]
}

func (e *enc) pxLE(c, t int) int { return e.posLE(e.posX, e.pxVar[c], t) }
func (e *enc) pyLE(c, t int) int { return e.posLE(e.posY, e.pyVar[c], t) }

// minW/minH give the smallest footprint dimension achievable for an item under
// the rotation policy (used for the SB1 large-item check).
func (e *enc) minW(it scaledItem) int {
	if e.rotate && it.rotate && it.h < it.w {
		return it.h
	}
	return it.w
}

func (e *enc) minH(it scaledItem) int {
	if e.rotate && it.rotate && it.w < it.h {
		return it.w
	}
	return it.h
}

// identical reports whether items a and b are interchangeable: same footprint and
// same rotation policy. Used by SB2 to break the duplicate-permutation symmetry.
func (e *enc) identical(a, b int) bool {
	x, y := e.items[a], e.items[b]
	return x.w == y.w && x.h == y.h && x.rotate == y.rotate
}

func (e *enc) build() {
	n := len(e.items)
	e.sVar = make([][]int, n)
	e.pxVar = make([][]int, n)
	e.pyVar = make([][]int, n)
	e.rVar = make([]int, n)

	// Allocate position vars over the normal-pattern sets + order-encoding axioms.
	// pxVar[c] has one var per position except the last (x ≤ last is always true).
	nx, ny := len(e.posX)-1, len(e.posY)-1
	for c := range e.items {
		if nx > 0 {
			e.pxVar[c] = make([]int, nx)
			for i := range e.pxVar[c] {
				e.pxVar[c][i] = e.newVar()
			}
			for i := 0; i < nx-1; i++ {
				e.add(-e.pxVar[c][i], e.pxVar[c][i+1]) // x≤posX[i] ⇒ x≤posX[i+1]
			}
		}
		if ny > 0 {
			e.pyVar[c] = make([]int, ny)
			for i := range e.pyVar[c] {
				e.pyVar[c][i] = e.newVar()
			}
			for i := 0; i < ny-1; i++ {
				e.add(-e.pyVar[c][i], e.pyVar[c][i+1])
			}
		}
		if e.rotate && e.items[c].rotate {
			e.rVar[c] = e.newVar()
		}
	}

	// Assignment vars + exactly-one per item (bins 0..maxBin).
	for c := range e.items {
		lits := make([]int, 0, e.maxBin(c)+1)
		e.sVar[c] = make([]int, e.k)
		for j := 0; j <= e.maxBin(c); j++ {
			v := e.newVar()
			e.sVar[c][j] = v
			lits = append(lits, v)
		}
		e.cards = append(e.cards, solver.Exactly1(lits...)...)
	}

	// Bin-usage vars: aVar[j] ⇐ any item on bin j. The incremental solver disables a
	// bin by asserting ¬aVar[j], which (via these links) forbids any item there.
	e.aVar = make([]int, e.k)
	for j := range e.aVar {
		e.aVar[j] = e.newVar()
	}
	for c := range e.items {
		for j := 0; j <= e.maxBin(c); j++ {
			e.add(-e.sVar[c][j], e.aVar[j])
		}
	}

	for c := range e.items {
		e.domainFit(c)
	}

	// Relation vars, links, SB1, and per-bin non-overlap.
	for a := 0; a < n; a++ {
		for b := a + 1; b < n; b++ {
			lab := e.relVar(e.lr, a, b)
			lba := e.relVar(e.lr, b, a)
			uab := e.relVar(e.ud, a, b)
			uba := e.relVar(e.ud, b, a)

			e.leftLink(a, b, lab)
			e.leftLink(b, a, lba)
			e.belowLink(a, b, uab)
			e.belowLink(b, a, uba)

			if e.sym {
				if e.minW(e.items[a])+e.minW(e.items[b]) > e.W {
					e.add(-lab)
					e.add(-lba)
				}
				if e.minH(e.items[a])+e.minH(e.items[b]) > e.H {
					e.add(-uab)
					e.add(-uba)
				}
				// SB2 (same-size ordering): identical items are interchangeable, so the
				// later one (b) may not sit strictly left of the earlier one (a) — WLOG
				// it goes to a's right or is separated vertically. This prunes the d!
				// permutation symmetry among duplicates without losing any packing.
				if e.identical(a, b) {
					e.add(-lba)
				}
			}

			// Non-overlap: on any shared bin, a and b must be separated.
			maxShared := e.maxBin(a) // a<b ⇒ maxBin(a) ≤ maxBin(b)
			for j := 0; j <= maxShared; j++ {
				e.add(-e.sVar[a][j], -e.sVar[b][j], lab, lba, uab, uba)
			}
		}
	}
}

func (e *enc) relVar(m map[[2]int]int, a, b int) int {
	key := [2]int{a, b}
	if v, ok := m[key]; ok {
		return v
	}
	v := e.newVar()
	m[key] = v
	return v
}

// domainFit forces each item to lie within the bin in its chosen orientation.
func (e *enc) domainFit(c int) {
	it := e.items[c]
	if !e.rotate || !it.rotate {
		e.add(e.pxLE(c, e.W-it.w))
		e.add(e.pyLE(c, e.H-it.h))
		return
	}
	r := e.rVar[c]
	natFits := it.w <= e.W && it.h <= e.H
	rotFits := it.h <= e.W && it.w <= e.H
	// SB3: if only one orientation fits at all, fix rotation.
	if natFits && !rotFits {
		e.add(-r)
	} else if rotFits && !natFits {
		e.add(r)
	}
	// not rotated ⇒ footprint w×h.
	e.add(r, e.pxLE(c, e.W-it.w))
	e.add(r, e.pyLE(c, e.H-it.h))
	// rotated ⇒ footprint h×w.
	e.add(-r, e.pxLE(c, e.W-it.h))
	e.add(-r, e.pyLE(c, e.H-it.w))
}

// leftLink encodes lab → x_a + w_a ≤ x_b (a is left of b). It suffices to enforce
// the implication at each candidate position of x_b (≥ x_b's value is the binding
// case), so we iterate over posX rather than the full grid.
func (e *enc) leftLink(a, b, lab int) {
	it := e.items[a]
	rot := e.rotate && it.rotate
	for _, p := range e.posX {
		if !rot {
			e.add(-lab, -e.pxLE(b, p), e.pxLE(a, p-it.w))
		} else {
			e.add(e.rVar[a], -lab, -e.pxLE(b, p), e.pxLE(a, p-it.w))  // not rotated: width w
			e.add(-e.rVar[a], -lab, -e.pxLE(b, p), e.pxLE(a, p-it.h)) // rotated: width h
		}
	}
}

// belowLink encodes uab → y_a + h_a ≤ y_b (a is below b).
func (e *enc) belowLink(a, b, uab int) {
	it := e.items[a]
	rot := e.rotate && it.rotate
	for _, p := range e.posY {
		if !rot {
			e.add(-uab, -e.pyLE(b, p), e.pyLE(a, p-it.h))
		} else {
			e.add(e.rVar[a], -uab, -e.pyLE(b, p), e.pyLE(a, p-it.h))  // not rotated: height h
			e.add(-e.rVar[a], -uab, -e.pyLE(b, p), e.pyLE(a, p-it.w)) // rotated: height w
		}
	}
}

// placement is one decoded item position (scaled-integer coordinates).
type placement struct {
	item    int
	bin     int
	x, y    int
	w, h    int // footprint as placed (swapped if rotated)
	rotated bool
}

// problem builds the gophersat problem from the accumulated cardinality clauses.
func (e *enc) problem() *solver.Problem { return solver.ParseCardConstrs(e.cards) }

// solve runs a one-shot SAT solve on the built formula. Returns (placements, true)
// if satisfiable, (nil, false) if UNSAT. Used by the non-incremental search.
func (e *enc) solve() ([]placement, bool) {
	s := solver.New(e.problem())
	if s.Solve() != solver.Sat {
		return nil, false
	}
	return e.decode(s.Model()), true
}

// decode turns a satisfying model into item placements (scaled-integer coords).
func (e *enc) decode(model []bool) []placement {
	get := func(v int) bool { return v > 0 && v <= len(model) && model[v-1] }

	out := make([]placement, len(e.items))
	for c := range e.items {
		it := e.items[c]
		// bin
		bin := 0
		for j := 0; j <= e.maxBin(c); j++ {
			if get(e.sVar[c][j]) {
				bin = j
				break
			}
		}
		// x: posX[smallest i with px_c ≤ posX[i] true]; last position if none.
		x := e.posX[len(e.posX)-1]
		for i := 0; i < len(e.pxVar[c]); i++ {
			if get(e.pxVar[c][i]) {
				x = e.posX[i]
				break
			}
		}
		y := e.posY[len(e.posY)-1]
		for i := 0; i < len(e.pyVar[c]); i++ {
			if get(e.pyVar[c][i]) {
				y = e.posY[i]
				break
			}
		}
		rotated := e.rotate && it.rotate && get(e.rVar[c])
		w, h := it.w, it.h
		if rotated {
			w, h = it.h, it.w
		}
		out[c] = placement{item: c, bin: bin, x: x, y: y, w: w, h: h, rotated: rotated}
	}
	return out
}
