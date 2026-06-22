package d3

import (
	"fmt"
	"sort"

	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// LAFF packs 3-D items into containers using the Largest-Area-Fit-First layered
// heuristic popularised by skjolber/3d-bin-container-packing (Apache-2.0; see ATTRIBUTION.md for the pinned commit), in
// turn based on "An Efficient Algorithm for 3D Rectangular Box Packing".
//
// Items are stacked in horizontal levels. The item with the largest footprint
// that fits opens a level and fixes its height; remaining items short enough for
// that level are packed into its floor area (a 2-D packing, solved here with the
// MaxRects engine from package d2). When no more items fit the level, a new level
// starts above it; when the container height is used up, a new container opens.
//
// Rotatable items are laid flat (smallest dimension vertical) to maximise ground
// coverage, the classic LAFF orientation; the footprint may still rotate in-plane.
// Items too large for an empty container are returned as unplaced.
func LAFF(items []pack.Item, w, depth, h float64) (pack.Result, error) {
	const eps = 1e-9
	var result pack.Result

	// Split into placeable 3-D items and immediately-unplaceable ones.
	remaining := make([]*Item3D, 0, len(items))
	for _, raw := range items {
		it, ok := raw.(*Item3D)
		if !ok {
			result.Unplaced = append(result.Unplaced, raw.ID())
			continue
		}
		fw, fd, fh := laffOrient(it)
		footFits := (fw <= w+eps && fd <= depth+eps) ||
			(it.AllowRotate && fw <= depth+eps && fd <= w+eps)
		if !footFits || fh > h+eps {
			result.Unplaced = append(result.Unplaced, it.ID())
			continue
		}
		remaining = append(remaining, it)
	}

	binIdx := 0
	for len(remaining) > 0 {
		binID := fmt.Sprintf("laff-bin-%d", binIdx)
		curZ := 0.0
		placedInBin := 0

		for curZ < h-eps && len(remaining) > 0 {
			placements, levelHeight, leftover := packLevel(binID, remaining, w, depth, h-curZ, curZ)
			if len(placements) == 0 {
				break // nothing fits in the remaining height — container is done
			}
			result.Placements = append(result.Placements, placements...)
			placedInBin += len(placements)
			remaining = leftover
			curZ += levelHeight
		}

		if placedInBin == 0 {
			break // should not happen (all remaining are pre-filtered as fittable)
		}
		// A bookkeeping bin so Result.BinsUsed and bin indices are correct; the
		// authoritative geometry lives in the Placement3D entries above.
		result.Bins = append(result.Bins, NewBin(binID, w, depth, h, NewExtremePointStrategy(w, depth, h)))
		binIdx++
	}
	return result, nil
}

// packLevel fills one level: it seeds the level with the largest-footprint item
// that fits maxHeight, fixes the level height, then packs the remaining items
// short enough for that level into the level's floor (2-D MaxRects). It returns
// the level's placements (at z=baseZ), the level height, and the items not placed.
func packLevel(binID string, items []*Item3D, w, depth, maxHeight, baseZ float64) ([]pack.Placement, float64, []*Item3D) {
	const eps = 1e-9

	// Largest footprint area first — the seed is the first that fits maxHeight.
	order := make([]*Item3D, len(items))
	copy(order, items)
	sort.SliceStable(order, func(i, j int) bool {
		return footArea(order[i]) > footArea(order[j])
	})

	levelHeight := 0.0
	seeded := false
	bin2d := d2.NewBin(binID, w, depth, d2.NewMaxRectsDefault(w, depth))

	placedIDs := map[string]bool{}
	var placements []pack.Placement

	for _, it := range order {
		_, _, fh := laffOrient(it)
		if fh > maxHeight+eps {
			continue // too tall for the remaining container height
		}
		if seeded && fh > levelHeight+eps {
			continue // taller than this level — leave it to seed a later level
		}
		fw, fd, _ := laffOrient(it)
		p, err := bin2d.TryPlace(d2.NewItem(it.ID(), fw, fd, it.AllowRotate))
		if err != nil {
			continue // footprint doesn't fit the remaining floor — try a later level
		}
		pl2, ok := p.(*d2.Placement2D)
		if !ok {
			continue
		}
		if !seeded {
			levelHeight = fh
			seeded = true
		}
		placements = append(placements, &Placement3D{
			binID: binID, itemID: it.ID(),
			X: pl2.X, Y: pl2.Y, Z: baseZ,
			W: pl2.W, D: pl2.H, H: fh, // pl2.W/H are the (possibly rotated) footprint extents
		})
		placedIDs[it.ID()] = true
	}

	leftover := items[:0:0]
	for _, it := range items {
		if !placedIDs[it.ID()] {
			leftover = append(leftover, it)
		}
	}
	return placements, levelHeight, leftover
}

// laffOrient returns the laid-flat orientation (footprint fw×fd, height fh). A
// rotatable item lies with its smallest dimension vertical to maximise ground
// area; a fixed item keeps its natural orientation.
func laffOrient(it *Item3D) (fw, fd, fh float64) {
	if !it.AllowRotate {
		return it.W, it.D, it.H
	}
	d := []float64{it.W, it.D, it.H}
	sort.Float64s(d) // d[0] ≤ d[1] ≤ d[2]
	return d[2], d[1], d[0]
}

func footArea(it *Item3D) float64 {
	fw, fd, _ := laffOrient(it)
	return fw * fd
}
