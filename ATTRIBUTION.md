# Attribution

This project is original Go code. Some algorithms are clean-room
reimplementations of *ideas* from other open-source packing libraries — no code
was copied. Each derived source file names its upstream inspiration in its doc
comment; this file records the exact upstream commit each was based on, so the
implementations can be revisited when upstream advances.

## skjolber/3d-bin-container-packing (Apache-2.0)

- Repo: https://github.com/skjolber/3d-bin-container-packing
- Based on commit: `4adad7950a31208e7910b500f1ef2966ab28a234` ("Remove warnings", 2026-06-20)
- License: Apache License 2.0, © Thomas Skjølberg

Ideas reimplemented:

| Idea | Our implementation |
|------|--------------------|
| Largest-Area-Fit-First (LAFF) layered packing | [`d3/laff.go`](d3/laff.go) |
| Layered packing, sequential/streaming variant — lay items flat, sort by smallest dimension, fill one layer's floor (2-D MaxRects) at a time so progress streams | [`d3/layer.go`](d3/layer.go) (our adaptation of the LAFF layer idea) |
| Block-building layered packing — fuse items into solid layer-height blocks (direct + same-footprint vertical stacks summing to the course height), tile them into the floor, then a last-resort tallest-fit gap-filler. A bounded clean-room recombination of established container-loading heuristics: wall/layer building (George & Robinson, *A heuristic for packing boxes into a container*, Computers & OR, 1980), block arrangement (Eley, *Solving container loading problems by block arrangement*, EJOR, 2002), and the block heuristics of Fanslau & Bortfeldt (*A tree search algorithm for solving the container loading problem*, INFORMS J. Computing, 2010). The tiering (waste-free blocks first to keep clean layer lines, tallest-fit fallback second) is our own design choice, not a novel method. | [`d3/blocks.go`](d3/blocks.go) |
| Assemble-then-place — greedy *guillotine block* construction (fuse boxes sharing a full face into larger solid rectangles, rotation-aware) feeding the EMS placer. The guillotine-block idea is again Eley (2002) / Fanslau & Bortfeldt (2010); the two-phase split (assemble perfect blocks, then hand off to a separate placement engine) is our composition. | [`d3/assemble.go`](d3/assemble.go) |
| Brute-force packager for small orders (with permutation pruning + deadline) | [`offline/bruteforce.go`](offline/bruteforce.go) — uses our `context` deadline |
| Heterogeneous container catalog + max container count | [`catalog/catalog.go`](catalog/catalog.go) |
| Manifest rule (incompatible items must not share a container) | [`pack/incompatible.go`](pack/incompatible.go) |

To update: re-clone upstream, diff the relevant algorithm against the new
commit, port any improvements, and bump the commit hash above.

## bavix/boxpacker3 (MIT)

- Repo: https://github.com/bavix/boxpacker3
- Commit used by the benchmark: `a927bae749d02916c8ec91ba7d271356e49964c9`
- Used only as a comparison baseline in [`bench/`](bench/) (a separate module);
  no algorithm code is derived from it.

## gedex/bp3d (MIT)

- Repo: https://github.com/gedex/bp3d
  (commit `0ba3dcda7ab3`; pre-modules, resolved via a synthesized pseudo-version)
- Used only as a comparison baseline in [`bench/`](bench/) (a separate module);
  no algorithm code is derived from it.
- bp3d is a near-line-for-line implementation of the pivot heuristic in Dube &
  Kanavathy, *Optimizing Three-Dimensional Bin Packing Through Simulation*
  (IASTED, 2006) — the same paper behind enzoruiz/3dbinpacking and py3dbp. That
  method is a **restricted, early form of extreme-point packing**: candidate
  pivots are generated only at the right/front/top corner of each placed item,
  with **no backtracking** (an item, once placed, is never repacked). The paper's
  worst-case "analysis" reuses the classic *1-D* bin-packing ratios (FFD ≤ 11/9,
  FF ≤ 17/10), which do not hold in 3-D. go-pack-bins' [`d3`](d3/) extreme-point
  packer is the proper, more general descendant of this idea; the benchmark
  quantifies the gap (bp3d leaves small instances in half-empty bins — the
  leftover-item pathology the paper itself describes).

## 3-D placement heuristics from the literature (clean-room, ideas only)

Two `d3` placement strategies are clean-room implementations of standard
container-loading heuristics — no code from any repository was used:

| Idea | Source | Our implementation |
|------|--------|--------------------|
| Empty-Maximal-Space (EMS): maintain the set of maximal free boxes, place at the back-bottom-left of the snuggest space, then split intruded spaces into maximal slabs (the 3-D analogue of 2-D MaxRects) | Parreño, Alvarez-Valdés, Oliveira & Tamarit, *A maximal-space algorithm for the container loading problem*, INFORMS J. Computing (2008); Lai & Chan (1997) | [`d3/ems.go`](d3/ems.go) |
| Heightmap / skyline: model the occupied volume as a top surface and land each item on the lowest resting height over its footprint (can bridge gaps) — the standard baseline in the online-3D-BPP literature | Common heuristic; e.g. the EMS/heightmap baselines in Zhao et al., *Online-3D-BPP-PCT* (ICLR 2022) | [`d3/heightmap.go`](d3/heightmap.go) |

## Online-3D-BPP-PCT (MIT)

- Repo: https://github.com/alexfrom0815/Online-3D-BPP-PCT (commit `5e7b4238b18310af4529e0f85157d17de605850c`)
- Investigated (ICLR 2022, Zhao et al.) but **not** adopted: the core method is a
  trained deep-RL policy, impractical for a pure-Go, dependency-free library. Its
  non-ML heuristic baselines (heightmap-min, EMS scoring) are now implemented —
  see the EMS and heightmap strategies above.

## Generalized Bin Packing literature (Mantzou & Dimitriadis 2025 SLR)

Mantzou, T., & Dimitriadis, S. (2025). *Generalized bin packing and related
problems: A systematic literature review.* Journal of Industrial Engineering and
Management, 18(3), 394–426. https://doi.org/10.3926/jiem.8784

Clean-room implementations of methods/objectives surveyed there (ideas, not code):

| Idea | Source(s) cited in the SLR | Our implementation |
|------|----------------------------|--------------------|
| Generalized Bin Packing objective: optional items + profit, bin cost, rejection (BPRC), net-cost minimisation | Baldi, Crainic, Perboli & Tadei (2012); Hu, Wei & Lim (2018) | [`gbpp/gbpp.go`](gbpp/gbpp.go) |
| Ruin-and-recreate metaheuristic | Gardeyn & Wauters (2022), 2-D variable-sized BPP | [`offline/metaheuristic.go`](offline/metaheuristic.go) |
| GRASP (greedy randomized adaptive search) | Correcher et al. (2017); Calzavara et al. (2021) | [`offline/metaheuristic.go`](offline/metaheuristic.go) |
| Beam search (bounded tree search over placement order) | Araya et al. (2020); Parreño et al. (2020) | [`offline/beam.go`](offline/beam.go) |
| Lexicographic multi-objective selection | Bin packing with lexicographic objectives (2022) | [`meta/lexicographic.go`](meta/lexicographic.go) |

Not yet implemented (larger, geometric — candidates for future work): load-bearing
/ support-fraction and fragile-on-top constraints (Gzara et al. 2020; Paquay et
al. 2018), and multi-drop / LIFO unload ordering (Gimenez-Palacios et al. 2023).
The existing *incompatible-items* constraint already covers the conflict / type-
compatibility line (Goldberg & Karhi 2019; Chen et al. 2025).
