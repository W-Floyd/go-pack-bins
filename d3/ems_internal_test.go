package d3

import (
	"testing"
)

// TestPruneNewSlabsMatchesGlobal drives an EMS bin through many real commits and,
// at every step, checks that the fast incremental prune (pruneNewSlabs, used by
// commit) yields exactly the same maximal-space set — same elements, same order —
// as the original whole-set pruneContained. This is the guarantee that the
// O(slabs·S) optimisation is output-identical to the O(S²) version it replaced,
// so EMS / fit / Occupy all pack bit-for-bit as before, only faster.
func TestPruneNewSlabsMatchesGlobal(t *testing.T) {
	// A small deterministic LCG so the fuzz is reproducible.
	rng := uint64(0x9e3779b97f4a7c15)
	next := func(n int) int {
		rng = rng*6364136223846793005 + 1442695040888963407
		return int((rng >> 33) % uint64(n))
	}

	for trial := 0; trial < 400; trial++ {
		e := NewEmptyMaximalSpace(20, 20, 20)
		for step := 0; step < 60; step++ {
			if len(e.spaces) == 0 {
				break
			}
			// Carve a random sub-box out of a random current space, so the box
			// genuinely overlaps the set and exercises splitting + pruning.
			s := e.spaces[next(len(e.spaces))]
			b := randomSubBox(s, next)
			if b.w <= compactEps || b.d <= compactEps || b.h <= compactEps {
				continue
			}

			// Build next+isNew exactly as commit does, then compare the two prunes
			// on identical input before letting the real commit advance the state.
			var cand []box
			var isNew []bool
			for _, sp := range e.spaces {
				if !boxesOverlap(sp, b) {
					cand = append(cand, sp)
					isNew = append(isNew, false)
					continue
				}
				for _, sl := range splitSpace(sp, b) {
					cand = append(cand, sl)
					isNew = append(isNew, true)
				}
			}
			got := pruneNewSlabs(clone(cand), append([]bool(nil), isNew...))
			want := pruneContained(clone(cand))
			if !sameSpaces(got, want) {
				t.Fatalf("trial %d step %d: incremental prune diverged\n  box=%v\n  got =%v\n  want=%v",
					trial, step, b, got, want)
			}

			e.commit(b) // advances e.spaces via pruneNewSlabs, keeping the invariant
		}
	}
}

// TestEMS_CommitNoCorruption packs random items through real TryInsert/commit
// cycles and asserts the result is always physically valid — placements inside
// the bin, no two overlapping, used volume consistent. This guards the commit
// space-maintenance (incl. any scratch-buffer reuse) against aliasing/corruption
// that the pure prune-equivalence test would not see.
func TestEMS_CommitNoCorruption(t *testing.T) {
	rng := uint64(0xd1b54a32d192ed03)
	next := func(n int) int {
		rng = rng*6364136223846793005 + 1442695040888963407
		return int((rng >> 33) % uint64(n))
	}
	dimset := []float64{2, 3, 4, 5, 6}
	for trial := 0; trial < 300; trial++ {
		e := NewEmptyMaximalSpace(20, 20, 20)
		var placed []box
		var usedVol float64
		for step := 0; step < 120; step++ {
			w := dimset[next(len(dimset))]
			d := dimset[next(len(dimset))]
			h := dimset[next(len(dimset))]
			x, y, z, pw, pd, ph, ok := e.TryInsert([][3]float64{{w, d, h}})
			if !ok {
				continue
			}
			b := box{x, y, z, pw, pd, ph}
			if x < -compactEps || y < -compactEps || z < -compactEps ||
				x+pw > 20+compactEps || y+pd > 20+compactEps || z+ph > 20+compactEps {
				t.Fatalf("trial %d: placement %v escapes the bin", trial, b)
			}
			for _, q := range placed {
				if boxesOverlap(b, q) {
					t.Fatalf("trial %d: placement %v overlaps existing %v", trial, b, q)
				}
			}
			placed = append(placed, b)
			usedVol += pw * pd * ph
		}
		if absDiff(usedVol, e.usedVol) > compactEps {
			t.Fatalf("trial %d: usedVol drift: tracked %.3f vs actual %.3f", trial, e.usedVol, usedVol)
		}
	}
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

func clone(bs []box) []box { return append([]box(nil), bs...) }

func randomSubBox(s box, next func(int) int) box {
	// Pick offsets and extents on a coarse grid inside s so sub-boxes line up and
	// produce the exact-overlap / containment cases the prune must handle.
	step := func(extent float64) (off, size float64) {
		const g = 4
		a, b := next(g+1), next(g+1)
		lo, hi := a, b
		if lo > hi {
			lo, hi = hi, lo
		}
		off = extent * float64(lo) / g
		size = extent * float64(hi-lo) / g
		return
	}
	ox, sw := step(s.w)
	oy, sd := step(s.d)
	oz, sh := step(s.h)
	return box{s.x + ox, s.y + oy, s.z + oz, sw, sd, sh}
}

func sameSpaces(a, b []box) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameBox(a[i], b[i]) {
			return false
		}
	}
	return true
}
