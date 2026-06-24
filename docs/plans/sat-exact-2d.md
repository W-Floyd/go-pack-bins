# Plan: SAT-Based Exact 2-D Bin Packing with Optimality Certificates

**Status:** ✅ Implemented. `sat` package + packapi `2d/sat` algorithm shipped and
tested (encoder, certificate, float-scaling, packapi wiring, wasm-verified). Deferred
follow-ups (incremental SAT, MaxSAT, 1-D/3-D, demand multiplicity) remain per §6.
**Source:** Kieu, Hoang & To (2026), "Solving the Two-Dimensional Single Stock Size
Cutting Stock Problem with SAT and MaxSAT", arXiv:2604.01732 — the order-encoding +
conditional non-overlap + symmetry-breaking layer, adapted from cutting-stock back to
plain 2-D bin packing. Builds on Soh, Inoue, Tamura, Banbara & Nabeshima (2010), the
order-encoding SAT strip-packing formulation.

---

## 0. Decisions already taken

- **gophersat is now an allowed main-module dependency** (`github.com/crillab/gophersat`
  v1.4.0, pure Go). The no-external-deps rule in [CLAUDE.md](../../CLAUDE.md) has been
  relaxed to this one solver. It MUST stay confined to the SAT package so the rest of
  the library remains dependency-free.
- **Target is exact 2-D rectangular bin packing with optimality certificates** — not
  cutting-stock, not 1-D, not MaxSAT (those are follow-ups in §6).

## 1. Why this exists / the use case

[offline/bincompletion.go](../../offline/bincompletion.go) is the only **exact** solver
and it is **1-D only**. For 2-D, the library has strong heuristics (MaxRects /
Guillotine / Skyline / Shelf, FFD/BFD, beam, ruin-recreate, GRASP) but **none can prove
a bin count optimal** — a caller never learns whether "12 bins" is the best possible or
just the best found. SAT closes that gap: encode "do all items fit in `k` bins?" as a
Boolean formula; SAT ⇒ yes, UNSAT ⇒ provably no. Squeezing `k` down to where `k`
is SAT and `k−1` is UNSAT yields a **certified optimum** for small/medium instances.

Use case: exact answers and real optimality gaps for 2-D rectangular packing —
QA/validation of the heuristics, tight benchmarks, and any setting where "is this
actually optimal?" matters more than raw speed.

## 2. Why the library does NOT already cover it

- No 2-D exact solver of any kind; `BinCompletion` is 1-D (it reasons over scalar sizes,
  not 2-D geometry).
- The heuristics compute **no lower bound** and emit no certificate (same gap noted in
  [exact-1d-lower-bounds.md](exact-1d-lower-bounds.md)).
- No SAT/CP machinery existed in-tree until gophersat was added for this feature.

## 3. Design

### 3.1 New package `sat` (confines the dependency)

A dedicated package is the **only** place that imports gophersat. Proposed surface:

```go
package sat

type Options struct {
    AllowRotation bool          // permit 90° rotation (adds R_c vars + clauses)
    SymmetryBreak bool          // SB1–SB4 (default true)
    TimeLimit     time.Duration // 0 = honour ctx only
    // Scale: see §3.4. 0 = auto-detect integer/rational scaling; error if not exact.
}

type Result struct {
    pack.Result          // bins + placements
    Optimal   bool       // true iff the bin count is *proven* minimal (see §3.3)
    LowerBound int        // best LB established (area bound or last UNSAT k+1)
    UpperBound int        // best feasible k found
    Proof     string     // short human note: "UNSAT at k=11" / "meets area bound"
}

// Pack2D packs items into W×H bins, minimising bin count, and certifies
// optimality when it can. ctx/TimeLimit bound the search; on timeout it returns
// the best feasible packing with Optimal=false.
func Pack2D(ctx context.Context, items []*d2.Item2D, W, H float64, opts Options) (Result, error)
```

### 3.2 SAT encoding (per candidate bin count `k`)

Variables (gophersat uses 1-indexed int literals; allocate a counter):

- **Bin assignment** `s_{c,j}` — item `c` on bin `j`. Exactly-one per item. Use
  gophersat's native **`CardConstr`** (cardinality) for exactly-one rather than the
  O(k²) pairwise `¬s_{c,j1} ∨ ¬s_{c,j2}` clauses in the paper — more compact.
- **Order-encoded position** `px_{c,e}` ("x_c ≤ e", `e ∈ 0..W−w_c`), `py_{c,f}`
  likewise. Axioms `px_{c,e} ⇒ px_{c,e+1}` enforce monotonicity.
- **Relative position** `ℓ_{c,c'}` ("c left of c'"), `u_{c,c'}` ("c below c'").
- **Rotation** `R_c` — only when `AllowRotation`; swaps `w_c↔h_c` in the fit clauses.
- **Bin usage** `a_j` ("bin j non-empty"), via `¬s_{c,j} ∨ a_j`.

Clauses (paper §4.2):

- **Conditional non-overlap**, for `c<c'` and each bin `j`:
  `¬s_{c,j} ∨ ¬s_{c',j} ∨ ℓ_{c,c'} ∨ ℓ_{c',c} ∨ u_{c,c'} ∨ u_{c',c}` — the geometric
  disjunction fires only when both share bin `j`.
- **Position-relation consistency** linking `ℓ/u` to the `px/py` coordinate vars
  (the offset clauses, eq. 7 in the paper).
- **Domain fit** within `W×H` (eq. 8–9).

### 3.3 Solve loop + certificate semantics

- `LB = ⌈Σ wᵢhᵢ / (W·H)⌉` (area bound — same L1 used everywhere else).
- `UB =` FFD bin count via existing [offline.FirstFitDecreasing](../../offline).
- Binary search `k ∈ [LB, UB]`: build `Φ_k`, solve. SAT → record placement, `UB=k`;
  UNSAT → `LB=k+1`. Stop at `LB==UB`.
- **`Optimal = true`** iff the returned `k` is backed by a proof that `k−1` is
  impossible: either an explicit **UNSAT at `k−1`**, or `k == area-LB` (the area bound
  itself proves `k−1` infeasible without a solver call). Otherwise (timeout, or fell
  back to heuristic, or grid-capped per §3.4) return best feasible with `Optimal=false`
  and an honest `Proof` string. **Never** claim optimality without one of those two
  proofs — that is the core integrity guarantee.
- Decode the SAT model's `px/py` back into `Placement2D` (largest `e` with `px_{c,e}`
  false gives `x_c`, etc.); reconstruct which bin from `s_{c,j}`.

Incremental SAT (paper §5.2) is **implemented and the default**: build the formula
once at the upper bound with bin-usage vars `a_j`, then walk the count *down*,
appending a unit clause `¬a_m` to disable one more bin and re-solving — gophersat
retains learned clauses across solves. (gophersat's `Assume` bakes assumptions in as
permanent units, so the paper's *binary-search-with-assumptions* isn't possible; the
monotonic downward `AppendClause` search is the equivalent that fits its append-only
model.) Measured ~3–4× faster than the per-`k` rebuild on wide-gap instances
(`TestIncrementalSpeed`); `Options.NonIncremental` forces the legacy rebuild.

### 3.4 Floating-point dimensions (the main correctness surface)

Order-encoding is defined for **integer** `W, H, w, h`. The library is `float64`. So:

- Auto-detect a common scale: if all dimensions are integers (or rationals with a small
  common denominator), scale up to integers losslessly and encode.
- If they are **not** exactly representable on a reasonable integer grid, **return an
  error** (or, opt-in, round to a grid — but rounding can make a *feasible* `k` look
  UNSAT, producing a `k*` that is too large; a wrong certificate is a correctness bug,
  not a slowdown). Default = exact-or-error; never silently round under `Optimal=true`.
- The grid size `W·H` after scaling drives formula size. Cap it (configurable); above
  the cap, skip SAT and return the heuristic packing with `Optimal=false`.

### 3.5 Symmetry breaking (paper §4.3)

- **SB1** large-item: pairs that can't sit side-by-side in any orientation ⇒ fix the
  relevant `ℓ/u` false.
- **SB2** equal-item ordering: identical items get a fixed relative order (kills the
  permutation symmetry that demand/duplicate items create — the library often has many
  identical items, so this matters).
- **SB3** infeasible-orientation: if only one orientation fits, fix `R_c` by a unit
  clause.
- **SB4** bin ordering `¬a_{j+1} ∨ a_j`: use bins in index order, killing the `k!`
  relabelling symmetry.

## 4. Integration (per CLAUDE.md "Adding an algorithm")

1. `sat` package: encoder + solve loop + `Pack2D`, importing gophersat **only here**.
2. `packapi`: `registerSolve("2d", "sat", solveSAT)` in
   [algos_2d.go](../../packapi/algos_2d.go); advertise in `AlgoCapabilities()` with the
   rotation/symmetry tunables. The drift tests (`TestRegistryMatchesCapabilities`,
   `TestAdvertisedAlgosSolve`) enforce both halves — so registering without advertising
   (or vice-versa) fails CI.
3. **Not** stream-incremental (the solve is global, commits no placements until done) —
   do **not** add it to `isStreamable`; it emits one batched frame like `auto`/`gbpp`.
4. Surface the **certificate** (`Optimal`, `LowerBound`/`UpperBound`) through the solve
   metadata so both front-ends can show "proven optimal" vs "best found". Likely a small
   extension to `solveMeta` / the metrics payload.
5. `cmd/webdemo/static/index.html`: appears automatically via `/api/algos` (frontend
   self-configures) — but the *certificate display* is a UI addition to flag for the
   user to verify (UI isn't covered by Go tests).
6. **wasm**: gophersat is pure Go and should compile under `GOOS=js GOARCH=wasm` —
   **verify** with `GOOS=js GOARCH=wasm go build ./cmd/wasm` and `./scripts/build-wasm.sh`.
   If it pulls in anything wasm-hostile, gate the SAT algo out of the wasm build.
7. `ATTRIBUTION.md` + package doc: attribute the Soh et al. order-encoding and the Kieu
   et al. 2-D-CSSP adaptation; note gophersat as the solver.

## 5. Implementation steps

1. `go.mod`: gophersat added ✓. Run `go mod tidy` once the `sat` package imports it (it
   is currently `// indirect`).
2. `sat`: variable allocator + order-encoding axioms + domain-fit clauses; unit-test the
   encoding of a single item in isolation.
3. `sat`: conditional non-overlap + position-relation clauses; SB1–SB4.
4. `sat`: float→int scaling (§3.4) with exact-or-error default; tests asserting the grid
   reconstructs exact coordinates.
5. `sat`: solve loop + certificate logic; decode model → `Placement2D`.
6. `sat`: correctness tests — small hand-checked instances where the optimum is known
   (e.g. 4 items each `0.6W×0.6H` ⇒ need 4 bins; assert `Optimal` and the count). Assert
   `Pack2D` never returns `Optimal=true` with a count above a brute-force optimum, and
   never below the area bound. Cross-check vs `offline.BruteForce`/`BinCompletion`-style
   results on tiny 1-D-embeddable cases.
7. `packapi`: register + advertise + metadata; run drift tests and `go test -race`.
8. wasm build check (§4.6); ATTRIBUTION + doc comments.

## 6. Risks / decisions to revisit

- **Float→int correctness (highest).** A wrong scale → a certificate that lies. Default
  to exact-or-error; gate optimistic rounding behind an explicit opt-in that also forces
  `Optimal=false`. Property test: `Pack2D` result is never below the area LB and, on
  brute-forceable instances, equals the true optimum.
- **Formula-size / runtime blow-up.** *(Addressed.)* The binding cost is the
  **clause count**, dominated by the O(n²·(W+H)) pairwise position-link clauses
  (×2 under rotation) — *not* the n·(W+H) grid-cell count. Rather than reject on an
  a-priori worst-case estimate (which overcounts when link clauses turn out
  tautological), the encoder **builds and aborts on the actual count**: `enc` tracks
  live var/clause counts and sets `overflow` the moment a cap is crossed, so `Pack2D`
  degrades to the FFD heuristic packing (`Optimal=false`, `ErrGridTooLarge`) only when
  the real formula exceeds the budget — instances whose formula is smaller than the
  estimate still solve. The bound is enforced at build time (where the formula memory
  is committed); gophersat's solve is not cancellable mid-run, and the ≈250 B/clause
  calibration already includes the solve-phase peak. (`estimateFormula` remains as an
  informational helper for `TestMemSweep`.) Calibrated by `sat.TestMemSweep`: peak heap is ≈250
  bytes/clause (near-constant, after dropping our clause copy once gophersat parses
  it — `enc.problem`). `MaxClauses` 2M ≈ 0.5 GB (default); the packapi UI ceiling is
  ≈250 B/clause. The UI exposes a single "Memory budget (MB)" knob (default 4 GB);
  packapi derives the clause/var caps from it via that figure, so the user picks an
  allowable RAM figure rather than opaque clause counts. A user-set budget is *not*
  clamped — the budget itself is the guard, and a very high value is the user's call.
  gophersat is correct but not Glucose-fast, so this stays a small/medium-instance
  tool; `ctx`/time-limit bound runtime.
- **Further memory levers (applied).** (1) `enc.problem` frees `e.cards` right after
  `ParseCardConstrs`, so our clause slice and gophersat's copy don't coexist through
  the solve (~340→~250 B/clause). (2) `build` skips generating the O(positions) link
  clauses for pairs SB1 already proved can't sit side by side, fixing the relation
  false instead (helps large-item instances). Net with normal patterns: the uniform
  50×(100×100)-style case dropped from ~2 GB to ~0.17 GB at n=140, and the sweep now
  reaches n≈400 under 2 GB.
- **Normal-pattern position reduction *(memory win)*.** Coordinates range only over
  the reachable subset sums of item widths/heights (`normalPositions`), not the full
  integer grid — a packing can always be pushed toward the origin onto such positions,
  so this is complete. This replaces the W·H factor in the clause count with the
  (often far smaller) number of reachable positions: for repeated dimensions (e.g.
  many 10×10 items in 100×100) positions collapse to multiples of 10, ~8× fewer
  clauses / less peak heap, and ~2.5× more items solvable under the same cap
  (`TestMemSweep`). The previously-untractable uniform 50×(100×100) case now certifies
  (`TestNormalPatternsShrinksUniformGrid`).
- **wasm.** Must confirm gophersat compiles to `js/wasm`; if not, exclude the SAT algo
  from the wasm bundle (build tag) rather than break the demo.
- **Dependency confinement.** gophersat must be imported by the `sat` package only; a
  test or `go list` check could guard against leakage into other packages.
- **Scope creep.** MaxSAT single-shot minimisation, 1-D and 3-D encodings, and the
  full 2-D-CSSP demand/multiplicity model are all deliberately deferred. (Incremental
  SAT — once on this list — is now implemented and the default; see §3.3.)
