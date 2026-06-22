package meta_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/meta"
)

// BestOf/LexBestOf run candidates concurrently; the winner and tie-break must be
// deterministic (reduced in candidate order). Equal-bin candidates must resolve
// to the earliest, every run.
func TestParallelMetaDeterministic(t *testing.T) {
	bestOf := func() string {
		p := meta.BestOf(fixedResult("a", 3), fixedResult("b", 2), fixedResult("c", 2), fixedResult("d", 4))
		p.PackAll(nil)
		return p.Winner()
	}
	for i := 0; i < 50; i++ {
		if w := bestOf(); w != "b" { // first of the two 2-bin candidates wins
			t.Fatalf("BestOf winner = %q, want \"b\" (run %d)", w, i)
		}
	}

	lexOf := func() string {
		p := meta.LexBestOf([]meta.Metric{meta.BinsUsed}, fixedResult("a", 3), fixedResult("b", 2), fixedResult("c", 2))
		p.PackAll(nil)
		return p.Winner()
	}
	for i := 0; i < 50; i++ {
		if w := lexOf(); w != "b" {
			t.Fatalf("LexBestOf winner = %q, want \"b\" (run %d)", w, i)
		}
	}
}
