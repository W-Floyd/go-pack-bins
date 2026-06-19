package offline

import (
	"github.com/wfloyd/go-pack-bins/online"
	"github.com/wfloyd/go-pack-bins/pack"
)

// FirstFitDecreasing returns an FFD offline packer.
// Items are sorted by decreasing volume, then First Fit is applied.
// FFD(I) ≤ (11/9)·OPT(I) + 6/9, and this bound is tight.
func FirstFitDecreasing(factory pack.BinFactory) *Wrapper {
	return New("FFD", DecreasingVolume, online.FirstFit(factory))
}

// BestFitDecreasing returns a BFD offline packer.
// Items are sorted by decreasing volume, then Best Fit is applied.
func BestFitDecreasing(factory pack.BinFactory) *Wrapper {
	return New("BFD", DecreasingVolume, online.BestFit(factory))
}

// WorstFitDecreasing returns a WFD offline packer.
func WorstFitDecreasing(factory pack.BinFactory) *Wrapper {
	return New("WFD", DecreasingVolume, online.WorstFit(factory))
}
