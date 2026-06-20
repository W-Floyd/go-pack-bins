package d3

import (
	"fmt"
	"sync/atomic"

	"github.com/W-Floyd/go-pack-bins/geometry"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// SolidPlacement3D records where and in which orientation a solid item was placed.
type SolidPlacement3D struct {
	binID, itemID string
	Position      geometry.Vec3
	RotationIndex int // index into geometry.AxisAlignedOrientations()
}

func (p *SolidPlacement3D) BinID() string  { return p.binID }
func (p *SolidPlacement3D) ItemID() string { return p.itemID }

var _ pack.Placement = (*SolidPlacement3D)(nil)

// SolidBin3D is a bin whose container shape is itself an arbitrary Solid.
// It tracks occupancy via a VoxelGrid and places items by trying candidate
// positions and orientations until a collision-free placement is found.
//
// Placement search: positions are sampled on a grid with step VoxelResolution
// inside the container's bounding box. For each orientation of the item, the
// item's voxel grid is checked against the bin's occupied-voxel grid.
//
// This is computationally expensive for large bins or many items. For box-
// shaped containers and box items, prefer Bin3D with ExtremePoint.
type SolidBin3D struct {
	id              string
	container       Solid
	voxRes          float64
	containerVox    *VoxelGrid // interior voxels of the container
	occupiedVox     *VoxelGrid // voxels occupied by placed items (same grid as containerVox)
	usedVol         float64
	items           []pack.Item
	orientations    []geometry.Mat3x3 // 24 axis-aligned rotations, lazily initialised
}

// NewSolidBin creates a SolidBin3D with the given container solid and voxel resolution.
func NewSolidBin(id string, container Solid, voxRes float64) *SolidBin3D {
	cv := container.Voxelize(voxRes)
	occ := newVoxelGrid(cv.NX, cv.NY, cv.NZ, voxRes, cv.Origin)
	return &SolidBin3D{
		id:           id,
		container:    container,
		voxRes:       voxRes,
		containerVox: cv,
		occupiedVox:  occ,
	}
}

func (b *SolidBin3D) ID() string      { return b.id }
func (b *SolidBin3D) Dimensions() int { return 3 }

func (b *SolidBin3D) TryPlace(item pack.Item) (pack.Placement, error) {
	si, ok := item.(*SolidItem3D)
	if !ok {
		return nil, pack.ErrNoRoom
	}
	if b.orientations == nil {
		b.orientations = geometry.AxisAlignedOrientations()
	}

	itemBBox := si.solid.AABB()
	conBBox := b.container.AABB()
	itemVox := si.solid.Voxelize(si.VoxelResolution)

	// Track whether any rotation's bounding box fit in the container bbox.
	anyBBoxFit := false

	for _, rotIdx := range si.AllowRotations {
		rot := b.orientations[int(rotIdx)]
		// Compute bounding box of the rotated item.
		rotBBox := rotatedBBox(itemBBox, rot)
		rw := rotBBox.W()
		rd := rotBBox.D()
		rh := rotBBox.H()

		// Quick rejection: rotated item doesn't fit in container bounding box.
		if rw > conBBox.W() || rd > conBBox.D() || rh > conBBox.H() {
			continue
		}
		anyBBoxFit = true

		// Sample placement positions on a grid.
		xMax := conBBox.W() - rw
		yMax := conBBox.D() - rd
		zMax := conBBox.H() - rh

		for iz := 0.0; iz <= zMax; iz += b.voxRes {
			for iy := 0.0; iy <= yMax; iy += b.voxRes {
				for ix := 0.0; ix <= xMax; ix += b.voxRes {
					pos := geometry.Vec3{
						X: conBBox.Min.X + ix,
						Y: conBBox.Min.Y + iy,
						Z: conBBox.Min.Z + iz,
					}
					translate := pos.Sub(rotBBox.Min)
					if b.canPlace(si.solid, rot, translate, itemVox, si.VoxelResolution) {
						b.commitPlace(si.solid, rot, translate, itemVox, si.VoxelResolution)
						b.usedVol += si.Volume()
						b.items = append(b.items, item)
						return &SolidPlacement3D{
							binID:         b.id,
							itemID:        item.ID(),
							Position:      pos,
							RotationIndex: int(rotIdx),
						}, nil
					}
				}
			}
		}
	}
	if !anyBBoxFit {
		return nil, pack.ErrItemTooLarge
	}
	return nil, pack.ErrNoRoom
}

// canPlace checks whether the item (after rotation+translation) fits inside the
// container without overlapping already-placed items.
func (b *SolidBin3D) canPlace(s Solid, rot geometry.Mat3x3, translate geometry.Vec3, itemVox *VoxelGrid, res float64) bool {
	// Compute the offset in voxel coordinates.
	ox := int((translate.X - b.containerVox.Origin.X) / b.voxRes)
	oy := int((translate.Y - b.containerVox.Origin.Y) / b.voxRes)
	oz := int((translate.Z - b.containerVox.Origin.Z) / b.voxRes)

	// Every item voxel must be inside the container and not already occupied.
	for iz := 0; iz < itemVox.NZ; iz++ {
		for iy := 0; iy < itemVox.NY; iy++ {
			for ix := 0; ix < itemVox.NX; ix++ {
				if !itemVox.Get(ix, iy, iz) {
					continue
				}
				gx, gy, gz := ix+ox, iy+oy, iz+oz
				if !b.containerVox.Get(gx, gy, gz) {
					return false // outside container
				}
				if b.occupiedVox.Get(gx, gy, gz) {
					return false // already occupied
				}
			}
		}
	}
	return true
}

func (b *SolidBin3D) commitPlace(s Solid, rot geometry.Mat3x3, translate geometry.Vec3, itemVox *VoxelGrid, res float64) {
	ox := int((translate.X - b.occupiedVox.Origin.X) / b.voxRes)
	oy := int((translate.Y - b.occupiedVox.Origin.Y) / b.voxRes)
	oz := int((translate.Z - b.occupiedVox.Origin.Z) / b.voxRes)
	for iz := 0; iz < itemVox.NZ; iz++ {
		for iy := 0; iy < itemVox.NY; iy++ {
			for ix := 0; ix < itemVox.NX; ix++ {
				if itemVox.Get(ix, iy, iz) {
					b.occupiedVox.Set(ix+ox, iy+oy, iz+oz)
				}
			}
		}
	}
}

func (b *SolidBin3D) Utilization() float64 {
	total := float64(b.containerVox.OccupiedCount())
	if total == 0 {
		return 1
	}
	return float64(b.occupiedVox.OccupiedCount()) / total
}

func (b *SolidBin3D) Remaining() float64 {
	total := float64(b.containerVox.OccupiedCount()) * b.voxRes * b.voxRes * b.voxRes
	occ := float64(b.occupiedVox.OccupiedCount()) * b.voxRes * b.voxRes * b.voxRes
	return total - occ
}

func (b *SolidBin3D) Items() []pack.Item { return b.items }

var _ pack.Bin = (*SolidBin3D)(nil)

// rotatedBBox returns the AABB of the solid's bbox corners after rotation.
func rotatedBBox(bbox geometry.BBox3, rot geometry.Mat3x3) geometry.BBox3 {
	corners := [8]geometry.Vec3{
		{bbox.Min.X, bbox.Min.Y, bbox.Min.Z},
		{bbox.Max.X, bbox.Min.Y, bbox.Min.Z},
		{bbox.Min.X, bbox.Max.Y, bbox.Min.Z},
		{bbox.Max.X, bbox.Max.Y, bbox.Min.Z},
		{bbox.Min.X, bbox.Min.Y, bbox.Max.Z},
		{bbox.Max.X, bbox.Min.Y, bbox.Max.Z},
		{bbox.Min.X, bbox.Max.Y, bbox.Max.Z},
		{bbox.Max.X, bbox.Max.Y, bbox.Max.Z},
	}
	r0 := rot.MulVec(corners[0])
	min, max := r0, r0
	for _, c := range corners[1:] {
		rc := rot.MulVec(c)
		if rc.X < min.X { min.X = rc.X }
		if rc.Y < min.Y { min.Y = rc.Y }
		if rc.Z < min.Z { min.Z = rc.Z }
		if rc.X > max.X { max.X = rc.X }
		if rc.Y > max.Y { max.Y = rc.Y }
		if rc.Z > max.Z { max.Z = rc.Z }
	}
	return geometry.NewBBox3(min, max)
}

// SolidBinFactory creates SolidBin3D instances.
type SolidBinFactory struct {
	container Solid
	voxRes    float64
	counter   atomic.Int64
}

// NewSolidBinFactory creates a factory that produces SolidBin3D bins.
func NewSolidBinFactory(container Solid, voxRes float64) *SolidBinFactory {
	return &SolidBinFactory{container: container, voxRes: voxRes}
}

func (f *SolidBinFactory) Open() pack.Bin {
	n := f.counter.Add(1)
	return NewSolidBin(fmt.Sprintf("solidbin-%d", n), f.container, f.voxRes)
}

var _ pack.BinFactory = (*SolidBinFactory)(nil)
