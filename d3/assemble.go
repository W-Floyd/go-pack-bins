package d3

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// Assembler is a two-phase 3-D packer. First it fuses items that pack *perfectly*
// (zero internal waste) into larger solid rectangular pseudo-items, then it places
// those — together with any items that couldn't be fused — using the
// Empty-Maximal-Space engine, which reuses gaps so the consolidated blocks pack
// tightly without sealed voids.
//
// Fusion is a greedy guillotine construction: two boxes combine into one larger
// box whenever they share a full face — matching footprints stack in z, matching
// side faces join in x or y. Applied in rounds (largest shared face first), this
// grows perfect rectangles. Only rotatable items are fused (so the whole assembly
// can be reoriented consistently); the resulting multi-item blocks are then placed
// in a fixed orientation, while lone items keep their own rotation. A merge is
// rejected if its result would not fit the bin in any orientation, which bounds
// how large blocks grow.
//
// Each block records its constituent items' relative positions, so a placed block
// is decomposed back into real per-item placements (committed through the observer,
// so the solve streams during the placement phase).
type Assembler struct {
	w, d, h  float64
	observer pack.PlaceObserver
}

// NewAssembler creates a block-assembling + EMS packer for the given bin.
func NewAssembler(w, d, h float64) *Assembler { return &Assembler{w: w, d: d, h: h} }

// Observe registers a per-placement callback (pack.Observable).
func (a *Assembler) Observe(fn pack.PlaceObserver) { a.observer = fn }

// PackAll runs the solve with no cancellation.
func (a *Assembler) PackAll(items []pack.Item) (pack.Result, error) {
	return a.PackAllCtx(context.Background(), items)
}

// Name satisfies pack.OfflinePacker so the packer can join a meta.BestOf race.
func (a *Assembler) Name() string { return "Assemble" }

// psub is one real item inside a composite, at offset (dx,dy,dz) with size w×d×h.
type psub struct {
	id         string
	dx, dy, dz float64
	w, d, h    float64
}

// composite is a solid rectangular box built from one or more items. rotate is
// true only for a lone rotatable item (a multi-item block is placed as-built).
type composite struct {
	w, d, h float64
	subs    []psub
	rotate  bool
}

func (c composite) volume() float64 { return c.w * c.d * c.h }

var perms6 = [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}

// PackAllCtx assembles perfect blocks then places them with EMS, decomposing each
// placed block back into real per-item placements.
func (a *Assembler) PackAllCtx(ctx context.Context, items []pack.Item) (pack.Result, error) {
	var result pack.Result

	var leaves, fixed []composite
	for _, raw := range items {
		i3, ok := raw.(*Item3D)
		if !ok {
			result.Unplaced = append(result.Unplaced, raw.ID())
			continue
		}
		c := composite{w: i3.W, d: i3.D, h: i3.H, rotate: i3.AllowRotate,
			subs: []psub{{id: i3.ID(), w: i3.W, d: i3.D, h: i3.H}}}
		if i3.AllowRotate {
			leaves = append(leaves, c) // eligible for fusion
		} else {
			fixed = append(fixed, c) // fixed orientation — placed as-is
		}
	}

	blocks := a.assemble(ctx, leaves)
	all := append(blocks, fixed...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].volume() > all[j].volume() }) // biggest first

	// First-Fit-Decreasing placement of the composites with the EMS strategy.
	var bins []*Bin3D
	for _, c := range all {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		item := NewItem("", c.w, c.d, c.h, c.rotate)
		placed := false
		for _, b := range bins {
			if p, err := b.TryPlace(item); err == nil {
				a.commit(&result, c, p.(*Placement3D))
				placed = true
				break
			}
		}
		if !placed {
			b := NewBin(fmt.Sprintf("asm-bin-%d", len(bins)), a.w, a.d, a.h, NewEMSStrategy(a.w, a.d, a.h))
			if p, err := b.TryPlace(item); err == nil {
				bins = append(bins, b)
				result.Bins = append(result.Bins, b)
				a.commit(&result, c, p.(*Placement3D))
				placed = true
			}
		}
		if !placed {
			for _, s := range c.subs {
				result.Unplaced = append(result.Unplaced, s.id)
			}
		}
	}
	return result, nil
}

// commit decomposes a placed composite into real per-item placements, appends them
// to the result, and streams each through the observer. EMS may place the block in
// any orientation, so the composite is first reoriented to match the placed box's
// dimensions; its sub-items then map straight onto the placement.
func (a *Assembler) commit(result *pack.Result, c composite, p *Placement3D) {
	o := orientToDims(c, p.W, p.D, p.H)
	for _, s := range o.subs {
		pl := &Placement3D{binID: p.binID, itemID: s.id,
			X: p.X + s.dx, Y: p.Y + s.dy, Z: p.Z + s.dz, W: s.w, D: s.d, H: s.h}
		result.Placements = append(result.Placements, pl)
		if a.observer != nil {
			a.observer(pl)
		}
	}
}

// orientToDims reorients a composite so its box matches the given placed
// dimensions, so its sub-items line up with where EMS put the block. Falls back to
// the composite as-built if nothing matches (shouldn't happen).
func orientToDims(c composite, w, d, h float64) composite {
	near := func(x, y float64) bool { return x-y < blockEps && y-x < blockEps }
	for _, p := range perms6 {
		if o := permuteComposite(c, p); near(o.w, w) && near(o.d, d) && near(o.h, h) {
			return o
		}
	}
	return c
}

// assemble greedily fuses composites that share a full face into larger boxes,
// in rounds (largest shared face first) until no perfect merge fits the bin.
func (a *Assembler) assemble(ctx context.Context, comps []composite) []composite {
	for pass := 0; pass < 24; pass++ {
		if ctx.Err() != nil {
			return comps
		}
		type oref struct {
			idx int
			oc  composite
		}
		buckets := map[[2]float64][]oref{}
		var keys [][2]float64
		for i, c := range comps {
			for axis := 0; axis < 3; axis++ {
				o := orientUp(c, axis)
				k := [2]float64{o.w, o.d}
				if _, ok := buckets[k]; !ok {
					keys = append(keys, k)
				}
				buckets[k] = append(buckets[k], oref{i, o})
			}
		}
		sort.Slice(keys, func(x, y int) bool { return keys[x][0]*keys[x][1] > keys[y][0]*keys[y][1] })

		consumed := make([]bool, len(comps))
		var merged []composite
		any := false
		for _, k := range keys {
			var live []oref
			seen := map[int]bool{}
			for _, r := range buckets[k] {
				if consumed[r.idx] || seen[r.idx] {
					continue
				}
				seen[r.idx] = true
				live = append(live, r)
			}
			// Pair equal-thickness boxes first: a block of two same-thickness pieces
			// keeps a regular shape, so it still shares faces with its peers and can
			// fuse again next round — this is what drives recursion beyond pairs.
			sort.SliceStable(live, func(i, j int) bool { return live[i].oc.h < live[j].oc.h })
			for x := 0; x+1 < len(live); x += 2 {
				A, B := live[x], live[x+1]
				res := glueZ(A.oc, B.oc)
				if !a.tilesBin(res.w, res.d, res.h) {
					continue
				}
				consumed[A.idx], consumed[B.idx] = true, true
				merged = append(merged, res)
				any = true
			}
		}
		if !any {
			return comps
		}
		for i, c := range comps {
			if !consumed[i] {
				merged = append(merged, c)
			}
		}
		comps = merged
	}
	return comps
}

// orientUp reorients a composite so the given source axis (0=x,1=y,2=z) becomes
// the vertical, and normalises the footprint so w ≤ d. The footprint (w,d) is then
// the shared-face key and h is the thickness glued along z.
func orientUp(c composite, axis int) composite {
	var p [3]int
	switch axis {
	case 0:
		p = [3]int{1, 2, 0} // old x → height
	case 1:
		p = [3]int{0, 2, 1} // old y → height
	default:
		p = [3]int{0, 1, 2} // old z → height (identity)
	}
	o := permuteComposite(c, p)
	if o.w > o.d {
		o = permuteComposite(o, [3]int{1, 0, 2}) // normalise footprint to w ≤ d
	}
	return o
}

// permuteComposite reorients a composite by an axis permutation, transforming the
// box dimensions and every sub's offset and size. Axis-aligned boxes are symmetric,
// so all six permutations yield valid (possibly mirrored) layouts.
func permuteComposite(c composite, p [3]int) composite {
	pick := func(v [3]float64) [3]float64 { return [3]float64{v[p[0]], v[p[1]], v[p[2]]} }
	nd := pick([3]float64{c.w, c.d, c.h})
	out := composite{w: nd[0], d: nd[1], h: nd[2], rotate: c.rotate}
	for _, s := range c.subs {
		off := pick([3]float64{s.dx, s.dy, s.dz})
		sz := pick([3]float64{s.w, s.d, s.h})
		out.subs = append(out.subs, psub{id: s.id, dx: off[0], dy: off[1], dz: off[2], w: sz[0], d: sz[1], h: sz[2]})
	}
	return out
}

// glueZ stacks two composites sharing a footprint (bot below, top above) into one
// taller box. Only rotatable items are ever fused, so the block can itself rotate;
// commit reorients it (and its subs) to whatever orientation EMS places it in.
func glueZ(bot, top composite) composite {
	out := composite{w: bot.w, d: bot.d, h: bot.h + top.h, rotate: true}
	out.subs = append(out.subs, bot.subs...)
	for _, s := range top.subs {
		s.dz += bot.h
		out.subs = append(out.subs, s)
	}
	return out
}

// tilesBin reports whether a block tiles the bin: in some orientation each of its
// dimensions evenly divides the corresponding bin dimension. Gating merges on this
// (rather than mere fit) stops fusion from overshooting the bin's divisors — e.g.
// stacking 4-tall pieces into an 8-tall block that can't tile a height-12 bin — so
// the blocks EMS receives lay down like bricks. It self-tunes: when little divides
// the bin (random sizes), few merges pass and assemble degrades to EMS on raw items.
func (a *Assembler) tilesBin(w, d, h float64) bool {
	bin := [3]float64{a.w, a.d, a.h}
	box := [3]float64{w, d, h}
	for _, p := range perms6 {
		if divides(box[0], bin[p[0]]) && divides(box[1], bin[p[1]]) && divides(box[2], bin[p[2]]) {
			return true
		}
	}
	return false
}

// divides reports whether b evenly divides B (B = k·b for some integer k ≥ 1),
// within tolerance, and b ≤ B.
func divides(b, B float64) bool {
	if b <= blockEps || b > B+blockEps {
		return false
	}
	k := math.Round(B / b)
	return k >= 1 && math.Abs(k*b-B) <= blockEps*k+blockEps
}

var _ pack.Observable = (*Assembler)(nil)
