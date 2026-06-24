package sat

import (
	"math/rand"
	"reflect"
	"testing"
)

// normalPositionsScalar is the original position-at-a-time implementation, kept as
// the reference the word-parallel bitset version is checked against.
func normalPositionsScalar(items []scaledItem, limit int, rotate bool) []int {
	reach := make([]bool, limit+1)
	reach[0] = true
	for _, it := range items {
		for p := limit; p >= 0; p-- { // high→low: each item contributes at most once
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

// TestNormalPositionsBitsetMatchesScalar checks the bitset version is bit-for-bit
// identical to the scalar reference across randomized instances (varied limits,
// dimensions, rotation), including edge cases (oversized items, exact-fit widths).
func TestNormalPositionsBitsetMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 2000; trial++ {
		limit := 1 + rng.Intn(400) // exercise both small (<64) and multi-word bitsets
		n := rng.Intn(40)
		items := make([]scaledItem, n)
		for i := range items {
			items[i] = scaledItem{
				w:      1 + rng.Intn(limit+20), // sometimes > limit
				h:      1 + rng.Intn(limit+20),
				rotate: rng.Intn(2) == 0,
			}
		}
		rotate := rng.Intn(2) == 0
		got := normalPositions(items, limit, rotate)
		want := normalPositionsScalar(items, limit, rotate)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mismatch (trial %d, limit %d, rotate %v):\n got=%v\nwant=%v", trial, limit, rotate, got, want)
		}
	}
}

// benchItems builds n items with widths/heights spread across [1, limit/3] so the
// reachable-sum set is dense (the realistic worst case for this routine).
func benchItems(n, limit int) []scaledItem {
	rng := rand.New(rand.NewSource(7))
	items := make([]scaledItem, n)
	for i := range items {
		items[i] = scaledItem{w: 1 + rng.Intn(limit/3), h: 1 + rng.Intn(limit/3), rotate: true}
	}
	return items
}

func BenchmarkNormalPositionsBitset(b *testing.B) {
	items := benchItems(200, 3000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = normalPositions(items, 3000, true)
	}
}

func BenchmarkNormalPositionsScalar(b *testing.B) {
	items := benchItems(200, 3000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = normalPositionsScalar(items, 3000, true)
	}
}
