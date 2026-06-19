package d3

import "github.com/wfloyd/go-pack-bins/pack"

// SolidItem3D is a 3-D item with an arbitrary manifold solid shape.
// Placement is performed via voxel-grid collision detection.
// VoxelResolution controls the cell size (world units) used for voxelization;
// smaller values are more accurate but more expensive.
type SolidItem3D struct {
	id              string
	solid           Solid
	VoxelResolution float64
	// AllowRotations lists rotation matrices to try during placement.
	// If nil, only the identity (no rotation) is tried.
	AllowRotations []AxisRotation
}

// AxisRotation is one of the 24 axis-aligned rotation indices.
// Use geometry.AxisAlignedOrientations() to enumerate them.
type AxisRotation int

// NewSolidItem creates a SolidItem3D.
// resolution is the voxel cell size in world units.
// rotate=true enables all 24 axis-aligned orientations.
func NewSolidItem(id string, solid Solid, resolution float64, rotate bool) *SolidItem3D {
	item := &SolidItem3D{id: id, solid: solid, VoxelResolution: resolution}
	if rotate {
		for i := 0; i < 24; i++ {
			item.AllowRotations = append(item.AllowRotations, AxisRotation(i))
		}
	} else {
		item.AllowRotations = []AxisRotation{0}
	}
	return item
}

func (i *SolidItem3D) ID() string      { return i.id }
func (i *SolidItem3D) Dimensions() int { return 3 }

func (i *SolidItem3D) Volume() float64 {
	return i.solid.Volume()
}

// Solid returns the underlying shape.
func (i *SolidItem3D) Solid() Solid { return i.solid }

var _ pack.Item = (*SolidItem3D)(nil)
