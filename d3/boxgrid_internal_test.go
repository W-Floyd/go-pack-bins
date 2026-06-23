package d3

import (
	"math"
	"testing"
)

// TestBoxGridConflictMatchesBrute drives an ExtremePoint through real placements
// and, after each one, checks the spatial-grid conflict test against the
// linear-scan reference on a batch of random query boxes (and on every candidate
// extreme point). Agreement everywhere is the guarantee that routing conflicts
// through the grid leaves the packing bit-identical — only faster.
func TestBoxGridConflictMatchesBrute(t *testing.T) {
	rng := uint64(0x2545f4914f6cdd1d)
	next := func(n int) int {
		rng = rng*6364136223846793005 + 1442695040888963407
		return int((rng >> 33) % uint64(n))
	}
	// A spread of bin shapes so cell sizing (cube and oblong) is exercised.
	bins := [][3]float64{{20, 20, 20}, {12, 30, 8}, {75, 75, 75}, {5, 5, 5}}
	sizes := []float64{1, 2, 3, 4, 5, 6}

	for _, bin := range bins {
		for trial := 0; trial < 40; trial++ {
			ep := NewExtremePoint(bin[0], bin[1], bin[2])
			for step := 0; step < 80; step++ {
				w := sizes[next(len(sizes))]
				d := sizes[next(len(sizes))]
				h := sizes[next(len(sizes))]
				if _, _, _, _, _, _, ok := ep.TryInsert([][3]float64{{w, d, h}}); !ok {
					continue
				}
				// Random query boxes spanning the bin.
				for q := 0; q < 12; q++ {
					qx := float64(next(int(bin[0]) + 1))
					qy := float64(next(int(bin[1]) + 1))
					qz := float64(next(int(bin[2]) + 1))
					qw := sizes[next(len(sizes))]
					qd := sizes[next(len(sizes))]
					qh := sizes[next(len(sizes))]
					if ep.grid.conflict(qx, qy, qz, qw, qd, qh, ep.placed) != ep.conflictsBrute(qx, qy, qz, qw, qd, qh) {
						t.Fatalf("bin %v trial %d step %d: grid/brute disagree at query %v size %v",
							bin, trial, step, [3]float64{qx, qy, qz}, [3]float64{qw, qd, qh})
					}
				}
				// Every candidate extreme point must agree too (these drive selection).
				for _, p := range ep.extremePoints() {
					if ep.grid.conflict(p[0], p[1], p[2], w, d, h, ep.placed) != ep.conflictsBrute(p[0], p[1], p[2], w, d, h) {
						t.Fatalf("bin %v trial %d step %d: grid/brute disagree at EP %v", bin, trial, step, p)
					}
				}
			}
		}
	}
}

// TestBLFGridMatchesBrute is the BLF analogue: its grid-backed conflicts (eps
// overlap on all three axes) and supported (top face flush at z) must match
// linear-scan references after every placement, so BLF packs bit-identically.
func TestBLFGridMatchesBrute(t *testing.T) {
	rng := uint64(0x94d049bb133111eb)
	next := func(n int) int {
		rng = rng*6364136223846793005 + 1442695040888963407
		return int((rng >> 33) % uint64(n))
	}
	bruteConflict := func(placed []box, x, y, z, w, d, h float64) bool {
		for _, b := range placed {
			if overlap1D(x, x+w, b.x, b.x+b.w) > compactEps &&
				overlap1D(y, y+d, b.y, b.y+b.d) > compactEps &&
				overlap1D(z, z+h, b.z, b.z+b.h) > compactEps {
				return true
			}
		}
		return false
	}
	bruteSupport := func(placed []box, x, y, z, w, d float64) bool {
		if z <= compactEps {
			return true
		}
		for _, b := range placed {
			if math.Abs(b.z+b.h-z) <= compactEps &&
				overlap1D(x, x+w, b.x, b.x+b.w) > compactEps &&
				overlap1D(y, y+d, b.y, b.y+b.d) > compactEps {
				return true
			}
		}
		return false
	}
	bins := [][3]float64{{20, 20, 20}, {12, 30, 8}, {75, 75, 75}, {5, 5, 5}}
	sizes := []float64{1, 2, 3, 4, 5, 6}
	for _, bin := range bins {
		for trial := 0; trial < 40; trial++ {
			s := NewBottomLeftFill(bin[0], bin[1], bin[2])
			for step := 0; step < 80; step++ {
				w := sizes[next(len(sizes))]
				d := sizes[next(len(sizes))]
				h := sizes[next(len(sizes))]
				if _, _, _, _, _, _, ok := s.TryInsert([][3]float64{{w, d, h}}); !ok {
					continue
				}
				for q := 0; q < 12; q++ {
					qx := float64(next(int(bin[0]) + 1))
					qy := float64(next(int(bin[1]) + 1))
					qz := float64(next(int(bin[2]) + 1))
					qw, qd, qh := sizes[next(len(sizes))], sizes[next(len(sizes))], sizes[next(len(sizes))]
					if s.conflicts(qx, qy, qz, qw, qd, qh) != bruteConflict(s.placed, qx, qy, qz, qw, qd, qh) {
						t.Fatalf("bin %v trial %d step %d: BLF conflicts grid/brute disagree", bin, trial, step)
					}
					if s.supported(qx, qy, qz, qw, qd) != bruteSupport(s.placed, qx, qy, qz, qw, qd) {
						t.Fatalf("bin %v trial %d step %d: BLF supported grid/brute disagree", bin, trial, step)
					}
				}
			}
		}
	}
}
