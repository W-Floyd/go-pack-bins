// Command bench compares go-pack-bins against bavix/boxpacker3 on identical 3-D
// packing instances, reporting both result quality (bins used, fill rate, items
// left unplaced) and wall-clock time.
//
// It lives in its own module so boxpacker3 is not a dependency of the main
// (dependency-free) go-pack-bins module. Run it from this directory:
//
//	cd bench && go run .            # default instances
//	go run . -items 50,200,800     # custom item counts
//	go run . -runs 5               # average timing over N runs (min reported)
//
// Both libraries are given the same items and the same single box size; each is
// asked to minimise box count (FFD-equivalent) and, separately, best-fit. The
// two engines interpret the box axes differently internally, but both consider
// all six rotations and the same volume, so the comparison is fair on the
// quantities that matter: how many boxes, how full, how fast.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	bp3 "github.com/bavix/boxpacker3"

	"github.com/W-Floyd/go-pack-bins/packapi"
)

// instance is one packing problem: a fixed box and a list of items to fit.
type instance struct {
	name string
	box  dims
	itm  []dims
}

type dims struct{ w, h, d float64 }

func (x dims) vol() float64 { return x.w * x.h * x.d }

// stats is the outcome of one solve.
type stats struct {
	bins     int
	unplaced int
	fillPct  float64       // packed item volume / (bins × box volume)
	took     time.Duration // best wall-clock time over -runs
}

func main() {
	itemsFlag := flag.String("items", "20,50,100,300", "comma-separated item counts to benchmark")
	runs := flag.Int("runs", 3, "timing runs per solve (minimum time reported)")
	seed := flag.Int64("seed", 42, "RNG seed for reproducible instances")
	flag.Parse()

	counts := parseInts(*itemsFlag)
	insts := make([]instance, 0, len(counts))
	for _, n := range counts {
		insts = append(insts, genInstance(n, *seed))
	}

	// Each row pairs a go-pack-bins algorithm with the boxpacker3 strategy that
	// implements the same idea, so the columns are directly comparable.
	pairs := []struct {
		label string
		ours  string
		bp3   bp3.PackingStrategy
	}{
		{"FFD (minimize boxes)", "ffd", bp3.StrategyMinimizeBoxes},
		{"BFD (best-fit decreasing)", "bfd", bp3.StrategyBestFitDecreasing},
	}

	fmt.Printf("go-pack-bins vs boxpacker3 — %d runs/solve, seed %d\n", *runs, *seed)
	for _, inst := range insts {
		fmt.Printf("\n=== %s · box %g×%g×%g · total item vol %.0f ===\n",
			inst.name, inst.box.w, inst.box.h, inst.box.d, totalVol(inst.itm))
		fmt.Printf("%-26s  %-28s  %-28s\n", "strategy", "go-pack-bins", "boxpacker3")
		fmt.Printf("%-26s  %-28s  %-28s\n", strings.Repeat("-", 26), strings.Repeat("-", 28), strings.Repeat("-", 28))
		for _, p := range pairs {
			ours := runOurs(p.ours, inst, *runs)
			theirs := runBp3(p.bp3, inst, *runs)
			fmt.Printf("%-26s  %-28s  %-28s\n", p.label, fmtStats(ours), fmtStats(theirs))
		}
	}
	fmt.Println("\nfill% = packed item volume ÷ (bins × box volume); higher is tighter.")
}

func fmtStats(s stats) string {
	unfit := ""
	if s.unplaced > 0 {
		unfit = fmt.Sprintf(" !%d unfit", s.unplaced)
	}
	return fmt.Sprintf("%2d bins  %5.1f%%  %8s%s", s.bins, s.fillPct, s.took.Round(time.Microsecond), unfit)
}

// ─── go-pack-bins ────────────────────────────────────────────────────────────

func runOurs(algo string, inst instance, runs int) stats {
	items := make([]packapi.ItemSpec, len(inst.itm))
	for i, it := range inst.itm {
		items[i] = packapi.ItemSpec{
			ID: "i" + strconv.Itoa(i), Width: it.w, Height: it.h, Depth: it.d, AllowRotate: true,
		}
	}
	req := packapi.PackRequest{
		Mode:      "3d",
		Algorithm: algo,
		Bin:       packapi.BinSpec{Width: inst.box.w, Height: inst.box.h, Depth: inst.box.d},
		Items:     items,
	}

	var resp packapi.PackResponse
	best := time.Duration(1<<63 - 1)
	for r := 0; r < runs; r++ {
		t0 := time.Now()
		resp = packapi.PackCtx(context.Background(), req)
		if d := time.Since(t0); d < best {
			best = d
		}
	}

	unplacedVol := 0.0
	unplaced := map[string]bool{}
	for _, id := range resp.Unplaced {
		unplaced[id] = true
	}
	for i, it := range inst.itm {
		if unplaced["i"+strconv.Itoa(i)] {
			unplacedVol += it.vol()
		}
	}
	return makeStats(resp.BinsUsed, len(resp.Unplaced), totalVol(inst.itm)-unplacedVol, inst.box.vol(), best)
}

// ─── boxpacker3 ──────────────────────────────────────────────────────────────

func runBp3(strategy bp3.PackingStrategy, inst instance, runs int) stats {
	// Give boxpacker3 as many identical boxes as there are items (an upper bound
	// on boxes needed); it uses as few as the strategy allows. maxWeight is set
	// huge so weight never gates a purely geometric comparison.
	const bigWeight = 1e18
	build := func() ([]*bp3.Box, []*bp3.Item) {
		boxes := make([]*bp3.Box, len(inst.itm))
		for i := range boxes {
			boxes[i] = bp3.NewBox("box"+strconv.Itoa(i), inst.box.w, inst.box.h, inst.box.d, bigWeight)
		}
		items := make([]*bp3.Item, len(inst.itm))
		for i, it := range inst.itm {
			items[i] = bp3.NewItem("i"+strconv.Itoa(i), it.w, it.h, it.d, 0)
		}
		return boxes, items
	}

	packer := bp3.NewPacker(bp3.WithStrategy(strategy))
	var res *bp3.Result
	best := time.Duration(1<<63 - 1)
	for r := 0; r < runs; r++ {
		boxes, items := build() // fresh inputs each run; the packer mutates boxes
		t0 := time.Now()
		res, _ = packer.PackCtx(context.Background(), boxes, items)
		if d := time.Since(t0); d < best {
			best = d
		}
	}

	bins, packedVol := 0, 0.0
	for _, b := range res.Boxes {
		used := b.GetItems()
		if len(used) == 0 {
			continue
		}
		bins++
		for _, it := range used {
			packedVol += it.GetVolume()
		}
	}
	return makeStats(bins, len(res.UnfitItems), packedVol, inst.box.vol(), best)
}

// ─── helpers ────────────────────────────────────────────────────────────────

func makeStats(bins, unplaced int, packedVol, boxVol float64, took time.Duration) stats {
	fill := 0.0
	if bins > 0 && boxVol > 0 {
		fill = 100 * packedVol / (float64(bins) * boxVol)
	}
	return stats{bins: bins, unplaced: unplaced, fillPct: fill, took: took}
}

func totalVol(items []dims) float64 {
	v := 0.0
	for _, it := range items {
		v += it.vol()
	}
	return v
}

// genInstance builds a deterministic instance of n items: a 10×10×10 box and
// items whose sides are 2..5 (so several fit per box, with non-trivial gaps).
func genInstance(n int, seed int64) instance {
	rng := rand.New(rand.NewSource(seed + int64(n))) // vary per size, still reproducible
	side := func() float64 { return float64(2 + rng.Intn(4)) } // 2..5
	items := make([]dims, n)
	for i := range items {
		items[i] = dims{w: side(), h: side(), d: side()}
	}
	return instance{name: fmt.Sprintf("%d items", n), box: dims{10, 10, 10}, itm: items}
}

func parseInts(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}
