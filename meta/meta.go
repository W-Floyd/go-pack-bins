// Package meta provides meta-algorithms that compose multiple packers.
package meta

import (
	"context"
	"errors"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// Func adapts a bare function into a pack.OfflinePacker. Useful for wrapping
// algorithms like KarmarkarKarp that expose a function rather than a struct.
// A context-aware function may be supplied via NewFuncCtx for cancellation.
type Func struct {
	name  string
	fn    func([]pack.Item) (pack.Result, error)
	fnCtx func(context.Context, []pack.Item) (pack.Result, error)
}

// NewFunc wraps fn as a named OfflinePacker.
func NewFunc(name string, fn func([]pack.Item) (pack.Result, error)) *Func {
	return &Func{name: name, fn: fn}
}

// NewFuncCtx wraps a context-aware fn as a named CtxOfflinePacker.
func NewFuncCtx(name string, fn func(context.Context, []pack.Item) (pack.Result, error)) *Func {
	return &Func{name: name, fnCtx: fn}
}

func (f *Func) PackAll(items []pack.Item) (pack.Result, error) {
	return f.PackAllCtx(context.Background(), items)
}

func (f *Func) PackAllCtx(ctx context.Context, items []pack.Item) (pack.Result, error) {
	if f.fnCtx != nil {
		return f.fnCtx(ctx, items)
	}
	return f.fn(items)
}
func (f *Func) Name() string { return f.name }

var _ pack.OfflinePacker = (*Func)(nil)
var _ pack.CtxOfflinePacker = (*Func)(nil)

// BestOfPacker runs every candidate packer and returns the result with the
// fewest bins used. Ties are broken by fewest unplaced items.
// Candidates that return a non-ErrItemTooLarge error are skipped.
type BestOfPacker struct {
	candidates []pack.OfflinePacker
	winner     string
}

// BestOf returns a packer that tries every candidate and keeps the best result.
func BestOf(candidates ...pack.OfflinePacker) *BestOfPacker {
	return &BestOfPacker{candidates: candidates}
}

// Winner returns the Name of the candidate that produced the best result on the
// most recent PackAll, or "" if none succeeded.
func (p *BestOfPacker) Winner() string { return p.winner }

func (p *BestOfPacker) Name() string { return "auto" }

func (p *BestOfPacker) PackAll(items []pack.Item) (pack.Result, error) {
	return p.PackAllCtx(context.Background(), items)
}

// PackAllCtx runs each candidate with cancellation: it checks ctx before each
// candidate, passes ctx to any candidate that supports it, and aborts with
// ctx.Err() if cancelled mid-solve.
func (p *BestOfPacker) PackAllCtx(ctx context.Context, items []pack.Item) (pack.Result, error) {
	var best pack.Result
	found := false
	p.winner = ""
	for _, c := range p.candidates {
		if err := ctx.Err(); err != nil {
			return pack.Result{}, err
		}
		var r pack.Result
		var err error
		if cc, ok := c.(pack.CtxOfflinePacker); ok {
			r, err = cc.PackAllCtx(ctx, items)
		} else {
			r, err = c.PackAll(items)
		}
		// A cancellation from within a candidate is terminal for the whole race.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return pack.Result{}, err
		}
		if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
			continue
		}
		if !found || isBetter(r, best) {
			best = r
			found = true
			p.winner = c.Name()
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
var _ pack.CtxOfflinePacker = (*BestOfPacker)(nil)
