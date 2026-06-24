package d3

import (
	"fmt"
	"testing"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// TestBlockConsumersNoDegeneratePlacements guards every packer that consumes
// buildBlocks output. block stores its sub-items in fixed [MaxBlockSubs] arrays
// with a live count n; a consumer that ranges blk.idxs/blk.subs without slicing to
// [:blk.n] walks the zero-value tail and fabricates empty-id, zero-height
// placements (and double-counts item index 0) — the regression that silently
// dropped the column packer's compactness. Assert the placements are well formed:
// each has a non-empty id and positive height, every input item is placed exactly
// once, and none is duplicated.
func TestBlockConsumersNoDegeneratePlacements(t *testing.T) {
	mk := func() []pack.Item {
		next := newLCG(11)
		sz := []float64{1, 2, 2, 3, 3, 4, 4, 5, 6}
		items := make([]pack.Item, 600)
		for i := range items {
			items[i] = NewItem(fmt.Sprintf("i%d", i), sz[int(next()*9)%9], sz[int(next()*9)%9], sz[int(next()*9)%9], true)
		}
		return items
	}

	type packer struct {
		name string
		run  func([]pack.Item) (pack.Result, error)
	}
	for _, p := range []packer{
		{"blocks", func(its []pack.Item) (pack.Result, error) { return NewBlockPacker(20, 20, 20).PackAll(its) }},
		{"columns", func(its []pack.Item) (pack.Result, error) { return NewColumnPacker(20, 20, 20).PackAll(its) }},
	} {
		items := mk()
		r, err := p.run(items)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		seen := map[string]int{}
		for _, pl := range r.Placements {
			p3, ok := pl.(*Placement3D)
			if !ok {
				continue
			}
			if p3.itemID == "" {
				t.Fatalf("%s: placement with empty itemID — fixed-array tail not sliced to blk.n", p.name)
			}
			if p3.H <= 0 || p3.W <= 0 || p3.D <= 0 {
				t.Fatalf("%s: degenerate placement %q size %gx%gx%g", p.name, p3.itemID, p3.W, p3.D, p3.H)
			}
			seen[p3.itemID]++
		}
		for id, n := range seen {
			if n != 1 {
				t.Fatalf("%s: item %q placed %d times (want 1)", p.name, id, n)
			}
		}
		if len(seen)+len(r.Unplaced) != len(items) {
			t.Fatalf("%s: placed %d + unplaced %d != %d items", p.name, len(seen), len(r.Unplaced), len(items))
		}
	}
}
