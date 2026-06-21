# bench — go-pack-bins vs boxpacker3

A head-to-head benchmark of [go-pack-bins](../) against
[bavix/boxpacker3](https://github.com/bavix/boxpacker3) on identical 3-D packing
instances, reporting both **result quality** (bins used, fill rate, unplaced
items) and **wall-clock time**.

This is a **separate module** on purpose: it depends on boxpacker3, while the
main go-pack-bins module stays dependency-free. The `replace` directive points
at the parent so it always builds the local working tree.

## Run

```sh
cd bench
go run .                      # default: 20, 50, 100, 300 items
go run . -items 50,200,800    # custom item counts
go run . -runs 5              # timing runs per solve (minimum reported)
go run . -seed 7              # different reproducible instances
```

## What it measures

Both engines get the same items and the same single box size. Each is asked to
minimise box count (FFD) and, separately, best-fit-decreasing — paired so the
columns are directly comparable. Both consider all six rotations and identical
volumes, so the comparison is fair on what matters: how many boxes, how full,
how fast.

`fill%` = packed item volume ÷ (bins × box volume); higher is tighter.

## Notes

- The two libraries interpret box axes differently internally; using a cube-ish
  box and small items keeps the geometric comparison even-handed.
- boxpacker3 is given one candidate box per item (an upper bound) and a huge
  `maxWeight`, so weight never gates a purely geometric run.
