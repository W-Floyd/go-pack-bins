package sat

import (
	"context"
	"os"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	"github.com/W-Floyd/go-pack-bins/d2"
)

// peakHeap runs fn while sampling HeapAlloc, returning the peak above a clean
// baseline (in MiB) and fn's wall-clock duration.
func peakHeap(fn func()) (float64, time.Duration) {
	runtime.GC()
	debug.FreeOSMemory()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	stop, done := make(chan struct{}), make(chan struct{})
	var peak uint64
	go func() {
		tk := time.NewTicker(time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				close(done)
				return
			case <-tk.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				if m.HeapAlloc > peak {
					peak = m.HeapAlloc
				}
			}
		}
	}()
	t0 := time.Now()
	fn()
	d := time.Since(t0)
	close(stop)
	<-done
	mbVal := float64(int64(peak)-int64(base.HeapAlloc)) / (1 << 20)
	if mbVal < 0 {
		mbVal = 0
	}
	return mbVal, d
}

// TestIncrementalSpeed compares the incremental and non-incremental (per-k rebuild)
// strategies on instances with a non-trivial gap between the area lower bound and
// the FFD upper bound — where the search does several probes, so the cost of
// re-encoding the formula per probe (non-incremental) shows up. Env-gated; run with:
//
//	SAT_INC_SPEED=1 go test ./sat/ -run TestIncrementalSpeed -v
func TestIncrementalSpeed(t *testing.T) {
	if os.Getenv("SAT_INC_SPEED") == "" {
		t.Skip("set SAT_INC_SPEED=1 to run the incremental-vs-rebuild timing")
	}
	type inst struct {
		name string
		n    int
		w, h float64
		W, H float64
	}
	// Items too big to share a bin (in either dimension) → optimum = n, area LB much
	// lower → wide LB..UB gap → the search does several probes. The larger grids make
	// the formula big enough that peak heap is meaningful to compare.
	cases := []inst{
		{"12 items, 6x6 in 10x10", 12, 6, 6, 10, 10},
		{"24 items, 9x9 in 14x14", 24, 9, 9, 14, 14},
		{"30 items, 120x120 in 200x200", 30, 120, 120, 200, 200},
	}
	mk := func(n int, w, h float64) []*d2.Item2D {
		out := make([]*d2.Item2D, n)
		for i := range out {
			out[i] = d2.NewItem(string(rune('a'+i%26)), w, h, false)
		}
		return out
	}
	for _, c := range cases {
		var inc, bin Result
		peakInc, dInc := peakHeap(func() {
			inc, _ = Pack2D(context.Background(), mk(c.n, c.w, c.h), c.W, c.H, Options{SymmetryBreak: true})
		})
		peakBin, dBin := peakHeap(func() {
			bin, _ = Pack2D(context.Background(), mk(c.n, c.w, c.h), c.W, c.H, Options{SymmetryBreak: true, NonIncremental: true})
		})
		if inc.BinsUsed() != bin.BinsUsed() {
			t.Errorf("%s: strategies disagree: %d vs %d", c.name, inc.BinsUsed(), bin.BinsUsed())
		}
		t.Logf("%-30s bins=%d  incr: %-8s peak %6.0fMB | rebuild: %-8s peak %6.0fMB | speedup %.2fx",
			c.name, inc.BinsUsed(),
			dInc.Round(time.Microsecond), peakInc,
			dBin.Round(time.Microsecond), peakBin,
			float64(dBin)/float64(dInc))
	}
}
