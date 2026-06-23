package packapi

// MeanCompactnessPct measures how void-free a packing's occupied envelope is:
// per bin, the placed items' total volume as a fraction of their axis-aligned
// bounding-box volume, averaged over the bins used. It is deliberately
// independent of how full the bin is — packed ÷ envelope, not packed ÷ bin — so
// a chronically underfilled bin does not score well merely for being empty; it
// scores well only when the items it does hold are clustered with few internal
// gaps. 100% = the envelope is solid; lower = scattered items with voids between.
// mode is "1d", "2d", or "3d". Shared by the benchmarks and cmd/render so the
// rendered sheets report the same number as the comparison table.
func MeanCompactnessPct(resp PackResponse, mode string) float64 {
	type acc struct{ minX, minY, minZ, maxX, maxY, maxZ, packed float64 }
	bins := map[int]*acc{}
	for _, p := range resp.Placements {
		a := bins[p.BinIndex]
		if a == nil {
			a = &acc{minX: p.X, minY: p.Y, minZ: p.Z}
			bins[p.BinIndex] = a
		}
		ey := axisExtentY(mode, p)
		a.minX, a.maxX = min(a.minX, p.X), max(a.maxX, p.X+p.W)
		a.minY, a.maxY = min(a.minY, p.Y), max(a.maxY, p.Y+ey)
		a.minZ, a.maxZ = min(a.minZ, p.Z), max(a.maxZ, p.Z+p.H)
		v := p.W // placed item volume, from actual (possibly rotated) dimensions
		if mode != "1d" {
			v *= ey
		}
		if mode == "3d" {
			v *= p.H
		}
		a.packed += v
	}
	if len(bins) == 0 {
		return 0
	}
	total, n := 0.0, 0
	for _, a := range bins {
		bbox := a.maxX - a.minX
		if mode != "1d" {
			bbox *= a.maxY - a.minY
		}
		if mode == "3d" {
			bbox *= a.maxZ - a.minZ
		}
		if bbox > 0 {
			total += 100 * a.packed / bbox
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return total / float64(n)
}

// axisExtentY returns a placement's extent on the second (depth/height) axis,
// which is carried in different fields per mode (2-D uses H, 3-D uses D).
func axisExtentY(mode string, p PlacementResult) float64 {
	if mode == "3d" {
		return p.D
	}
	return p.H // 2-D: the y-axis extent is the height
}
