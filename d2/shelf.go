package d2

import "math"

const shelfEps = 1e-9

// ShelfFit selects which shelf an incoming item is placed on.
type ShelfFit int

const (
	// ShelfNextFit considers only the most recently opened (top) shelf — the
	// classic NFDH policy.
	ShelfNextFit ShelfFit = iota
	// ShelfFirstFit scans shelves bottom→top and uses the first that fits — FFDH.
	ShelfFirstFit
	// ShelfBestFit uses the fitting shelf that leaves the least leftover height — BFDH.
	ShelfBestFit
)

// Shelf implements shelf (level) packing: items rest on horizontal shelves whose
// height is fixed by the first item placed on each. Paired with a
// decreasing-height pre-sort (offline.DecreasingHeight) this realises the
// classic NFDH / FFDH / BFDH algorithms; the ShelfFit policy chooses which.
type Shelf struct {
	binW, binH float64
	policy     ShelfFit
	shelves    []shelf
	usedArea   float64
}

// shelf is one horizontal level: bottom at y, fixed height, x cursor for the
// next item's left edge.
type shelf struct{ y, height, cursor float64 }

// NewShelf creates a Shelf strategy for a bin of the given dimensions.
func NewShelf(binW, binH float64, policy ShelfFit) *Shelf {
	return &Shelf{binW: binW, binH: binH, policy: policy}
}

// NewShelfStrategy returns a Factory2D-compatible constructor for the policy.
func NewShelfStrategy(policy ShelfFit) func(w, h float64) PlacementStrategy2D {
	return func(w, h float64) PlacementStrategy2D { return NewShelf(w, h, policy) }
}

func (s *Shelf) Utilization() float64 {
	total := s.binW * s.binH
	if total == 0 {
		return 1
	}
	return s.usedArea / total
}

func (s *Shelf) Remaining() float64 { return s.binW*s.binH - s.usedArea }

// topY is the bottom of the next new shelf (the top of the highest one).
func (s *Shelf) topY() float64 {
	if len(s.shelves) == 0 {
		return 0
	}
	last := s.shelves[len(s.shelves)-1]
	return last.y + last.height
}

func (s *Shelf) fits(sh shelf, w, h float64) bool {
	return h <= sh.height+shelfEps && sh.cursor+w <= s.binW+shelfEps
}

type orient struct {
	w, h float64
	rot  bool
}

func orientations(w, h float64, allowRotate bool) []orient {
	o := []orient{{w, h, false}}
	if allowRotate && w != h {
		o = append(o, orient{h, w, true})
	}
	return o
}

func (s *Shelf) TryInsert(w, h float64, allowRotate bool) (x, y float64, rotated bool, ok bool) {
	pick := func(pw, ph float64) int {
		switch s.policy {
		case ShelfNextFit:
			if n := len(s.shelves); n > 0 && s.fits(s.shelves[n-1], pw, ph) {
				return n - 1
			}
			return -1
		case ShelfBestFit:
			best, bestLeft := -1, math.MaxFloat64
			for i, sh := range s.shelves {
				if s.fits(sh, pw, ph) && sh.height-ph < bestLeft {
					best, bestLeft = i, sh.height-ph
				}
			}
			return best
		default: // ShelfFirstFit
			for i, sh := range s.shelves {
				if s.fits(sh, pw, ph) {
					return i
				}
			}
			return -1
		}
	}

	// Prefer placing on an existing shelf (any orientation) before opening one.
	for _, o := range orientations(w, h, allowRotate) {
		if idx := pick(o.w, o.h); idx >= 0 {
			sh := &s.shelves[idx]
			x, y = sh.cursor, sh.y
			sh.cursor += o.w
			s.usedArea += o.w * o.h
			return x, y, o.rot, true
		}
	}
	// Otherwise open a new shelf for whichever orientation fits the remaining height.
	for _, o := range orientations(w, h, allowRotate) {
		ny := s.topY()
		if o.w <= s.binW+shelfEps && ny+o.h <= s.binH+shelfEps {
			s.shelves = append(s.shelves, shelf{y: ny, height: o.h, cursor: o.w})
			s.usedArea += o.w * o.h
			return 0, ny, o.rot, true
		}
	}
	return 0, 0, false, false
}

var _ PlacementStrategy2D = (*Shelf)(nil)
