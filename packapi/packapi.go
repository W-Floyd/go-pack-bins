// Package packapi holds the transport-independent packing API shared by the
// HTTP server (cmd/webdemo) and the WebAssembly bridge (cmd/wasm). It takes
// plain request structs and returns plain response structs — no net/http or
// encoding/json — so the same logic runs server-side or in the browser.
package packapi

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/joint"
	"github.com/W-Floyd/go-pack-bins/meta"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// Pack runs the (non-streaming) solve for req with no cancellation.
func Pack(req PackRequest) PackResponse { return PackCtx(context.Background(), req) }

// PackCtx runs the (non-streaming) solve for req and returns the response,
// folding any error (including ctx cancellation) into the response's Error
// field. It is the core of /api/pack; the HTTP handler passes the request
// context so a client disconnect or deadline aborts the solve.
func PackCtx(ctx context.Context, req PackRequest) PackResponse {
	if len(req.Containers) > 0 {
		return solveCatalog(ctx, req)
	}
	var resp PackResponse
	var err error
	switch req.Mode {
	case "1d":
		resp, err = pack1D(ctx, req)
	case "2d":
		resp, err = pack2D(ctx, req)
	case "3d":
		resp, err = pack3D(ctx, req)
	default:
		resp = PackResponse{Error: "unknown mode: " + req.Mode}
	}
	if err != nil {
		resp = PackResponse{Error: err.Error()}
	}
	return resp
}

// packOneBin runs the single-container solve for req's mode (no catalog).
func packOneBin(ctx context.Context, req PackRequest) PackResponse {
	req.Containers = nil
	switch req.Mode {
	case "1d":
		r, err := pack1D(ctx, req)
		return foldErr(r, err)
	case "2d":
		r, err := pack2D(ctx, req)
		return foldErr(r, err)
	case "3d":
		r, err := pack3D(ctx, req)
		return foldErr(r, err)
	}
	return PackResponse{Error: "unknown mode: " + req.Mode}
}

func foldErr(r PackResponse, err error) PackResponse {
	if err != nil {
		return PackResponse{Error: err.Error()}
	}
	return r
}

// binVolume returns one container's capacity in the mode's volume units.
func binVolume(mode string, b BinSpec) float64 {
	switch mode {
	case "1d":
		return b.Width
	case "2d":
		return b.Width * b.Height
	default:
		return b.Width * b.Height * b.Depth
	}
}

func containerLabel(mode string, b BinSpec) string {
	switch mode {
	case "1d":
		return fmt.Sprintf("%g", b.Width)
	case "2d":
		return fmt.Sprintf("%g×%g", b.Width, b.Height)
	default:
		return fmt.Sprintf("%g×%g×%g", b.Width, b.Height, b.Depth)
	}
}

// solveCatalog packs the order against a catalog of container types. It first
// tries each type on its own and, if one type packs the whole order, returns the
// best such single type (tightest — the homogeneous, cleanest result). When no
// single type can place everything (typically because per-type MaxCounts are
// exhausted), it falls back to a sequential cascade that spills the overflow from
// one type into the next, mixing sizes to place as many items as possible.
func solveCatalog(ctx context.Context, req PackRequest) PackResponse {
	single := solveCatalogSingle(ctx, req)
	if single.Error == "" && len(single.Unplaced) == 0 {
		return single // a single type holds the whole order — prefer it
	}
	cascade := solveCatalogCascade(ctx, req)
	if cascade.Error == "" && catalogUnplaced(cascade, req) < catalogUnplaced(single, req) {
		return cascade // mixing sizes placed more items
	}
	return single
}

// catalogUnplaced counts unplaced items, treating an errored response as if the
// whole order was unplaced (so a feasible response always compares better).
func catalogUnplaced(r PackResponse, req PackRequest) int {
	if r.Error != "" {
		return len(req.Items) + 1
	}
	return len(r.Unplaced)
}

// solveCatalogSingle packs the order into each candidate type on its own and
// returns the best single type, ranked by most items placed, then fewest
// containers, then least wasted volume. MaxCount caps a type; items past the cap
// are reported unplaced.
func solveCatalogSingle(ctx context.Context, req PackRequest) PackResponse {
	volByID := itemVolumes(req)

	var best PackResponse
	var bestWaste float64
	found := false
	for _, cs := range req.Containers {
		if err := ctx.Err(); err != nil {
			return PackResponse{Error: err.Error()}
		}
		sub := req
		sub.Bin = cs.Bin
		resp := packOneBin(ctx, sub)
		if resp.Error != "" {
			continue
		}
		resp = truncateCatalog(resp, cs.MaxCount)
		waste := binVolume(req.Mode, cs.Bin)*float64(resp.BinsUsed) - placedVolume(resp, volByID)
		if !found || betterContainer(resp, waste, best, bestWaste) {
			best, bestWaste, found = resp, waste, true
			b := cs.Bin
			best.Container = containerLabel(req.Mode, cs.Bin)
			best.ContainerBin = &b
		}
	}
	if !found {
		return PackResponse{Error: "no container type could pack the order"}
	}
	return best
}

// solveCatalogCascade fills each container type (in the order listed) up to its
// MaxCount, then spills the items that didn't fit into the next type — so an
// exhausted size hands its overflow to the next available size. Bins from all
// types are concatenated with global indices, and BinDims records each bin's
// dimensions so a mix of sizes renders correctly.
func solveCatalogCascade(ctx context.Context, req PackRequest) PackResponse {
	remaining := req.Items
	var out PackResponse
	var dims []BinSpec
	var labels []string
	globalBase := 0

	for _, cs := range req.Containers {
		if len(remaining) == 0 {
			break
		}
		if err := ctx.Err(); err != nil {
			return PackResponse{Error: err.Error()}
		}
		sub := req
		sub.Bin = cs.Bin
		sub.Items = remaining
		sub.Containers = nil
		resp := packOneBin(ctx, sub)
		if resp.Error != "" {
			continue // this type failed; try the next
		}
		resp = truncateCatalog(resp, cs.MaxCount)

		placed := make(map[string]bool, len(resp.Placements))
		for _, p := range resp.Placements {
			p.BinIndex += globalBase // shift local bin index into the global range
			out.Placements = append(out.Placements, p)
			placed[p.ItemID] = true
		}
		if resp.BinsUsed > 0 {
			for i := 0; i < resp.BinsUsed; i++ {
				dims = append(dims, cs.Bin)
			}
			globalBase += resp.BinsUsed
			labels = append(labels, containerLabel(req.Mode, cs.Bin))
		}
		// Items this type couldn't place (over cap or too large) cascade onward.
		var next []ItemSpec
		for _, it := range remaining {
			if !placed[it.ID] {
				next = append(next, it)
			}
		}
		remaining = next
	}

	if globalBase == 0 {
		return PackResponse{Error: "no container type could pack the order"}
	}
	out.BinsUsed = globalBase
	out.BinDims = dims
	out.Container = strings.Join(labels, " + ")
	for _, it := range remaining {
		out.Unplaced = append(out.Unplaced, it.ID)
	}
	return out
}

// truncateCatalog enforces a container count cap by dropping placements in bins
// beyond maxCount; their items become unplaced.
func truncateCatalog(resp PackResponse, maxCount int) PackResponse {
	if maxCount <= 0 || resp.BinsUsed <= maxCount {
		return resp
	}
	kept := resp.Placements[:0:0]
	for _, p := range resp.Placements {
		if p.BinIndex < maxCount {
			kept = append(kept, p)
		} else {
			resp.Unplaced = append(resp.Unplaced, p.ItemID)
		}
	}
	resp.Placements = kept
	resp.BinsUsed = maxCount
	return resp
}

func itemVolumes(req PackRequest) map[string]float64 {
	m := make(map[string]float64, len(req.Items))
	for _, it := range req.Items {
		switch req.Mode {
		case "1d":
			m[it.ID] = it.Width
		case "2d":
			m[it.ID] = it.Width * it.Height
		default:
			m[it.ID] = it.Width * it.Height * it.Depth
		}
	}
	return m
}

func placedVolume(resp PackResponse, volByID map[string]float64) float64 {
	v := 0.0
	for _, p := range resp.Placements {
		v += volByID[p.ItemID]
	}
	return v
}

func betterContainer(a PackResponse, aWaste float64, b PackResponse, bWaste float64) bool {
	if len(a.Unplaced) != len(b.Unplaced) {
		return len(a.Unplaced) < len(b.Unplaced)
	}
	if a.BinsUsed != b.BinsUsed {
		return a.BinsUsed < b.BinsUsed
	}
	return aWaste < bWaste
}

// PackNested runs the two-level nested solve (cartons → pallets) with no
// cancellation. It is the core of /api/pack/nested.
func PackNested(req NestedPackRequest) (NestedPackResponse, error) {
	return PackNestedCtx(context.Background(), req)
}

// PackNestedCtx is PackNested with cancellation threaded into both levels.
func PackNestedCtx(ctx context.Context, req NestedPackRequest) (NestedPackResponse, error) {
	return doNestedPack(ctx, req)
}

// ─── cancellation helpers ──────────────────────────────────────────────────

// runOnline drives an online packer over items, checking ctx before each Pack
// so a long single-pass solve can be cancelled. ErrItemTooLarge is returned to
// the caller (which tolerates it); ctx cancellation returns ctx.Err().
func runOnline(ctx context.Context, p pack.OnlinePacker, items []pack.Item) (pack.Result, error) {
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return p.Result(), err
		}
		if _, e := p.Pack(it); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return p.Result(), e
		}
	}
	return p.Result(), nil
}

// packAllCtx runs an offline packer with cancellation if it supports it,
// otherwise falls back to the plain PackAll.
func packAllCtx(ctx context.Context, p pack.OfflinePacker, items []pack.Item) (pack.Result, error) {
	if c, ok := p.(pack.CtxOfflinePacker); ok {
		return c.PackAllCtx(ctx, items)
	}
	return p.PackAll(items)
}

// ─── request / response types ─────────────────────────────────────────────────

type ItemSpec struct {
	ID          string             `json:"id"`
	Width       float64            `json:"width"`
	Height      float64            `json:"height"`
	Depth       float64            `json:"depth"`
	AllowRotate bool               `json:"allow_rotate"`
	Scalars     map[string]float64 `json:"scalars,omitempty"`
}

type BinSpec struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Depth  float64 `json:"depth"`
}

// ConstraintSpec is a hard constraint on the bin's accumulated scalar totals.
type ConstraintSpec struct {
	Scalar string  `json:"scalar"` // name of the scalar (or category, for "incompatible")
	Op     string  `json:"op"`     // "max", "min", "allsame", "incompatible"
	Value  float64 `json:"value"`
	// Value2 is the second category for op "incompatible": items whose Scalar
	// equals Value and items whose Scalar equals Value2 may not share a bin.
	Value2 float64 `json:"value2,omitempty"`
}

// PreferenceSpec is a soft balancing objective on a scalar. When any are
// present the packer switches to PreferenceFit and the algorithm dropdown is
// ignored (preferences are an online-selection concept).
type PreferenceSpec struct {
	Scalar string  `json:"scalar"`           // name of the scalar to balance
	Mode   string  `json:"mode"`             // "concentrate" (fill fullest first) or "balance" (even totals)
	Weight float64 `json:"weight,omitempty"` // relative pull; defaults to 1
}

type PackRequest struct {
	Mode        string           `json:"mode"`      // "1d", "2d", "3d"
	Algorithm   string           `json:"algorithm"` // "ff", "bf", "wf", "nf", "nkf", "awf", "ffd", "bfd", "nfd", "wfd", "mffd", "kk", "bc", "hk", "rff"
	Bin         BinSpec          `json:"bin"`
	Items       []ItemSpec       `json:"items"`
	Constraints []ConstraintSpec `json:"constraints,omitempty"`
	Preferences []PreferenceSpec `json:"preferences,omitempty"`
	Contact     ContactSpec      `json:"contact,omitempty"` // per-face support/anti-slosh
	// Containers, when non-empty, switches to container-catalog mode: the order is
	// packed into each candidate container type and the best single type is chosen
	// (see package catalog). Bin is ignored in this mode.
	Containers []ContainerSpec `json:"containers,omitempty"`
}

// ContainerSpec is one container type in a catalog: its size and an optional cap
// on how many of that type may be used (0 = unlimited).
type ContainerSpec struct {
	Bin      BinSpec `json:"bin"`
	MaxCount int     `json:"max_count,omitempty"`
}

// ContactSpec is the per-face contact requirement (fractions 0-1). Bottom is a
// hard support gate (3-D); SideX/SideY are lateral anti-slosh targets that drive
// contact-maximizing placement and lateral compaction.
type ContactSpec struct {
	Bottom     float64 `json:"bottom,omitempty"`
	SideX      float64 `json:"side_x,omitempty"`
	SideY      float64 `json:"side_y,omitempty"`
	NoFloating bool    `json:"no_floating,omitempty"` // every item must rest on floor/box (3-D)
}

// lateralAxes reports which lateral axes have an anti-slosh target (and whether any do).
func (c ContactSpec) lateralAxes() (doX, doY, any bool) {
	return c.SideX > 0, c.SideY > 0, c.SideX > 0 || c.SideY > 0
}

type PlacementResult struct {
	BinIndex int     `json:"bin_index"`
	ItemID   string  `json:"item_id"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
	W        float64 `json:"w"`
	H        float64 `json:"h"`
	D        float64 `json:"d"`
	Rotated  bool    `json:"rotated"`
}

// FreeRect is a free rectangle produced by the guillotine algorithm: [x, y, w, h].
type FreeRect [4]float64

type PackResponse struct {
	BinsUsed   int               `json:"bins_used"`
	Placements []PlacementResult `json:"placements"`
	Unplaced   []string          `json:"unplaced"`
	FreeRects  [][]FreeRect      `json:"free_rects,omitempty"` // per-bin, guillotine only
	ItemErrors map[string]string `json:"item_errors,omitempty"`
	BestPacker string            `json:"best_packer,omitempty"` // winning algorithm name (auto mode)
	Error      string            `json:"error,omitempty"`
	// Container/ContainerBin are set in catalog mode: the chosen container type's
	// label and its dimensions (so the client renders with the right box size).
	Container    string   `json:"container,omitempty"`
	ContainerBin *BinSpec `json:"container_bin,omitempty"`
	// BinDims is set when catalog mode mixes container types: one entry per bin
	// index giving that bin's dimensions, so the client can render bins of
	// differing sizes. Empty when all bins are the same type (use ContainerBin).
	BinDims []BinSpec `json:"bin_dims,omitempty"`
}

// ─── streaming ──────────────────────────────────────────────────────────────

// streamBatch is the placement-batch chunk size. Placements are emitted in
// solve order so the client can render them progressively as they "appear".
const streamBatch = 64

// StreamFrame is one NDJSON line on the /api/pack/stream response. Frames arrive
// in order, keyed by Type:
//   - "meta":  sent once up front. Count is the item count (for label budgeting);
//     Streaming reports whether placements will arrive genuinely mid-solve (true)
//     or all at once after the solve completes (false, for non-incremental algos).
//   - "batch": a slice of placements in commit order. For streaming algos these
//     are flushed as the packer decides them; otherwise sent after the full solve.
//   - "done":  terminal; carries the authoritative final summary.
//   - "error": fatal error; terminal.
type StreamFrame struct {
	Type       string            `json:"type"`
	Streaming  bool              `json:"streaming,omitempty"` // meta: genuine mid-solve emission?
	Count      int               `json:"count,omitempty"`     // meta: item count
	Multi      bool              `json:"multi,omitempty"`     // meta: segmented multi-candidate race (auto)
	Segments   []string          `json:"segments,omitempty"`  // meta: candidate labels, one per segment
	Seg        int               `json:"seg,omitempty"`       // batch: segment this batch belongs to (0 default)
	WinnerSeg  *int              `json:"winner_seg,omitempty"` // done: winning segment index (multi)
	BinsUsed   int               `json:"bins_used,omitempty"` // done
	BestPacker string            `json:"best_packer,omitempty"`
	FreeRects  [][]FreeRect      `json:"free_rects,omitempty"`
	ItemErrors map[string]string `json:"item_errors,omitempty"`
	Unplaced     []string          `json:"unplaced,omitempty"`
	Placements   []PlacementResult `json:"placements,omitempty"`
	Error        string            `json:"error,omitempty"`
	Container    string            `json:"container,omitempty"`     // done: chosen container (catalog)
	ContainerBin *BinSpec          `json:"container_bin,omitempty"` // done: chosen container dims (catalog)
	BinDims      []BinSpec         `json:"bin_dims,omitempty"`      // done: per-bin dims (mixed catalog)
}

// StreamPack runs the same packing as Pack but delivers the result as a series
// of StreamFrames via the send callback: one "meta" frame, then placement
// "batch" frames in solve order, then "done". The solve runs to completion
// synchronously (it is fast); send paces delivery. The HTTP handler wraps send
// around a flushed NDJSON encoder; other front-ends (e.g. WASM) supply their own.
// send is only ever called from the calling goroutine, so it need not be
// concurrency-safe. The frame protocol leaves room for a genuinely-incremental
// solver to emit batches mid-solve without any client change.
func StreamPack(ctx context.Context, req PackRequest, send func(StreamFrame)) {
	// A buffered emitter coalesces placements into batches so we are not flushing
	// one tiny frame per box, while still pushing each batch the moment it fills.
	var buf []PlacementResult
	flushBatch := func() {
		if len(buf) > 0 {
			send(StreamFrame{Type: "batch", Placements: buf})
			buf = nil
		}
	}
	emit := func(pr PlacementResult) {
		buf = append(buf, pr)
		if len(buf) >= streamBatch {
			flushBatch()
		}
	}

	// Catalog mode runs a full solve per container type — no honest partial state —
	// so solve fully and emit the result at once, carrying the chosen container.
	if len(req.Containers) > 0 {
		resp := solveCatalog(ctx, req)
		if resp.Error != "" {
			send(StreamFrame{Type: "error", Error: resp.Error})
			return
		}
		send(StreamFrame{Type: "meta", Count: len(req.Items), Streaming: false})
		for _, pr := range resp.Placements {
			emit(pr)
		}
		flushBatch()
		send(StreamFrame{Type: "done", BinsUsed: resp.BinsUsed, Unplaced: resp.Unplaced,
			ItemErrors: resp.ItemErrors, Container: resp.Container, ContainerBin: resp.ContainerBin,
			BinDims: resp.BinDims})
		return
	}

	// Auto: race every candidate at once into its own segment, each streaming its
	// own genuine solve, then collapse to the winner. Skipped when a relocating
	// post-pass is active (then there is no honest partial state to show).
	if cands := autoCandidates(ctx, req); cands != nil {
		streamAuto(ctx, send, req, cands)
		return
	}

	streaming := isStreamable(req)
	send(StreamFrame{Type: "meta", Count: len(req.Items), Streaming: streaming})

	// Genuine path: the observer fires as the packer commits each placement, so
	// batches leave mid-solve. The solve runs synchronously here; flushing per
	// batch is what makes the bytes genuinely progressive.
	if streaming {
		if resp, ok := streamSolve(ctx, req, emit); ok {
			flushBatch()
			send(StreamFrame{Type: "done", BinsUsed: resp.BinsUsed,
				Unplaced: resp.Unplaced, ItemErrors: resp.ItemErrors})
			return
		}
	}

	// Non-incremental algorithms (auto, exact solvers, balancing, compaction):
	// no honest partial state exists, so solve fully and send the result at once.
	var resp PackResponse
	var err error
	switch req.Mode {
	case "1d":
		resp, err = pack1D(ctx, req)
	case "2d":
		resp, err = pack2D(ctx, req)
	case "3d":
		resp, err = pack3D(ctx, req)
	default:
		resp = PackResponse{Error: "unknown mode: " + req.Mode}
	}
	if err != nil {
		resp = PackResponse{Error: err.Error()}
	}
	if resp.Error != "" {
		send(StreamFrame{Type: "error", Error: resp.Error})
		return
	}
	for i := 0; i < len(resp.Placements); i += streamBatch {
		j := i + streamBatch
		if j > len(resp.Placements) {
			j = len(resp.Placements)
		}
		send(StreamFrame{Type: "batch", Placements: resp.Placements[i:j]})
	}
	send(StreamFrame{Type: "done", BinsUsed: resp.BinsUsed, BestPacker: resp.BestPacker,
		FreeRects: resp.FreeRects, Unplaced: resp.Unplaced, ItemErrors: resp.ItemErrors})
}

// isStreamable reports whether req's solve is a single sequential pass that can
// honestly emit placements as it commits them. It excludes:
//   - balancing objectives, which run a post-pass (RefineBalance) that relocates
//     already-committed items;
//   - lateral compaction (any side contact target), which slides items after
//     placement;
//   - algorithms with no incremental commit point: auto (BestOf), exact solvers
//     (bc, kk), the multi-phase mffd, the self-managed harmonic/RFF variants, and
//     2-D guillotine (its free-rect overlay is only meaningful when complete).
func isStreamable(req PackRequest) bool {
	// JointFit commits each placement once, in final position, with no post-pass
	// (it handles balance and anti-slosh during placement), so it always streams.
	if req.Mode == "3d" && req.Algorithm == "joint" {
		return true
	}
	if prefs, _ := buildPreferences(req.Preferences); isBalanceable(req.Algorithm) && (len(prefs) > 0 || req.Algorithm == "pref") {
		return false
	}
	if _, _, any := req.Contact.lateralAxes(); any {
		return false
	}
	switch req.Mode {
	case "1d":
		switch req.Algorithm {
		case "", "ff", "nf", "nkf", "bf", "wf", "awf", "ss", "ffd", "bfd", "nfd", "wfd", "mffd":
			return true
		}
	case "2d":
		switch req.Algorithm {
		case "", "ff", "maxrects", "skyline", "nf", "bf", "wf", "ffd", "bfd", "nfd", "nfdh", "ffdh", "bfdh":
			return true
		}
	case "3d":
		switch req.Algorithm {
		case "", "ff", "blf", "nf", "bf", "wf", "ffd", "bfd", "nfd":
			return true
		}
	}
	return false
}

// placeConv turns committed pack.Placements into PlacementResults for the stream,
// assigning bin indices in first-seen order — which equals bin-opening order, so
// they match the non-streaming buildResponseND. For 1-D it accumulates per-bin
// x-offsets exactly as buildResponse1D does.
type placeConv struct {
	mode     string
	binIdx   map[string]int
	next     int
	offsets  map[string]float64
	sizeByID map[string]float64
	binW     float64
}

func (c *placeConv) idx(binID string) int {
	if i, ok := c.binIdx[binID]; ok {
		return i
	}
	i := c.next
	c.binIdx[binID] = i
	c.next++
	return i
}

func (c *placeConv) conv(p pack.Placement) (PlacementResult, bool) {
	switch c.mode {
	case "1d":
		pl, ok := p.(*d1.Placement1D)
		if !ok {
			return PlacementResult{}, false
		}
		bi := c.idx(pl.BinID())
		sz := c.sizeByID[pl.ItemID()]
		x := c.offsets[pl.BinID()]
		c.offsets[pl.BinID()] += sz
		return PlacementResult{BinIndex: bi, ItemID: pl.ItemID(), X: x, W: sz, H: c.binW}, true
	case "2d":
		pl, ok := p.(*d2.Placement2D)
		if !ok {
			return PlacementResult{}, false
		}
		return PlacementResult{BinIndex: c.idx(pl.BinID()), ItemID: pl.ItemID(),
			X: pl.X, Y: pl.Y, W: pl.W, H: pl.H, Rotated: pl.Rotated}, true
	case "3d":
		pl, ok := p.(*d3.Placement3D)
		if !ok {
			return PlacementResult{}, false
		}
		return PlacementResult{BinIndex: c.idx(pl.BinID()), ItemID: pl.ItemID(),
			X: pl.X, Y: pl.Y, Z: pl.Z, W: pl.W, H: pl.H, D: pl.D}, true
	}
	return PlacementResult{}, false
}

// streamSolve builds the same factory, items and packer the non-streaming path
// would, attaches an observer that converts and emits each placement as the
// packer commits it, then runs the solve. It returns the final summary and true.
// Returns ok=false only if the algorithm is not a recognised sequential packer
// (the caller then falls back to the full solve). Precondition: isStreamable(req).
func streamSolve(ctx context.Context, req PackRequest, emit func(PlacementResult)) (PackResponse, bool) {
	conv := &placeConv{
		mode: req.Mode, binIdx: map[string]int{}, offsets: map[string]float64{},
		sizeByID: map[string]float64{}, binW: req.Bin.Width,
	}
	for _, s := range req.Items {
		conv.sizeByID[s.ID] = s.Width
	}
	observer := func(p pack.Placement) {
		if pr, ok := conv.conv(p); ok {
			emit(pr)
		}
	}

	var factory pack.BinFactory
	var items []pack.Item
	switch req.Mode {
	case "1d":
		factory = constrainedFactory(d1.NewFactory(req.Bin.Width), req.Constraints)
		for _, spec := range req.Items {
			it := d1.NewItem(spec.ID, spec.Width)
			for k, v := range spec.Scalars {
				it.WithScalar(k, v)
			}
			items = append(items, it)
		}
	case "2d":
		// Strategy follows the algorithm (MaxRects / Skyline / Shelf); Guillotine is
		// excluded by isStreamable because of its free-rect overlay.
		factory = constrainedFactory(d2.NewFactory(req.Bin.Width, req.Bin.Height, strat2DFor(req.Algorithm)), req.Constraints)
		for _, spec := range req.Items {
			it := d2.NewItem(spec.ID, spec.Width, spec.Height, spec.AllowRotate)
			for k, v := range spec.Scalars {
				it.WithScalar(k, v)
			}
			items = append(items, it)
		}
	case "3d":
		// BLF is its own strategy; otherwise extreme-point with placement-time
		// gates only (lateral compaction is ruled out by isStreamable), so streamed
		// positions are final.
		stratFn := d3.NewExtremePointStrategyContact(d3.ContactSpec{
			Bottom: req.Contact.Bottom, NoFloating: req.Contact.NoFloating,
		})
		if req.Algorithm == "blf" {
			stratFn = d3.NewBottomLeftFillStrategy
		}
		factory = constrainedFactory(d3.NewFactory(req.Bin.Width, req.Bin.Depth, req.Bin.Height, stratFn), req.Constraints)
		for _, spec := range req.Items {
			it := d3.NewItem(spec.ID, spec.Width, spec.Depth, spec.Height, spec.AllowRotate)
			for k, v := range spec.Scalars {
				it.WithScalar(k, v)
			}
			items = append(items, it)
		}
	default:
		return PackResponse{}, false
	}

	obs, run, ok := buildStreamPacker(ctx, req, factory)
	if !ok {
		return PackResponse{}, false
	}
	obs.Observe(observer)
	result := run(items)
	return PackResponse{
		BinsUsed:   result.BinsUsed(),
		Unplaced:   result.Unplaced,
		ItemErrors: placementErrors(result.PlacementErrors),
	}, true
}

// buildStreamPacker returns the observable packer for req's (streamable) algorithm
// and a closure that runs it over the items, returning the final result. Online
// algorithms loop Pack; the decreasing offline wrappers (sort-then-online) run
// PackAll — both commit through online.Packer, so the observer fires either way.
func buildStreamPacker(ctx context.Context, req PackRequest, factory pack.BinFactory) (pack.Observable, func([]pack.Item) pack.Result, bool) {
	online1 := func(op *online.Packer) (pack.Observable, func([]pack.Item) pack.Result, bool) {
		return op, func(items []pack.Item) pack.Result {
			for _, it := range items {
				if ctx.Err() != nil { // cancelled — stop committing further placements
					break
				}
				op.Pack(it) // failures are recorded in the result (unplaced / errors)
			}
			return op.Result()
		}, true
	}
	wrap := func(w *offline.Wrapper) (pack.Observable, func([]pack.Item) pack.Result, bool) {
		return w, func(items []pack.Item) pack.Result {
			r, _ := w.PackAllCtx(ctx, items)
			return r
		}, true
	}
	switch req.Algorithm {
	case "nf":
		return online1(online.NextFit(factory))
	case "bf":
		return online1(online.BestFit(factory))
	case "wf":
		return online1(online.WorstFit(factory))
	case "nkf":
		return online1(online.NextKFit(3, factory))
	case "awf":
		return online1(online.AlmostWorstFit(factory))
	case "joint": // 3-D joint multi-objective; manages its own bins, ignores factory
		prefs, weights := buildPreferences(req.Preferences)
		jf := joint.New(req.Bin.Width, req.Bin.Depth, req.Bin.Height, d3.ContactSpec{
			Bottom: req.Contact.Bottom, SideX: req.Contact.SideX, SideY: req.Contact.SideY,
			NoFloating: req.Contact.NoFloating,
		}, prefs, weights, buildConstraints(req.Constraints))
		return jf, func(items []pack.Item) pack.Result { r, _ := jf.PackAllCtx(ctx, items); return r }, true
	case "ss":
		return online1(online.SumOfSquares(req.Bin.Width, factory))
	case "nfdh", "ffdh", "bfdh": // shelf factory + decreasing-height sort
		return wrap(offline.New(shelfLabel[req.Algorithm], offline.DecreasingHeight, online.FirstFit(factory)))
	case "ffd":
		return wrap(offline.FirstFitDecreasing(factory))
	case "bfd":
		return wrap(offline.BestFitDecreasing(factory))
	case "nfd":
		return wrap(offline.NextFitDecreasing(factory))
	case "wfd":
		return wrap(offline.WorstFitDecreasing(factory))
	case "mffd": // 1-D only; a single First-Fit pass over class-ordered items
		mp := offline.ModifiedFirstFitDecreasing(req.Bin.Width, factory)
		return mp, func(items []pack.Item) pack.Result { r, _ := packAllCtx(ctx, mp, items); return r }, true
	default: // "", "ff", 2-D "maxrects"
		return online1(online.FirstFit(factory))
	}
}

// ─── auto: segmented multi-candidate race ──────────────────────────────────────

// candidate is one packer in an auto race. obs is non-nil when the packer can
// stream each placement as it commits (FFD/BFD/NFD/WFD); otherwise (KK, MFFD,
// guillotine wrappers we treat opaquely) run produces the full result and its
// placements are emitted at the end. Each candidate owns its factory and a fresh
// items slice so the candidates can run concurrently with no shared state.
type candidate struct {
	label string
	obs   pack.Observable
	run   func() (pack.Result, error)
}

// autoCandidates returns the BestOf candidate set for req, mirroring the auto
// branch of pack1D/2D/3D, or nil if a segmented race does not apply: not "auto",
// a balancing post-pass is active (auto + preferences → BalancedFit, not BestOf),
// or lateral compaction would relocate committed items.
func autoCandidates(ctx context.Context, req PackRequest) []candidate {
	if req.Algorithm != "auto" {
		return nil
	}
	if prefs, _ := buildPreferences(req.Preferences); len(prefs) > 0 {
		return nil
	}
	if _, _, any := req.Contact.lateralAxes(); any {
		return nil
	}

	wrap := func(label string, w *offline.Wrapper, items []pack.Item) candidate {
		return candidate{label: label, obs: w, run: func() (pack.Result, error) { return w.PackAllCtx(ctx, items) }}
	}
	switch req.Mode {
	case "1d":
		cap := req.Bin.Width
		f := func() pack.BinFactory { return constrainedFactory(d1.NewFactory(cap), req.Constraints) }
		mffd := offline.ModifiedFirstFitDecreasing(cap, f())
		mffdItems := items1D(req)
		return []candidate{
			wrap("FFD", offline.FirstFitDecreasing(f()), items1D(req)),
			wrap("BFD", offline.BestFitDecreasing(f()), items1D(req)),
			wrap("WFD", offline.WorstFitDecreasing(f()), items1D(req)),
			{label: "MFFD", obs: mffd, run: func() (pack.Result, error) { return packAllCtx(ctx, mffd, mffdItems) }},
			{label: "KK", run: func() (pack.Result, error) {
				return offline.KarmarkarKarpCtx(ctx, items1D(req), cap, d1.NewFactory(cap))
			}},
		}
	case "2d":
		mr := func() pack.BinFactory {
			return constrainedFactory(d2.NewFactory(req.Bin.Width, req.Bin.Height, d2.NewMaxRectsDefault), req.Constraints)
		}
		g := func() pack.BinFactory {
			return constrainedFactory(d2.NewFactory(req.Bin.Width, req.Bin.Height, d2.NewGuillotineDefault), req.Constraints)
		}
		sky := func() pack.BinFactory {
			return constrainedFactory(d2.NewFactory(req.Bin.Width, req.Bin.Height, d2.NewSkylineDefault), req.Constraints)
		}
		return []candidate{
			wrap("FFD", offline.FirstFitDecreasing(mr()), items2D(req)),
			wrap("BFD", offline.BestFitDecreasing(mr()), items2D(req)),
			wrap("NFD", offline.NextFitDecreasing(mr()), items2D(req)),
			wrap("FFD·guillotine", offline.FirstFitDecreasing(g()), items2D(req)),
			wrap("BFD·guillotine", offline.BestFitDecreasing(g()), items2D(req)),
			wrap("FFD·skyline", offline.FirstFitDecreasing(sky()), items2D(req)),
		}
	case "3d":
		f := func() pack.BinFactory {
			stratFn := d3.NewExtremePointStrategyContact(d3.ContactSpec{Bottom: req.Contact.Bottom, NoFloating: req.Contact.NoFloating})
			return constrainedFactory(d3.NewFactory(req.Bin.Width, req.Bin.Depth, req.Bin.Height, stratFn), req.Constraints)
		}
		blf := func() pack.BinFactory {
			return constrainedFactory(d3.NewFactory(req.Bin.Width, req.Bin.Depth, req.Bin.Height, d3.NewBottomLeftFillStrategy), req.Constraints)
		}
		return []candidate{
			wrap("FFD", offline.FirstFitDecreasing(f()), items3D(req)),
			wrap("BFD", offline.BestFitDecreasing(f()), items3D(req)),
			wrap("NFD", offline.NextFitDecreasing(f()), items3D(req)),
			wrap("BLF", offline.FirstFitDecreasing(blf()), items3D(req)),
		}
	}
	return nil
}

// items1D/2D/3D build a fresh items slice for one candidate (so concurrent
// candidates never share an item slice that a packer might sort in place).
func items1D(req PackRequest) []pack.Item {
	out := make([]pack.Item, 0, len(req.Items))
	for _, s := range req.Items {
		it := d1.NewItem(s.ID, s.Width)
		for k, v := range s.Scalars {
			it.WithScalar(k, v)
		}
		out = append(out, it)
	}
	return out
}

func items2D(req PackRequest) []pack.Item {
	out := make([]pack.Item, 0, len(req.Items))
	for _, s := range req.Items {
		it := d2.NewItem(s.ID, s.Width, s.Height, s.AllowRotate)
		for k, v := range s.Scalars {
			it.WithScalar(k, v)
		}
		out = append(out, it)
	}
	return out
}

func items3D(req PackRequest) []pack.Item {
	out := make([]pack.Item, 0, len(req.Items))
	for _, s := range req.Items {
		it := d3.NewItem(s.ID, s.Width, s.Depth, s.Height, s.AllowRotate)
		for k, v := range s.Scalars {
			it.WithScalar(k, v)
		}
		out = append(out, it)
	}
	return out
}

// streamAuto runs every candidate concurrently, interleaving their genuine
// per-commit placement streams into per-segment batches, then sends a done frame
// naming the winning segment (fewest bins, ties by fewest unplaced — matching
// meta.BestOf). Candidates that cannot stream emit their placements once solved.
func streamAuto(ctx context.Context, send func(StreamFrame), req PackRequest, cands []candidate) {
	_ = ctx // candidates already capture ctx in their run closures (see autoCandidates)
	labels := make([]string, len(cands))
	for i, c := range cands {
		labels[i] = c.label
	}
	send(StreamFrame{Type: "meta", Count: len(req.Items), Streaming: true, Multi: true, Segments: labels})

	type segBatch struct {
		seg int
		pls []PlacementResult
	}
	ch := make(chan segBatch, 64)
	results := make([]pack.Result, len(cands))
	errs := make([]error, len(cands))

	var wg sync.WaitGroup
	for i, c := range cands {
		wg.Add(1)
		go func(i int, c candidate) {
			defer wg.Done()
			conv := &placeConv{mode: req.Mode, binIdx: map[string]int{}, offsets: map[string]float64{},
				sizeByID: map[string]float64{}, binW: req.Bin.Width}
			for _, s := range req.Items {
				conv.sizeByID[s.ID] = s.Width
			}
			var buf []PlacementResult
			flush := func() {
				if len(buf) > 0 {
					ch <- segBatch{seg: i, pls: buf}
					buf = nil
				}
			}
			emit := func(p pack.Placement) {
				if pr, ok := conv.conv(p); ok {
					buf = append(buf, pr)
					if len(buf) >= streamBatch {
						flush()
					}
				}
			}
			if c.obs != nil {
				c.obs.Observe(emit)
				results[i], errs[i] = c.run()
			} else {
				results[i], errs[i] = c.run()
				for _, p := range results[i].Placements {
					if p != nil {
						emit(p)
					}
				}
			}
			flush()
		}(i, c)
	}
	go func() { wg.Wait(); close(ch) }()

	for sb := range ch {
		send(StreamFrame{Type: "batch", Seg: sb.seg, Placements: sb.pls})
	}

	// Winner: fewest bins, ties by fewest unplaced, over candidates that did not
	// fail with a non-ErrItemTooLarge error (mirrors meta.BestOfPacker.PackAll).
	winner := -1
	for i := range cands {
		if errs[i] != nil && !errors.Is(errs[i], pack.ErrItemTooLarge) {
			continue
		}
		if winner < 0 || isBetterResult(results[i], results[winner]) {
			winner = i
		}
	}
	done := StreamFrame{Type: "done"}
	if winner >= 0 {
		w := winner
		done.WinnerSeg = &w
		done.BinsUsed = results[winner].BinsUsed()
		done.BestPacker = cands[winner].label
		done.Unplaced = results[winner].Unplaced
		done.ItemErrors = placementErrors(results[winner].PlacementErrors)
	}
	send(done)
}

// isBetterResult is meta.isBetter: fewer bins wins; ties break on fewer unplaced.
func isBetterResult(a, b pack.Result) bool {
	if a.BinsUsed() != b.BinsUsed() {
		return a.BinsUsed() < b.BinsUsed()
	}
	return len(a.Unplaced) < len(b.Unplaced)
}

// ─── 1-D ─────────────────────────────────────────────────────────────────────

func pack1D(ctx context.Context, req PackRequest) (PackResponse, error) {
	cap := req.Bin.Width
	factory := constrainedFactory(d1.NewFactory(cap), req.Constraints)

	items := make([]pack.Item, len(req.Items))
	for i, spec := range req.Items {
		it := d1.NewItem(spec.ID, spec.Width)
		for k, v := range spec.Scalars {
			it.WithScalar(k, v)
		}
		items[i] = it
	}

	// Balance objectives layer on any balanceable algorithm (bf/wf/pref/auto).
	if prefs, weights := buildPreferences(req.Preferences); isBalanceable(req.Algorithm) && (len(prefs) > 0 || req.Algorithm == "pref") {
		result, best, perr := runBalanced(ctx, req.Algorithm, factory, prefs, weights, items)
		if perr != nil && !errors.Is(perr, pack.ErrItemTooLarge) {
			return PackResponse{Error: perr.Error()}, nil
		}
		sizeByID := make(map[string]float64, len(req.Items))
		for _, spec := range req.Items {
			sizeByID[spec.ID] = spec.Width
		}
		resp := buildResponse1D(result, req.Bin.Width, sizeByID)
		resp.BestPacker = best
		return resp, nil
	}

	var result pack.Result
	var err error
	var bestPacker string

	switch req.Algorithm {
	case "nf":
		p := online.NextFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "nkf":
		p := online.NextKFit(3, factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "bf":
		p := online.BestFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "wf":
		p := online.WorstFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "awf":
		p := online.AlmostWorstFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "rff":
		p := online.NewRFF(cap, factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "hk":
		p := online.NewHarmonicK(11, cap, factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "ss":
		p := online.SumOfSquares(cap, factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "ffd":
		p := offline.FirstFitDecreasing(factory)
		result, err = packAllCtx(ctx, p, items)
	case "bfd":
		p := offline.BestFitDecreasing(factory)
		result, err = packAllCtx(ctx, p, items)
	case "nfd":
		p := offline.NextFitDecreasing(factory)
		result, err = packAllCtx(ctx, p, items)
	case "wfd":
		p := offline.WorstFitDecreasing(factory)
		result, err = packAllCtx(ctx, p, items)
	case "mffd":
		p := offline.ModifiedFirstFitDecreasing(cap, factory)
		result, err = packAllCtx(ctx, p, items)
	case "kk":
		result, err = offline.KarmarkarKarpCtx(ctx, items, cap, factory)
	case "bc":
		result, err = offline.BinCompletionCtx(ctx, items, cap, d1.NewFactory(cap), buildConstraints(req.Constraints)...)
	case "brute":
		result, err = offline.BruteForce(ctx, items, factory, offline.BruteForceOptions{Key: shapeKey1D})
	case "beam":
		result = offline.BeamSearch(ctx, items, factory, offline.BeamOptions{})
	case "rr":
		result = offline.RuinRecreate(ctx, items, factory, offline.SearchOptions{})
	case "grasp":
		result = offline.GRASP(ctx, items, factory, offline.SearchOptions{})
	case "auto":
		p := meta.BestOf(
			offline.FirstFitDecreasing(factory),
			offline.BestFitDecreasing(factory),
			offline.WorstFitDecreasing(factory),
			offline.ModifiedFirstFitDecreasing(cap, factory),
			meta.NewFuncCtx("kk", func(ctx context.Context, it []pack.Item) (pack.Result, error) {
				return offline.KarmarkarKarpCtx(ctx, it, cap, d1.NewFactory(cap))
			}),
		)
		result, err = packAllCtx(ctx, p, items)
		bestPacker = p.Winner()
	default:
		p := online.FirstFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	}

	if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
		return PackResponse{Error: err.Error()}, nil
	}

	// Build a lookup from item ID → size for offset tracking.
	sizeByID := make(map[string]float64, len(req.Items))
	for _, spec := range req.Items {
		sizeByID[spec.ID] = spec.Width
	}
	resp := buildResponse1D(result, req.Bin.Width, sizeByID)
	resp.BestPacker = bestPacker
	return resp, nil
}

func buildResponse1D(result pack.Result, binWidth float64, sizeByID map[string]float64) PackResponse {
	resp := PackResponse{
		BinsUsed:   result.BinsUsed(),
		Unplaced:   result.Unplaced,
		ItemErrors: placementErrors(result.PlacementErrors),
	}
	// Track how far along each bin we are so we can return x-offsets.
	offsets := make(map[string]float64)
	for _, p := range result.Placements {
		if p == nil {
			continue
		}
		pl1, ok := p.(*d1.Placement1D)
		if !ok {
			continue
		}
		sz := sizeByID[pl1.ItemID()]
		x := offsets[pl1.BinID()]
		resp.Placements = append(resp.Placements, PlacementResult{
			BinIndex: binIndexFromID(result.Bins, pl1.BinID()),
			ItemID:   pl1.ItemID(),
			X:        x,
			W:        sz,
			H:        binWidth,
		})
		offsets[pl1.BinID()] += sz
	}
	return resp
}

// ─── 2-D ─────────────────────────────────────────────────────────────────────

// strat2DFor maps an algorithm choice to the 2-D placement strategy it uses.
// MaxRects/Guillotine/Skyline are the standard trio; the shelf policies back the
// NFDH/FFDH/BFDH algorithms (paired with a decreasing-height sort).
func strat2DFor(algo string) func(w, h float64) d2.PlacementStrategy2D {
	switch algo {
	case "guillotine":
		return d2.NewGuillotineDefault
	case "skyline":
		return d2.NewSkylineDefault
	case "nfdh":
		return d2.NewShelfStrategy(d2.ShelfNextFit)
	case "ffdh":
		return d2.NewShelfStrategy(d2.ShelfFirstFit)
	case "bfdh":
		return d2.NewShelfStrategy(d2.ShelfBestFit)
	default:
		return d2.NewMaxRectsDefault
	}
}

// shelfLabel names the decreasing-height shelf algorithms for display.
var shelfLabel = map[string]string{"nfdh": "NFDH", "ffdh": "FFDH", "bfdh": "BFDH"}

func pack2D(ctx context.Context, req PackRequest) (PackResponse, error) {
	bw, bh := req.Bin.Width, req.Bin.Height
	factory := constrainedFactory(d2.NewFactory(bw, bh, strat2DFor(req.Algorithm)), req.Constraints)

	items := make([]pack.Item, len(req.Items))
	for i, spec := range req.Items {
		it := d2.NewItem(spec.ID, spec.Width, spec.Height, spec.AllowRotate)
		for k, v := range spec.Scalars {
			it.WithScalar(k, v)
		}
		items[i] = it
	}

	// Balance objectives layer on any balanceable algorithm (bf/wf/pref/auto).
	if prefs, weights := buildPreferences(req.Preferences); isBalanceable(req.Algorithm) && (len(prefs) > 0 || req.Algorithm == "pref") {
		result, best, perr := runBalanced(ctx, req.Algorithm, factory, prefs, weights, items)
		if perr != nil && !errors.Is(perr, pack.ErrItemTooLarge) {
			return PackResponse{Error: perr.Error()}, nil
		}
		if dx, dy, any := req.Contact.lateralAxes(); any {
			compactResult2D(result, bw, bh, dx, dy)
		}
		resp := buildResponse2D(result, false)
		resp.BestPacker = best
		return resp, nil
	}

	var result pack.Result
	var err error
	var bestPacker string

	switch req.Algorithm {
	case "ffd":
		p := offline.FirstFitDecreasing(factory)
		result, err = packAllCtx(ctx, p, items)
	case "bfd":
		p := offline.BestFitDecreasing(factory)
		result, err = packAllCtx(ctx, p, items)
	case "nfd":
		p := offline.NextFitDecreasing(factory)
		result, err = packAllCtx(ctx, p, items)
	case "nf":
		p := online.NextFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "bf":
		p := online.BestFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "wf":
		p := online.WorstFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "nfdh", "ffdh", "bfdh":
		// Shelf packing: decreasing-height sort + the shelf-fit policy baked into
		// the factory's strategy (set by strat2DFor).
		p := offline.New(shelfLabel[req.Algorithm], offline.DecreasingHeight, online.FirstFit(factory))
		result, err = packAllCtx(ctx, p, items)
	case "brute":
		result, err = offline.BruteForce(ctx, items, factory, offline.BruteForceOptions{Key: shapeKey2D})
	case "beam":
		result = offline.BeamSearch(ctx, items, factory, offline.BeamOptions{})
	case "rr":
		result = offline.RuinRecreate(ctx, items, factory, offline.SearchOptions{})
	case "grasp":
		result = offline.GRASP(ctx, items, factory, offline.SearchOptions{})
	case "auto":
		mrFactory := constrainedFactory(d2.NewFactory(bw, bh, d2.NewMaxRectsDefault), req.Constraints)
		gFactory := constrainedFactory(d2.NewFactory(bw, bh, d2.NewGuillotineDefault), req.Constraints)
		skyFactory := constrainedFactory(d2.NewFactory(bw, bh, d2.NewSkylineDefault), req.Constraints)
		p := meta.BestOf(
			offline.FirstFitDecreasing(mrFactory),
			offline.BestFitDecreasing(mrFactory),
			offline.NextFitDecreasing(mrFactory),
			offline.FirstFitDecreasing(gFactory),
			offline.BestFitDecreasing(gFactory),
			offline.FirstFitDecreasing(skyFactory),
		)
		result, err = packAllCtx(ctx, p, items)
		bestPacker = p.Winner()
	default: // ff, maxrects, guillotine, skyline
		p := online.FirstFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	}

	if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
		return PackResponse{Error: err.Error()}, nil
	}

	// Skip compaction for guillotine — it would desync the free-rect overlay.
	if dx, dy, any := req.Contact.lateralAxes(); any && req.Algorithm != "guillotine" {
		compactResult2D(result, bw, bh, dx, dy)
	}
	resp := buildResponse2D(result, req.Algorithm == "guillotine")
	resp.BestPacker = bestPacker
	return resp, nil
}

func buildResponse2D(result pack.Result, includeGuillotineFree bool) PackResponse {
	resp := PackResponse{
		BinsUsed:   result.BinsUsed(),
		Unplaced:   result.Unplaced,
		ItemErrors: placementErrors(result.PlacementErrors),
	}
	for _, p := range result.Placements {
		if p == nil {
			continue
		}
		pl2, ok := p.(*d2.Placement2D)
		if !ok {
			continue
		}
		resp.Placements = append(resp.Placements, PlacementResult{
			BinIndex: binIndexFromID(result.Bins, pl2.BinID()),
			ItemID:   pl2.ItemID(),
			X:        pl2.X,
			Y:        pl2.Y,
			W:        pl2.W,
			H:        pl2.H,
			Rotated:  pl2.Rotated,
		})
	}
	if includeGuillotineFree {
		resp.FreeRects = make([][]FreeRect, len(result.Bins))
		for i, bin := range result.Bins {
			b2, ok := bin.(*d2.Bin2D)
			if !ok {
				continue
			}
			g, ok := b2.Strategy().(*d2.Guillotine)
			if !ok {
				continue
			}
			for _, r := range g.FreeRects() {
				resp.FreeRects[i] = append(resp.FreeRects[i], FreeRect(r))
			}
		}
	}
	return resp
}

// ─── 3-D ─────────────────────────────────────────────────────────────────────

func pack3D(ctx context.Context, req PackRequest) (PackResponse, error) {
	bw, bd, bh := req.Bin.Width, req.Bin.Depth, req.Bin.Height
	// Deepest-bottom-left-fill is its own placement strategy; everything else uses
	// extreme-point with the contact spec (Bottom → hard support gate, SideX/SideY
	// → contact-maximizing placement).
	stratFn := d3.NewExtremePointStrategyContact(d3.ContactSpec{
		Bottom: req.Contact.Bottom, SideX: req.Contact.SideX, SideY: req.Contact.SideY,
		NoFloating: req.Contact.NoFloating,
	})
	if req.Algorithm == "blf" {
		stratFn = d3.NewBottomLeftFillStrategy
	}
	factory := constrainedFactory(d3.NewFactory(bw, bd, bh, stratFn), req.Constraints)

	items := make([]pack.Item, len(req.Items))
	for i, spec := range req.Items {
		it := d3.NewItem(spec.ID, spec.Width, spec.Depth, spec.Height, spec.AllowRotate)
		for k, v := range spec.Scalars {
			it.WithScalar(k, v)
		}
		items[i] = it
	}

	// Balance objectives layer on any balanceable algorithm (bf/wf/pref/auto).
	if prefs, weights := buildPreferences(req.Preferences); isBalanceable(req.Algorithm) && (len(prefs) > 0 || req.Algorithm == "pref") {
		result, best, perr := runBalanced(ctx, req.Algorithm, factory, prefs, weights, items)
		if perr != nil && !errors.Is(perr, pack.ErrItemTooLarge) {
			return PackResponse{Error: perr.Error()}, nil
		}
		if dx, dy, any := req.Contact.lateralAxes(); any {
			compactResult3D(result, bw, bd, bh, dx, dy, req.Contact.Bottom)
		}
		resp := buildResponse3D(result)
		resp.BestPacker = best
		return resp, nil
	}

	// LAFF (Largest-Area-Fit-First): layered packing; manages its own geometry
	// and containers, so it ignores the factory/contact settings.
	if req.Algorithm == "laff" {
		result, lerr := d3.LAFF(items, bw, bd, bh)
		if lerr != nil {
			return PackResponse{Error: lerr.Error()}, nil
		}
		return buildResponse3D(result), nil
	}

	// Brute-force: exhaustive item-order search for small orders (FFD fallback
	// above the cap). Identical boxes are pruned via a sorted-dimension key.
	if req.Algorithm == "brute" {
		result, berr := offline.BruteForce(ctx, items, factory, offline.BruteForceOptions{Key: shapeKey3D})
		if berr != nil && !errors.Is(berr, pack.ErrItemTooLarge) {
			return PackResponse{Error: berr.Error()}, nil
		}
		return buildResponse3D(result), nil
	}

	// Order-search metaheuristics (beam / ruin-recreate / GRASP): drop-in over the
	// extreme-point factory; they manage their own search and ignore contact.
	switch req.Algorithm {
	case "beam":
		return buildResponse3D(offline.BeamSearch(ctx, items, factory, offline.BeamOptions{})), nil
	case "rr":
		return buildResponse3D(offline.RuinRecreate(ctx, items, factory, offline.SearchOptions{})), nil
	case "grasp":
		return buildResponse3D(offline.GRASP(ctx, items, factory, offline.SearchOptions{})), nil
	}

	// Joint multi-objective: bin selection and placement under one score, in a
	// single pass — no separate compaction (contact is handled at placement).
	if req.Algorithm == "joint" {
		prefs, weights := buildPreferences(req.Preferences)
		jf := joint.New(bw, bd, bh, d3.ContactSpec{
			Bottom: req.Contact.Bottom, SideX: req.Contact.SideX, SideY: req.Contact.SideY,
			NoFloating: req.Contact.NoFloating,
		}, prefs, weights, buildConstraints(req.Constraints))
		result, jerr := jf.PackAllCtx(ctx, items)
		if jerr != nil && !errors.Is(jerr, pack.ErrItemTooLarge) {
			return PackResponse{Error: jerr.Error()}, nil
		}
		return buildResponse3D(result), nil
	}

	var result pack.Result
	var err error
	var bestPacker string

	switch req.Algorithm {
	case "ffd":
		p := offline.FirstFitDecreasing(factory)
		result, err = packAllCtx(ctx, p, items)
	case "bfd":
		p := offline.BestFitDecreasing(factory)
		result, err = packAllCtx(ctx, p, items)
	case "nfd":
		p := offline.NextFitDecreasing(factory)
		result, err = packAllCtx(ctx, p, items)
	case "nf":
		p := online.NextFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "bf":
		p := online.BestFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "wf":
		p := online.WorstFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	case "auto":
		blfFactory := constrainedFactory(d3.NewFactory(bw, bd, bh, d3.NewBottomLeftFillStrategy), req.Constraints)
		p := meta.BestOf(
			offline.FirstFitDecreasing(factory),
			offline.BestFitDecreasing(factory),
			offline.NextFitDecreasing(factory),
			offline.FirstFitDecreasing(blfFactory),
		)
		result, err = packAllCtx(ctx, p, items)
		bestPacker = p.Winner()
	default: // ff, blf
		p := online.FirstFit(factory)
		var e error
		if result, e = runOnline(ctx, p, items); e != nil && !errors.Is(e, pack.ErrItemTooLarge) {
			return PackResponse{Error: e.Error()}, nil
		}
	}

	if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
		return PackResponse{Error: err.Error()}, nil
	}

	if dx, dy, any := req.Contact.lateralAxes(); any {
		compactResult3D(result, bw, bd, bh, dx, dy, req.Contact.Bottom)
	}
	resp := buildResponse3D(result)
	resp.BestPacker = bestPacker
	return resp, nil
}

func buildResponse3D(result pack.Result) PackResponse {
	resp := PackResponse{
		BinsUsed:   result.BinsUsed(),
		Unplaced:   result.Unplaced,
		ItemErrors: placementErrors(result.PlacementErrors),
	}
	for _, p := range result.Placements {
		if p == nil {
			continue
		}
		pl3, ok := p.(*d3.Placement3D)
		if !ok {
			continue
		}
		resp.Placements = append(resp.Placements, PlacementResult{
			BinIndex: binIndexFromID(result.Bins, pl3.BinID()),
			ItemID:   pl3.ItemID(),
			X:        pl3.X,
			Y:        pl3.Y,
			Z:        pl3.Z,
			W:        pl3.W,
			D:        pl3.D,
			H:        pl3.H,
		})
	}
	return resp
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// placementErrors converts a map[string]error to map[string]string for JSON serialisation.
func placementErrors(errs map[string]error) map[string]string {
	if len(errs) == 0 {
		return nil
	}
	out := make(map[string]string, len(errs))
	for k, v := range errs {
		out[k] = v.Error()
	}
	return out
}

// buildPreferences converts PreferenceSpec slice to parallel preference and
// weight slices. Weights are kept separate (not baked via pack.Weighted) so the
// normalized selector can apply them after min-max normalizing each preference —
// making weights comparable across preferences on different scales. "balance"
// spreads a scalar evenly (ColocateLow); "concentrate" groups it (ColocateHigh);
// an empty scalar uses item count. A missing or zero weight defaults to 1.
func buildPreferences(specs []PreferenceSpec) (prefs []pack.Preference, weights []float64) {
	for _, s := range specs {
		w := s.Weight
		if w == 0 {
			w = 1
		}
		var base pack.Preference
		switch s.Mode {
		case "fillhigh":
			base = pack.FillHigh() // best-fit as a preference, no scalar
		case "filllow":
			base = pack.FillLow() // worst-fit as a preference, no scalar
		case "minheight":
			base = pack.MinimizeHeight() // geometric, no scalar needed
		case "mincg":
			if s.Scalar == "" {
				continue
			}
			base = pack.MinimizeCG(s.Scalar) // s.Scalar names the mass scalar
		case "balance":
			if s.Scalar == "" {
				base = pack.BalanceCount() // no scalar → balance by item count
			} else {
				base = pack.ColocateLow(s.Scalar)
			}
		default: // "concentrate"
			if s.Scalar == "" {
				base = pack.ConcentrateCount() // no scalar → concentrate by item count
			} else {
				base = pack.ColocateHigh(s.Scalar)
			}
		}
		prefs = append(prefs, base)
		weights = append(weights, w)
	}
	return prefs, weights
}

// runPreferenceFit packs items with the two-phase BalancedFit: it learns the
// minimum bin count, then distributes items across that many bins using the
// preferences. This balances within the fewest bins instead of spilling into an
// extra one the way single-pass online preference selection can. The factory is
// wrapped in a ConstrainedFactory if needed so bins expose Aggregates().
// isBalanceable reports whether balance objectives can layer on an algorithm.
// Only fit policies expressible as preferences qualify: Best-Fit (FillHigh),
// Worst-Fit (FillLow), Preference-Fit (objectives only), and Auto (which then
// chooses among the balanceable fit flavors).
func isBalanceable(algo string) bool {
	switch algo {
	case "bf", "wf", "pref", "auto":
		return true
	}
	return false
}

// runBalanced layers balance objectives on a balanceable algorithm via the
// two-pass BalancedFit. Best-Fit/Worst-Fit prepend a fill preference (at weight
// 1) so the distribution leans full/empty; Auto tries both balanceable flavors
// and keeps the one using fewer bins, breaking ties on lower imbalance. Returns
// the winning flavor label (for Auto).
func runBalanced(ctx context.Context, algo string, factory pack.BinFactory, prefs []pack.Preference, weights []float64, items []pack.Item) (pack.Result, string, error) {
	if _, ok := factory.(*pack.ConstrainedFactory); !ok {
		factory = pack.NewConstrainedFactory(factory)
	}
	run := func(fill pack.Preference) (pack.Result, error) {
		if err := ctx.Err(); err != nil {
			return pack.Result{}, err
		}
		p, w := prefs, weights
		if fill != nil {
			p = append([]pack.Preference{fill}, prefs...)
			w = append([]float64{1}, weights...)
		}
		r, err := packAllCtx(ctx, offline.NewBalancedFitW(factory, p, w), items)
		// Local-search pass: move/swap items between bins to tighten the balance
		// (no-op above RefineBalanceMaxItems items).
		return offline.RefineBalance(factory, r, items), err
	}
	switch algo {
	case "bf":
		r, err := run(pack.FillHigh())
		return r, "", err
	case "wf":
		r, err := run(pack.FillLow())
		return r, "", err
	case "pref":
		r, err := run(nil)
		return r, "", err
	case "auto":
		rh, eh := run(pack.FillHigh())
		rl, el := run(pack.FillLow())
		hOK := eh == nil || errors.Is(eh, pack.ErrItemTooLarge)
		lOK := el == nil || errors.Is(el, pack.ErrItemTooLarge)
		switch {
		case hOK && !lOK:
			return rh, "Best-Fit + balance", eh
		case lOK && !hOK:
			return rl, "Worst-Fit + balance", el
		case rh.BinsUsed() != rl.BinsUsed():
			if rh.BinsUsed() < rl.BinsUsed() {
				return rh, "Best-Fit + balance", eh
			}
			return rl, "Worst-Fit + balance", el
		case binImbalance(rh, items) <= binImbalance(rl, items):
			return rh, "Best-Fit + balance", eh
		default:
			return rl, "Worst-Fit + balance", el
		}
	}
	return pack.Result{}, "", nil
}

// binImbalance scores how unevenly a result spreads its metrics across bins,
// summing the coefficient of variation (σ/mean) of item count and each scalar.
// Lower is more balanced; used to break Auto ties between fit flavors.
func binImbalance(r pack.Result, items []pack.Item) float64 {
	scalars := make(map[string]map[string]float64, len(items))
	for _, it := range items {
		scalars[it.ID()] = pack.ScalarsOf(it)
	}
	// Per-bin count and per-bin scalar totals.
	keys := map[string]bool{}
	counts := map[string]float64{}
	totals := map[string]map[string]float64{} // binID -> scalar -> sum
	for _, p := range r.Placements {
		if p == nil {
			continue
		}
		b := p.BinID()
		counts[b]++
		if totals[b] == nil {
			totals[b] = map[string]float64{}
		}
		for k, v := range scalars[p.ItemID()] {
			totals[b][k] += v
			keys[k] = true
		}
	}
	n := r.BinsUsed()
	if n == 0 {
		return 0
	}
	cv := func(vals []float64) float64 {
		mean := 0.0
		for _, v := range vals {
			mean += v
		}
		mean /= float64(n)
		if mean == 0 {
			return 0
		}
		varc := 0.0
		for _, v := range vals {
			varc += (v - mean) * (v - mean)
		}
		return (varc/float64(n)) / (mean * mean) // σ²/mean² — scale-free
	}
	// item count
	countVals := make([]float64, 0, n)
	for _, b := range r.Bins {
		countVals = append(countVals, counts[binID(b)])
	}
	total := cv(countVals)
	// each scalar
	for k := range keys {
		vals := make([]float64, 0, n)
		for _, b := range r.Bins {
			id := binID(b)
			vals = append(vals, totals[id][k])
		}
		total += cv(vals)
	}
	return total
}

// compactResult3D slides each bin's items toward the lateral walls to remove
// slosh (in place on the d3 placements).
func compactResult3D(r pack.Result, bw, bd, bh float64, doX, doY bool, support float64) {
	byBin := map[string][]*d3.Placement3D{}
	for _, p := range r.Placements {
		if pl, ok := p.(*d3.Placement3D); ok {
			byBin[pl.BinID()] = append(byBin[pl.BinID()], pl)
		}
	}
	for _, ps := range byBin {
		d3.Compact(ps, bw, bd, bh, doX, doY, support)
	}
}

// compactResult2D is the 2-D equivalent of compactResult3D.
func compactResult2D(r pack.Result, bw, bh float64, doX, doY bool) {
	byBin := map[string][]*d2.Placement2D{}
	for _, p := range r.Placements {
		if pl, ok := p.(*d2.Placement2D); ok {
			byBin[pl.BinID()] = append(byBin[pl.BinID()], pl)
		}
	}
	for _, ps := range byBin {
		d2.Compact(ps, bw, bh, doX, doY)
	}
}

// binID extracts a bin's ID via its optional ID() method.
func binID(b pack.Bin) string {
	if idr, ok := b.(interface{ ID() string }); ok {
		return idr.ID()
	}
	return ""
}

// shapeKey3D/2D/1D map an item to an interchangeability signature for
// BruteForce: items with the same key pack identically, so permutations that
// only reorder them are pruned. Sorted dimensions make rotations-equivalent
// boxes collapse. Unknown item types fall back to the unique ID (no pruning).
func shapeKey3D(it pack.Item) string {
	if i, ok := it.(*d3.Item3D); ok {
		d := []float64{i.W, i.D, i.H}
		sort.Float64s(d)
		return fmt.Sprintf("%g,%g,%g", d[0], d[1], d[2])
	}
	return it.ID()
}

func shapeKey2D(it pack.Item) string {
	if i, ok := it.(*d2.Item2D); ok {
		d := []float64{i.W, i.H}
		sort.Float64s(d)
		return fmt.Sprintf("%g,%g", d[0], d[1])
	}
	return it.ID()
}

func shapeKey1D(it pack.Item) string {
	return fmt.Sprintf("%g", it.Volume())
}

// buildConstraints converts ConstraintSpec slice to pack.Constraint slice.
func buildConstraints(specs []ConstraintSpec) []pack.Constraint {
	cs := make([]pack.Constraint, 0, len(specs))
	for _, s := range specs {
		switch s.Op {
		case "max":
			cs = append(cs, pack.MaxAggregate(s.Scalar, s.Value))
		case "min":
			cs = append(cs, pack.MinAggregate(s.Scalar, s.Value))
		case "allsame":
			cs = append(cs, pack.AllSame(s.Scalar))
		case "incompatible":
			cs = append(cs, pack.Incompatible(s.Scalar, [2]float64{s.Value, s.Value2}))
		}
	}
	return cs
}

// constrainedFactory wraps factory with hard constraints if any are specified.
func constrainedFactory(factory pack.BinFactory, specs []ConstraintSpec) pack.BinFactory {
	cs := buildConstraints(specs)
	if len(cs) == 0 {
		return factory
	}
	return pack.NewConstrainedFactory(factory, cs...)
}

func binIndexFromID(bins []pack.Bin, id string) int {
	for i, b := range bins {
		type idder interface{ ID() string }
		if b2, ok := b.(idder); ok && b2.ID() == id {
			return i
		}
	}
	return 0
}

// ─── nested pack ─────────────────────────────────────────────────────────────

type NestedLevelSpec struct {
	Bin         BinSpec          `json:"bin"`
	Algorithm   string           `json:"algorithm"`
	Constraints []ConstraintSpec `json:"constraints,omitempty"`
	Preferences []PreferenceSpec `json:"preferences,omitempty"`
	Contact     ContactSpec      `json:"contact,omitempty"`
}

type NestedPackRequest struct {
	Mode   string            `json:"mode"`
	Levels []NestedLevelSpec `json:"levels"`
	Items  []ItemSpec        `json:"items"`
}

type NestedLevelResult struct {
	BinsUsed   int               `json:"bins_used"`
	Placements []PlacementResult `json:"placements"`
	Unplaced   []string          `json:"unplaced"`
	BinDims    BinSpec           `json:"bin_dims"`
}

type NestedPackResponse struct {
	Levels []NestedLevelResult `json:"levels"`
	Error  string              `json:"error,omitempty"`
}

func doNestedPack(ctx context.Context, req NestedPackRequest) (NestedPackResponse, error) {
	if len(req.Levels) < 2 {
		return NestedPackResponse{}, fmt.Errorf("nested packing requires at least 2 levels")
	}

	// Level 0: pack items into inner bins (cartons).
	// Inherit the outer level's bottom-support requirement so the physical
	// stacking rule is enforced inside cartons as well as on pallets.
	l0spec := req.Levels[0]
	l0Contact := l0spec.Contact
	if b := req.Levels[1].Contact.Bottom; b > l0Contact.Bottom {
		l0Contact.Bottom = b
	}
	if req.Levels[1].Contact.NoFloating {
		l0Contact.NoFloating = true
	}
	l0req := PackRequest{
		Mode:        req.Mode,
		Algorithm:   l0spec.Algorithm,
		Bin:         l0spec.Bin,
		Items:       req.Items,
		Constraints: l0spec.Constraints,
		Preferences: l0spec.Preferences,
		Contact:     l0Contact,
	}
	l0resp, err := packByMode(ctx, l0req)
	if err != nil {
		return NestedPackResponse{}, err
	}

	// Accumulate scalars per carton bin from placed items.
	binScalars := make(map[int]map[string]float64)
	itemScalarsByID := make(map[string]map[string]float64, len(req.Items))
	for _, spec := range req.Items {
		itemScalarsByID[spec.ID] = spec.Scalars
	}
	for _, p := range l0resp.Placements {
		if p.ItemID == "" {
			continue
		}
		bs := binScalars[p.BinIndex]
		if bs == nil {
			bs = make(map[string]float64)
			binScalars[p.BinIndex] = bs
		}
		for k, v := range itemScalarsByID[p.ItemID] {
			bs[k] += v
		}
	}

	// Build one carton item per filled bin.
	cartonItems := make([]ItemSpec, l0resp.BinsUsed)
	for b := 0; b < l0resp.BinsUsed; b++ {
		cartonItems[b] = ItemSpec{
			ID:      fmt.Sprintf("carton_%d", b),
			Width:   l0spec.Bin.Width,
			Height:  l0spec.Bin.Height,
			Depth:   l0spec.Bin.Depth,
			Scalars: binScalars[b],
		}
	}

	// Level 1: pack carton items into outer bins (pallets).
	l1spec := req.Levels[1]
	l1req := PackRequest{
		Mode:        req.Mode,
		Algorithm:   l1spec.Algorithm,
		Bin:         l1spec.Bin,
		Items:       cartonItems,
		Constraints: l1spec.Constraints,
		Preferences: l1spec.Preferences,
		Contact:     l1spec.Contact,
	}
	l1resp, err := packByMode(ctx, l1req)
	if err != nil {
		return NestedPackResponse{}, err
	}

	return NestedPackResponse{
		Levels: []NestedLevelResult{
			{BinsUsed: l0resp.BinsUsed, Placements: l0resp.Placements, Unplaced: l0resp.Unplaced, BinDims: l0spec.Bin},
			{BinsUsed: l1resp.BinsUsed, Placements: l1resp.Placements, Unplaced: l1resp.Unplaced, BinDims: l1spec.Bin},
		},
	}, nil
}

// packByMode dispatches to the appropriate dimensioned packer.
func packByMode(ctx context.Context, req PackRequest) (PackResponse, error) {
	switch req.Mode {
	case "1d":
		return pack1D(ctx, req)
	case "2d":
		return pack2D(ctx, req)
	case "3d":
		return pack3D(ctx, req)
	default:
		return PackResponse{}, fmt.Errorf("unknown mode: %s", req.Mode)
	}
}
