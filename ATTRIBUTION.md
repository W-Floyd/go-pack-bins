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
