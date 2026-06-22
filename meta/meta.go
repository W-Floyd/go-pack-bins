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

// PackAllCtx runs the candidates concurrently (each independent), then reduces in
// candidate order so the winner and tie-break are deterministic regardless of
// scheduling. Each candidate is passed ctx if it supports it; a cancellation from
// within any candidate is terminal for the whole race.
func (p *BestOfPacker) PackAllCtx(ctx context.Context, items []pack.Item) (pack.Result, error) {
	p.winner = ""
	if err := ctx.Err(); err != nil {
		return pack.Result{}, err
	}
	results := make([]pack.Result, len(p.candidates))
	errs := make([]error, len(p.candidates))
	parallelFor(len(p.candidates), func(i int) {
		if cc, ok := p.candidates[i].(pack.CtxOfflinePacker); ok {
			results[i], errs[i] = cc.PackAllCtx(ctx, items)
		} else {
			results[i], errs[i] = p.candidates[i].PackAll(items)
		}
	})
	var best pack.Result
	found := false
	for i, c := range p.candidates {
		// A cancellation from within a candidate is terminal for the whole race.
		if errors.Is(errs[i], context.Canceled) || errors.Is(errs[i], context.DeadlineExceeded) {
			return pack.Result{}, errs[i]
		}
		if errs[i] != nil && !errors.Is(errs[i], pack.ErrItemTooLarge) {
			continue
		}
		if !found || isBetter(results[i], best) {
			best = results[i]
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
