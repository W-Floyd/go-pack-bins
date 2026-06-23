package d3

import "math"

// EmptyMaximalSpace implements 3-D placement via the Empty-Maximal-Space (EMS)
// method — the 3-D analogue of the 2-D MaxRects strategy. Instead of tracking
// corner points (as the extreme-point and BLF strategies do), it maintains the
// set of *maximal* empty boxes that remain free in the bin. Each item is placed
// at the back-bottom-left corner of the empty space that holds it most snugly;
// every empty space the new box intrudes on is then replaced by the up-to-six
// maximal slabs of free volume around the box, and any space contained in
// another is dropped so the set stays minimal.
//
// Maintaining whole free volumes (rather than a corner list) lets EMS find
// placements the corner methods miss, so it typically packs tighter — at the
// cost of an O(n²) space-maintenance step per insert. Source: Parreño,
// Alvarez-Valdés, Oliveira & Tamarit, "A maximal-space algorithm for the
// container loading problem" (INFORMS J. Computing, 2008); Lai & Chan (1997).
type EmptyMaximalSpace struct {
	binW, binD, binH float64
	spaces           []box // current empty maximal spaces (free volumes)
	placed           []box
	usedVol          float64
	contact          ContactSpec

	// Scratch reused across commits to avoid per-step allocation. spare is a
	// second backing array for the space set: commit reads e.spaces and writes
	// the rebuilt set into spare, then swaps — so the two never alias mid-rebuild.
	// isNewBuf is the parallel new-slab flag buffer.
	spare    []box
	isNewBuf []bool
}

// NewEmptyMaximalSpace creates an EMS strategy for a bin of the given dimensions.
func NewEmptyMaximalSpace(w, d, h float64) *EmptyMaximalSpace {
	return &EmptyMaximalSpace{
		binW: w, binD: d, binH: h,
		spaces: []box{{0, 0, 0, w, d, h}},
	}
}

// NewEMSStrategy matches Factory3D's strategy-constructor signature.
func NewEMSStrategy(w, d, h float64) PlacementStrategy3D {
	return NewEmptyMaximalSpace(w, d, h)
}

// NewEMSStrategyContact returns a Factory3D-compatible constructor that applies
// the spec's hard support gates (Bottom fraction and NoFloating). Lateral
// SideX/SideY targets are not used by EMS — those are handled by the separate
// compaction pass, exactly as for the BLF strategy.
func NewEMSStrategyContact(spec ContactSpec) func(w, d, h float64) PlacementStrategy3D {
	return func(w, d, h float64) PlacementStrategy3D {
		e := NewEmptyMaximalSpace(w, d, h)
		e.contact = spec
		return e
	}
}

func (e *EmptyMaximalSpace) Utilization() float64 {
	total := e.binW * e.binD * e.binH
	if total == 0 {
		return 1
	}
	return e.usedVol / total
}

func (e *EmptyMaximalSpace) Remaining() float64 {
	return e.binW*e.binD*e.binH - e.usedVol
}

func (e *EmptyMaximalSpace) TryInsert(orientations [][3]float64) (rx, ry, rz, rw, rd, rh float64, ok bool) {
	bestSet := false
	var best box
	var bestSpaceVol float64

	for _, o := range orientations {
		w, d, h := o[0], o[1], o[2]
		if w > e.binW || d > e.binD || h > e.binH {
			continue
		}
		for _, s := range e.spaces {
			if w > s.w+compactEps || d > s.d+compactEps || h > s.h+compactEps {
				continue
			}
			x, y, z := s.x, s.y, s.z // back-bottom-left corner of the space
			if e.gated(x, y, z, w, d) {
				continue
			}
			c := box{x, y, z, w, d, h}
			sv := s.w * s.d * s.h
			if !bestSet || betterEMS(c, best, sv, bestSpaceVol) {
				best, bestSpaceVol, bestSet = c, sv, true
			}
		}
	}

	if !bestSet {
		return 0, 0, 0, 0, 0, 0, false
	}
	e.commit(best)
	return best.x, best.y, best.z, best.w, best.d, best.h, true
}

// gated reports whether a placement fails the spec's hard support gates.
func (e *EmptyMaximalSpace) gated(x, y, z, w, d float64) bool {
	if e.contact.Bottom <= 0 && !e.contact.NoFloating {
		return false
	}
	sf := footprintSupport(e.placed, x, y, z, w, d)
	if sf < e.contact.Bottom {
		return true
	}
	if e.contact.NoFloating && sf <= compactEps {
		return true
	}
	return false
}

// betterEMS prefers the lower landing (z, then y, then x); among equal corners
// it prefers the tighter containing space (smaller free volume), which favours
// filling snug pockets before opening up large regions.
func betterEMS(c, best box, spaceVol, bestSpaceVol float64) bool {
	if math.Abs(c.z-best.z) > compactEps {
		return c.z < best.z
	}
	if math.Abs(c.y-best.y) > compactEps {
		return c.y < best.y
	}
	if math.Abs(c.x-best.x) > compactEps {
		return c.x < best.x
	}
	return spaceVol < bestSpaceVol
}

// Occupy carves an already-placed box, at a known position, out of the free
// spaces — as if it had been committed by TryInsert. It reconstructs the free
// space of a bin from existing placements, so a caller can then probe the
// remaining maximal spaces (voids) with TryInsert.
func (e *EmptyMaximalSpace) Occupy(x, y, z, w, d, h float64) {
	e.commit(box{x, y, z, w, d, h})
}

// commit records the placed box and rebuilds the empty-space set: every space
// the box intrudes on is split into its surrounding maximal slabs, then any
// newly-created slab contained in another space is pruned.
//
// The space set is always an antichain (no space contains another). When box b
// is carved out, a space that does *not* overlap b is carried over unchanged —
// and such a space can never become redundant: it cannot be contained by a new
// slab (every slab is a sub-region of some overlapped space s, and if a carried
// space were inside that slab it would have been inside s, contradicting the
// antichain) nor by another carried space (antichain). So only the new slabs can
// be dropped, which is what lets pruneNewSlabs skip the carried spaces and turn
// the old whole-set O(S²) prune (→ O(S³) build-up over a pack) into O(slabs·S).
func (e *EmptyMaximalSpace) commit(b box) {
	e.placed = append(e.placed, b)
	e.usedVol += b.w * b.d * b.h

	// Build the rebuilt set into the spare backing array (never aliasing e.spaces,
	// which we are reading) and the reusable new-slab flag buffer.
	next := e.spare[:0]
	isNew := e.isNewBuf[:0]
	for _, s := range e.spaces {
		if !boxesOverlap(s, b) {
			next = append(next, s)
			isNew = append(isNew, false)
			continue
		}
		for _, sl := range splitSpace(s, b) {
			next = append(next, sl)
			isNew = append(isNew, true)
		}
	}
	kept := pruneNewSlabs(next, isNew)
	// Swap buffers: the old space set's backing array becomes next round's spare.
	e.spare = e.spaces[:0]
	e.spaces = kept
	e.isNewBuf = isNew
}

// splitSpace returns the up-to-six maximal empty slabs of space s left free
// after box b is carved out of it. Each slab spans the full extent of s on the
// two axes it is not bounded by, which is what keeps it maximal.
func splitSpace(s, b box) []box {
	var out []box
	add := func(x, y, z, w, d, h float64) {
		if w > compactEps && d > compactEps && h > compactEps {
			out = append(out, box{x, y, z, w, d, h})
		}
	}
	add(s.x, s.y, s.z, b.x-s.x, s.d, s.h)                 // x−
	add(b.x+b.w, s.y, s.z, (s.x+s.w)-(b.x+b.w), s.d, s.h) // x+
	add(s.x, s.y, s.z, s.w, b.y-s.y, s.h)                 // y−
	add(s.x, b.y+b.d, s.z, s.w, (s.y+s.d)-(b.y+b.d), s.h) // y+
	add(s.x, s.y, s.z, s.w, s.d, b.z-s.z)                 // z−
	add(s.x, s.y, b.z+b.h, s.w, s.d, (s.z+s.h)-(b.z+b.h)) // z+
	return out
}

// pruneContained drops any space wholly contained within another, so the
// returned set is minimal (no redundant overlaps).
func pruneContained(spaces []box) []box {
	keep := make([]bool, len(spaces))
	for i := range spaces {
		keep[i] = true
	}
	for i := range spaces {
		if !keep[i] {
			continue
		}
		for j := range spaces {
			if i == j || !keep[j] {
				continue
			}
			if contains(spaces[j], spaces[i]) && !sameBox(spaces[i], spaces[j]) {
				keep[i] = false
				break
			}
		}
	}
	out := spaces[:0]
	for i, s := range spaces {
		if keep[i] {
			out = append(out, s)
		}
	}
	return out
}

// pruneNewSlabs drops any newly-created slab (isNew[i]) wholly contained within
// another space, leaving carried-over spaces (isNew[i]==false) untouched. Given
// the antichain invariant a carried space is never contained by anything in the
// set, so visiting only the slabs yields exactly the same result as a whole-set
// pruneContained — see commit's comment — at a fraction of the cost. The inner
// scan, containment test and !keep[j] short-circuit mirror pruneContained
// exactly so the kept set (and its order) is identical.
func pruneNewSlabs(spaces []box, isNew []bool) []box {
	keep := make([]bool, len(spaces))
	for i := range spaces {
		keep[i] = true
	}
	for i := range spaces {
		if !isNew[i] {
			continue
		}
		for j := range spaces {
			if i == j || !keep[j] {
				continue
			}
			if contains(spaces[j], spaces[i]) && !sameBox(spaces[i], spaces[j]) {
				keep[i] = false
				break
			}
		}
	}
	out := spaces[:0]
	for i, s := range spaces {
		if keep[i] {
			out = append(out, s)
		}
	}
	return out
}

// boxesOverlap reports whether two boxes intersect with positive volume.
func boxesOverlap(a, b box) bool {
	return overlap1D(a.x, a.x+a.w, b.x, b.x+b.w) > compactEps &&
		overlap1D(a.y, a.y+a.d, b.y, b.y+b.d) > compactEps &&
		overlap1D(a.z, a.z+a.h, b.z, b.z+b.h) > compactEps
}

// contains reports whether box a wholly encloses box b.
func contains(a, b box) bool {
	return a.x <= b.x+compactEps && a.y <= b.y+compactEps && a.z <= b.z+compactEps &&
		a.x+a.w >= b.x+b.w-compactEps &&
		a.y+a.d >= b.y+b.d-compactEps &&
		a.z+a.h >= b.z+b.h-compactEps
}

func sameBox(a, b box) bool {
	return math.Abs(a.x-b.x) <= compactEps && math.Abs(a.y-b.y) <= compactEps && math.Abs(a.z-b.z) <= compactEps &&
		math.Abs(a.w-b.w) <= compactEps && math.Abs(a.d-b.d) <= compactEps && math.Abs(a.h-b.h) <= compactEps
}

// footprintSupport returns the fraction of an item's (w×d) bottom face at height
// z that rests on the bin floor or the top faces of placed boxes. Shared by the
// EMS and heightmap strategies. Placed boxes never overlap, so summing per-box
// intersection areas at the same top height is exact.
func footprintSupport(placed []box, x, y, z, w, d float64) float64 {
	if z <= compactEps {
		return 1
	}
	fp := w * d
	if fp == 0 {
		return 1
	}
	sup := 0.0
	for _, b := range placed {
		if math.Abs(b.z+b.h-z) > compactEps {
			continue
		}
		iw := overlap1D(x, x+w, b.x, b.x+b.w)
		id := overlap1D(y, y+d, b.y, b.y+b.d)
		if iw > 0 && id > 0 {
			sup += iw * id
		}
	}
	return sup / fp
}

var _ PlacementStrategy3D = (*EmptyMaximalSpace)(nil)
