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

## Online-3D-BPP-PCT (MIT)

- Repo: https://github.com/alexfrom0815/Online-3D-BPP-PCT (commit `5e7b4238b18310af4529e0f85157d17de605850c`)
- Investigated (ICLR 2022, Zhao et al.) but **not** adopted: the core method is a
  trained deep-RL policy, impractical for a pure-Go, dependency-free library. Its
  non-ML heuristic baselines (heightmap-min, EMS scoring) remain a possible
  future addition.

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
