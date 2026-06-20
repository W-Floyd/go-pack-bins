// Package meta provides meta-algorithms that compose multiple packers.
package meta

import (
	"errors"

	"github.com/wfloyd/go-pack-bins/pack"
)

// Func adapts a bare function into a pack.OfflinePacker. Useful for wrapping
// algorithms like KarmarkarKarp that expose a function rather than a struct.
type Func struct {
	name string
	fn   func([]pack.Item) (pack.Result, error)
}

// NewFunc wraps fn as a named OfflinePacker.
func NewFunc(name string, fn func([]pack.Item) (pack.Result, error)) *Func {
	return &Func{name: name, fn: fn}
}

func (f *Func) PackAll(items []pack.Item) (pack.Result, error) { return f.fn(items) }
func (f *Func) Name() string                                    { return f.name }

var _ pack.OfflinePacker = (*Func)(nil)

// BestOfPacker runs every candidate packer and returns the result with the
// fewest bins used. Ties are broken by fewest unplaced items.
// Candidates that return a non-ErrItemTooLarge error are skipped.
type BestOfPacker struct {
	candidates []pack.OfflinePacker
}

// BestOf returns a packer that tries every candidate and keeps the best result.
func BestOf(candidates ...pack.OfflinePacker) *BestOfPacker {
	return &BestOfPacker{candidates: candidates}
}

func (p *BestOfPacker) Name() string { return "auto" }

func (p *BestOfPacker) PackAll(items []pack.Item) (pack.Result, error) {
	var best pack.Result
	found := false
	for _, c := range p.candidates {
		r, err := c.PackAll(items)
		if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
			continue
		}
		if !found || isBetter(r, best) {
			best = r
			found = true
		}
	}
	if !found {
		return pack.Result{}, pack.ErrItemTooLarge
	}
	if len(best.Unplaced) > 0 {
		return best, pack.ErrItemTooLarge
	}
	return best, nil
}

func isBetter(a, b pack.Result) bool {
	if a.BinsUsed() != b.BinsUsed() {
		return a.BinsUsed() < b.BinsUsed()
	}
	return len(a.Unplaced) < len(b.Unplaced)
}

var _ pack.OfflinePacker = (*BestOfPacker)(nil)
