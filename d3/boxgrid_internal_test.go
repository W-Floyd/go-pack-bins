package d3

import "testing"

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
