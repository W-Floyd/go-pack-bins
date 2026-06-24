package sat

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"testing"
	"time"
)

// TestMemSweep measures peak heap as the item count grows, to calibrate the
// MaxClauses / MaxGridCells guards against real memory use. It is gated behind an
// env var so it never runs in the normal suite (it is slow and deliberately
// allocates a lot). Run with:
//
//	SAT_MEM_SWEEP=1 go test ./sat/ -run TestMemSweep -v -timeout 30m
//
// It builds the SAT formula and solves it for each n on a fixed grid (so the
// only variable is item count), sampling HeapAlloc throughout. It stops as soon
// as peak heap crosses a safe ceiling so the measurement can't exhaust memory.
func TestMemSweep(t *testing.T) {
	if os.Getenv("SAT_MEM_SWEEP") == "" {
		t.Skip("set SAT_MEM_SWEEP=1 to run the memory sweep")
	}

	// Fixed grid: 10×10 items (rotatable) on a 100×100 bin (W+H = 200). Only n varies.
	const W, H, w, h = 100, 100, 10, 10
	perBin := (W / w) * (H / h)
	const safeStop = uint64(2) << 30 // stop once peak heap exceeds 2 GiB

	t.Logf("grid %dx%d, items %dx%d (rotatable), per-bin≈%d", W, H, w, h, perBin)
	t.Logf("%4s %3s %10s %12s %12s %9s %9s %9s %8s %6s",
		"n", "k", "vars", "est.cls", "clauses", "buildMB", "peakMB", "B/clause", "solve", "sat")

	for n := 60; n <= 400; n += 10 {
		items := make([]scaledItem, n)
		for i := range items {
			items[i] = scaledItem{id: fmt.Sprint(i), w: w, h: h, rotate: true}
		}
		k := n/perBin + 2 // a little slack so the instance is comfortably feasible

		posX := normalPositions(items, W, true)
		posY := normalPositions(items, H, true)
		_, estC := estimateFormula(n, len(posX), len(posY), k, true)

		runtime.GC()
		debug.FreeOSMemory()
		var base runtime.MemStats
		runtime.ReadMemStats(&base)

		// Peak sampler.
		stop := make(chan struct{})
		done := make(chan struct{})
		var peak uint64
		go func() {
			tk := time.NewTicker(3 * time.Millisecond)
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
		e := newEnc(W, H, items, k, true, true, posX, posY, 0, 0)
		var ab runtime.MemStats
		runtime.ReadMemStats(&ab)
		clauses := len(e.cards) // capture before solve(); problem() frees e.cards
		_, sat := e.solve()
		dt := time.Since(t0)

		close(stop)
		<-done

		vars := e.nVars
		buildMB := mb(int64(ab.HeapAlloc) - int64(base.HeapAlloc))
		peakMB := mb(int64(peak) - int64(base.HeapAlloc))
		bPerClause := 0.0
		if clauses > 0 {
			bPerClause = float64(int64(peak)-int64(base.HeapAlloc)) / float64(clauses)
		}
		t.Logf("%4d %3d %10d %12d %12d %9.0f %9.0f %9.1f %8s %6v",
			n, k, vars, estC, clauses, buildMB, peakMB, bPerClause, dt.Round(time.Millisecond), sat)

		e = nil
		if peak > safeStop {
			t.Logf("STOP: peak heap %.2f GiB exceeded the %.0f GiB safe ceiling at n=%d",
				float64(peak)/(1<<30), float64(safeStop)/(1<<30), n)
			break
		}
	}
}

func mb(b int64) float64 {
	if b < 0 {
		b = 0
	}
	return float64(b) / (1 << 20)
}
