package offline

import (
	"github.com/wfloyd/go-pack-bins/online"
	"github.com/wfloyd/go-pack-bins/pack"
)

// NextFitDecreasing returns an NFD offline packer.
// Items are sorted by decreasing volume, then Next Fit is applied.
// Its asymptotic ratio is slightly less than 1.7 in the worst case.
// Note: Next-Fit packs a list and its reverse into the same number of bins,
// so NFD and Next-Fit-Increasing (NFI) have identical performance.
func NextFitDecreasing(factory pack.BinFactory) *Wrapper {
	return New("NFD", DecreasingVolume, online.NextFit(factory))
}
