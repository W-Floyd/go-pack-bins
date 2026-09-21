package d3

import "testing"

// box is a full BearBox literal with both limits unrestricted unless given.
func nb(x, y, z, w, d, h, weight, limit, pressure float64) BearBox {
	return BearBox{X: x, Y: y, Z: z, W: w, D: d, H: h,
		Weight: weight, Limit: limit, PressureLimit: pressure}
}

// The headline case: a carton whose four corner posts reach its lid carries a
// heavy box above *through the posts*. The carton's own rating never sees that
// load, so a carton far too weak to hold the box on its lid is still fine.
func TestLoadPathThroughCornerPosts(t *testing.T) {
	// 4x4x2 carton, lid at z=2, with four 1x1x2 posts one per corner.
	posts := func(limit float64) []BearBox {
		var out []BearBox
		for _, c := range [][2]float64{{0, 0}, {3, 0}, {0, 3}, {3, 3}} {
			out = append(out, nb(c[0], c[1], 0, 1, 1, 2, 1, limit, NoLimit))
		}
		return out
	}
	// The carton itself bears almost nothing on its own lid.
	carton := func(contents []BearBox) BearBox {
		b := nb(0, 0, 0, 4, 4, 2, 4, 1, NoLimit) // own limit: 1kg
		b.Contents = contents
		return b
	}
	// A heavy box resting only on the corners, matching the post layout.
	heavyOnCorners := []BearBox{
		nb(0, 0, 2, 1, 1, 1, 25, NoLimit, NoLimit),
		nb(3, 0, 2, 1, 1, 1, 25, NoLimit, NoLimit),
		nb(0, 3, 2, 1, 1, 1, 25, NoLimit, NoLimit),
		nb(3, 3, 2, 1, 1, 1, 25, NoLimit, NoLimit),
	}

	t.Run("strong posts carry it, weak carton is untouched", func(t *testing.T) {
		bs := append([]BearBox{carton(posts(100))}, heavyOnCorners...)
		if !LoadPathOK(bs) {
			t.Error("load did not transfer through the corner posts; the carton was charged for it")
		}
	})

	t.Run("weak posts are crushed even though the carton could take it", func(t *testing.T) {
		strongCarton := carton(posts(5)) // posts take 5kg; 25kg lands on each
		strongCarton.Limit = 1000
		bs := append([]BearBox{strongCarton}, heavyOnCorners...)
		if LoadPathOK(bs) {
			t.Error("25kg on a post rated for 5kg was allowed")
		}
	})

	t.Run("rigid carton carries the load itself and is crushed", func(t *testing.T) {
		c := carton(posts(100))
		c.Rigid = true // ignore contents: the lid takes everything
		bs := append([]BearBox{c}, heavyOnCorners...)
		if LoadPathOK(bs) {
			t.Error("a rigid carton rated for 1kg accepted 100kg on its lid")
		}
	})
}

// Load over a gap between contents rests on the container's own structure. Only
// the part actually over a flush content transfers.
func TestLoadPathSplitsBetweenContentsAndLid(t *testing.T) {
	// 4x1x2 carton with one 1x1x2 post at x=0. A 4x1 load spans the whole lid:
	// a quarter of it sits over the post, three quarters over empty space.
	build := func(cartonLimit float64) []BearBox {
		c := nb(0, 0, 0, 4, 1, 2, 1, cartonLimit, NoLimit)
		c.Contents = []BearBox{nb(0, 0, 0, 1, 1, 2, 1, 100, NoLimit)}
		return []BearBox{c, nb(0, 0, 2, 4, 1, 1, 40, NoLimit, NoLimit)}
	}
	// 40kg spread over 4 units of area: 10kg over the post, 30kg on the lid.
	if !LoadPathOK(build(30)) {
		t.Error("carton rated for 30kg refused the 30kg actually resting on it")
	}
	if LoadPathOK(build(29)) {
		t.Error("carton rated for 29kg accepted the 30kg resting on it")
	}
}

// A content that stops short of the lid carries nothing: the lid spans the gap.
func TestLoadPathContentsMustReachTheTop(t *testing.T) {
	build := func(postHeight float64) []BearBox {
		c := nb(0, 0, 0, 2, 1, 2, 1, 5, NoLimit)
		c.Contents = []BearBox{nb(0, 0, 0, 2, 1, postHeight, 1, 1000, NoLimit)}
		return []BearBox{c, nb(0, 0, 2, 2, 1, 1, 50, NoLimit, NoLimit)}
	}
	if !LoadPathOK(build(2)) {
		t.Error("a post reaching the lid should carry the load through")
	}
	if LoadPathOK(build(1.5)) {
		t.Error("a post stopping short of the lid carried load it cannot reach")
	}
}

// Nesting is arbitrarily deep: a carton inside a carton still transmits.
func TestLoadPathNestsDeeply(t *testing.T) {
	inner := nb(0, 0, 0, 1, 1, 2, 1, 1, NoLimit) // weak on its own lid
	inner.Contents = []BearBox{nb(0, 0, 0, 1, 1, 2, 1, 100, NoLimit)}

	outer := nb(0, 0, 0, 1, 1, 2, 1, 1, NoLimit) // also weak
	outer.Contents = []BearBox{inner}

	bs := []BearBox{outer, nb(0, 0, 2, 1, 1, 1, 50, NoLimit, NoLimit)}
	if !LoadPathOK(bs) {
		t.Error("load did not transfer through two levels of flush contents")
	}

	// Weaken the innermost post and it must fail all the way up.
	outer.Contents[0].Contents[0].Limit = 5
	if LoadPathOK([]BearBox{outer, nb(0, 0, 2, 1, 1, 1, 50, NoLimit, NoLimit)}) {
		t.Error("a crushed innermost item was not reported")
	}
}

// Contents bear each other regardless of external load, so a container with
// nothing on it can still be internally infeasible.
func TestLoadPathChecksContentsWithNoExternalLoad(t *testing.T) {
	c := nb(0, 0, 0, 1, 1, 3, 1, NoLimit, NoLimit)
	c.Contents = []BearBox{
		nb(0, 0, 0, 1, 1, 1, 1, 0, NoLimit), // fragile, on the carton floor
		nb(0, 0, 1, 1, 1, 1, 9, NoLimit, NoLimit),
	}
	if LoadPathOK([]BearBox{c}) {
		t.Error("a fragile item crushed by its own neighbours inside the carton was not caught")
	}
}

// Load transmitted through contents still reaches whatever is under the
// container: the decomposition changes which limits are checked, not how much
// load arrives below.
func TestLoadPathTransmitsDownward(t *testing.T) {
	c := nb(0, 0, 1, 1, 1, 2, 1, NoLimit, NoLimit)
	c.Contents = []BearBox{nb(0, 0, 1, 1, 1, 2, 1, NoLimit, NoLimit)}

	base := func(limit float64) BearBox { return nb(0, 0, 0, 1, 1, 1, 1, limit, NoLimit) }
	top := nb(0, 0, 3, 1, 1, 1, 20, NoLimit, NoLimit)

	// Base carries the top box (20) plus the carton and its content (2) = 22.
	if !LoadPathOK([]BearBox{base(22), c, top}) {
		t.Error("base rated for exactly the transmitted load refused it")
	}
	if LoadPathOK([]BearBox{base(21), c, top}) {
		t.Error("load passing through a container did not reach the box below it")
	}
}

// Pressure is resolved per patch through the nesting too: a narrow foot on a
// post is a concentrated load on that post, not a spread one on the carton.
func TestLoadPathPressureThroughContents(t *testing.T) {
	build := func(postPressure float64) []BearBox {
		c := nb(0, 0, 0, 4, 4, 2, 1, NoLimit, NoLimit)
		c.Contents = []BearBox{nb(0, 0, 0, 2, 2, 2, 1, NoLimit, postPressure)}
		// 10kg on a 1x1 foot directly over the post: pressure 10.
		return []BearBox{c, nb(0, 0, 2, 1, 1, 1, 10, NoLimit, NoLimit)}
	}
	if !LoadPathOK(build(10)) {
		t.Error("post rated for pressure 10 refused exactly 10")
	}
	if LoadPathOK(build(9)) {
		t.Error("post rated for pressure 9 accepted a pressure-10 patch")
	}
}

// A container's weight is its own tare plus everything inside it, so the boxes
// beneath carry the whole laden weight. Deriving this rather than asking callers
// to pre-add contents means a forgotten sum cannot silently under-load the stack.
func TestLoadPathLadenWeight(t *testing.T) {
	c := nb(0, 0, 1, 1, 1, 1, 2, NoLimit, NoLimit) // 2kg tare
	c.Contents = []BearBox{
		nb(0, 0, 1, 1, 1, 1, 3, NoLimit, NoLimit),
		nb(0, 0, 1, 1, 1, 1, 5, NoLimit, NoLimit),
	}
	if got := laden(&c); got != 10 {
		t.Errorf("laden = %v, want 10 (2 tare + 3 + 5)", got)
	}

	// The floor box must carry all 10, not just the 2kg tare.
	base := func(limit float64) BearBox { return nb(0, 0, 0, 1, 1, 1, 1, limit, NoLimit) }
	if !LoadPathOK([]BearBox{base(10), c}) {
		t.Error("base rated for the full laden weight refused it")
	}
	if LoadPathOK([]BearBox{base(9), c}) {
		t.Error("contents' weight did not reach the box below the container")
	}
}

// Deeply nested tare sums too.
func TestLoadPathLadenWeightNests(t *testing.T) {
	inner := nb(0, 0, 0, 1, 1, 1, 1, NoLimit, NoLimit)
	inner.Contents = []BearBox{nb(0, 0, 0, 1, 1, 1, 4, NoLimit, NoLimit)}
	outer := nb(0, 0, 0, 1, 1, 1, 2, NoLimit, NoLimit)
	outer.Contents = []BearBox{inner}
	if got := laden(&outer); got != 7 {
		t.Errorf("laden = %v, want 7 (2 + 1 + 4)", got)
	}
}
