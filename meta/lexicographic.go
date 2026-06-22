package meta

import (
	"context"
	"errors"
	"math"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// Metric scores a packing result on one objective; smaller is better. Wrap a
// "more is better" objective by negating it.
type Metric struct {
	Name string
	Eval func(pack.Result) float64
}

const lexEpsilon = 1e-9

// Common metrics for composing lexicographic objectives. All are "smaller is
// better" (negate inside Eval for maximisation).
var (
	// BinsUsed minimises the number of bins.
	BinsUsed = Metric{Name: "bins", Eval: func(r pack.Result) float64 { return float64(r.BinsUsed()) }}
	// Unplaced minimises the number of unplaced items.
	Unplaced = Metric{Name: "unplaced", Eval: func(r pack.Result) float64 { return float64(len(r.Unplaced)) }}
)

// UtilizationSpread returns a metric minimising the spread (max−min) of bin
// utilisation, a simple proxy for an even/balanced load across bins.
func UtilizationSpread() Metric {
	return Metric{Name: "util_spread", Eval: func(r pack.Result) float64 {
		if len(r.Bins) == 0 {
			return 0
		}
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, b := range r.Bins {
			u := b.Utilization()
			lo, hi = math.Min(lo, u), math.Max(hi, u)
		}
		return hi - lo
	}}
}

// LexBestOfPacker runs every candidate packer and keeps the result that is best
// under a lexicographic ordering of metrics: candidates are compared on the
// first metric, ties (within epsilon) broken by the second, and so on. This is
// the lexicographic-objective approach used for loading weight- and
// volume-constrained trucks (Bin packing with lexicographic objectives, 2022),
// applied here at the solution-selection level.
type LexBestOfPacker struct {
	metrics    []Metric
	candidates []pack.OfflinePacker
	winner     string
}

// LexBestOf returns a packer choosing the lexicographically best candidate under
// the given metric priority order (highest priority first).
func LexBestOf(metrics []Metric, candidates ...pack.OfflinePacker) *LexBestOfPacker {
	return &LexBestOfPacker{metrics: metrics, candidates: candidates}
}

// Winner returns the Name of the candidate chosen on the most recent PackAll.
func (p *LexBestOfPacker) Winner() string { return p.winner }
func (p *LexBestOfPacker) Name() string   { return "lex" }

func (p *LexBestOfPacker) PackAll(items []pack.Item) (pack.Result, error) {
	return p.PackAllCtx(context.Background(), items)
}

// PackAllCtx runs each candidate (with cancellation if supported) and returns
// the lexicographically best result.
func (p *LexBestOfPacker) PackAllCtx(ctx context.Context, items []pack.Item) (pack.Result, error) {
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
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return pack.Result{}, err
		}
		if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
			continue
		}
		if !found || p.lexLess(r, best) {
			best, found, p.winner = r, true, c.Name()
		}
	}
	if !found {
		return pack.Result{}, pack.ErrItemTooLarge
	}
	return best, nil
}

// lexLess reports whether a is lexicographically better than b under the metrics.
func (p *LexBestOfPacker) lexLess(a, b pack.Result) bool {
	for _, m := range p.metrics {
		va, vb := m.Eval(a), m.Eval(b)
		if math.Abs(va-vb) < lexEpsilon {
			continue
		}
		return va < vb
	}
	return false
}

var (
	_ pack.OfflinePacker    = (*LexBestOfPacker)(nil)
	_ pack.CtxOfflinePacker = (*LexBestOfPacker)(nil)
)
