package meta

import (
	"runtime"
	"sync"
)

// parallelFor runs body(i) for each i in [0,n) across a bounded pool of about
// NumCPU workers, returning once all have completed. body must be safe to call
// concurrently — write outputs into pre-sized, per-index storage and reduce them
// afterwards in index order, so the result is independent of scheduling.
//
// Under GOOS=js (wasm is single-threaded) the goroutines run cooperatively on one
// thread: correctness is unchanged, only the speedup is absent.
func parallelFor(n int, body func(i int)) {
	if n <= 1 {
		for i := 0; i < n; i++ {
			body(i)
		}
		return
	}
	workers := runtime.NumCPU()
	if workers > n {
		workers = n
	}
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				body(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
}
