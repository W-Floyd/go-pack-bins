package packapi

import (
	"context"
	"sync"
	"testing"
)

func items1d(n int, w float64) []ItemSpec {
	out := make([]ItemSpec, n)
	for i := range out {
		out[i] = ItemSpec{ID: string(rune('a' + i)), Width: w}
	}
	return out
}

// StreamPack must emit progress frames for slow solvers, threaded from the
// solver through ctx to the stream. Both a single metaheuristic (grasp) and the
// auto race (per-candidate) are covered.
func TestStreamProgressFrames(t *testing.T) {
	for _, algo := range []string{"grasp", "auto"} {
		var mu sync.Mutex
		var prog, total, maxDone int
		StreamPack(context.Background(), PackRequest{
			Mode: "1d", Algorithm: algo, Bin: BinSpec{Width: 10}, Items: items1d(10, 3),
		}, func(f StreamFrame) {
			mu.Lock()
			defer mu.Unlock()
			if f.Type == "progress" {
				prog++
				total = f.Total
				if f.Done > maxDone {
					maxDone = f.Done
				}
			}
		})
		if prog == 0 {
			t.Errorf("%s: no progress frames emitted", algo)
		}
		if total <= 0 || maxDone > total {
			t.Errorf("%s: bad progress total=%d maxDone=%d", algo, total, maxDone)
		}
	}
}
