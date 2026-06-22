# bench — go-pack-bins vs boxpacker3 vs bp3d

A head-to-head benchmark of [go-pack-bins](../) against
[bavix/boxpacker3](https://github.com/bavix/boxpacker3) and
[gedex/bp3d](https://github.com/gedex/bp3d) on identical 3-D packing instances,
reporting both **result quality** (bins used, fill rate, unplaced items) and
**wall-clock time**.

This is a **separate module** on purpose: it depends on boxpacker3 and bp3d,
while the main go-pack-bins module stays dependency-free. The `replace` directive
points at the parent so it always builds the local working tree.

## Run

```sh
cd bench
go run .                      # default: 20, 50, 100, 300 items
go run . -items 50,200,800    # custom item counts
go run . -runs 5              # timing runs per solve (minimum reported)
go run . -seed 7              # different reproducible instances
```

## What it measures

All three engines get the same items and the same single box size. go-pack-bins
and boxpacker3 are each asked to minimise box count (FFD) and, separately,
best-fit-decreasing — paired so the columns are directly comparable. bp3d offers
a single volume-descending first-fit strategy, so it is reported once, on the FFD
row (its natural analog). All consider all six rotations and identical volumes,
so the comparison is fair on what matters: how many boxes, how full, how fast.

`fill%` = packed item volume ÷ (bins × box volume); higher is tighter.

## Notes

- The libraries interpret box axes differently internally; using a cube-ish box
  and small items keeps the geometric comparison even-handed.
- boxpacker3 and bp3d are each given one candidate box per item (an upper bound)
  and a huge `maxWeight`, so weight never gates a purely geometric run. bp3d packs
  into a fixed bin list, spilling each full bin into the next identical one, so
  its non-empty bins are counted as boxes used.
- bp3d ([gedex/bp3d](https://github.com/gedex/bp3d)) is an older, pre-modules
  package with no `go.mod`; Go resolves it via a synthesized pseudo-version.
