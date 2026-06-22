package main

import (
	"image/color"
	"math"

	"github.com/fogleman/gg"
)

// Isometric (2:1) projection of algorithm coordinates (x=width, y=depth, z=height)
// to the screen. With sin30 = 0.5 the view ray is exactly (1,1,1), so the three
// faces with outward normals +x, +y, +z face the camera and are the ones drawn.
const (
	isoC = 0.86602540378443864676 // cos30
	isoS = 0.5                    // sin30
)

func proj(x, y, z float64) (sx, sy float64) {
	return (x - y) * isoC, (x+y)*isoS - z
}

// rbox is a placed box in algorithm coordinates plus its fill colour.
type rbox struct {
	x, y, z, w, d, h float64
	col              color.RGBA
}

// drawBin renders one bin (its wireframe plus boxes) into dc, fit and centred in
// the rectangle (ox,oy,bw,bh). Boxes are painted back-to-front (painter's
// algorithm along the (1,1,1) view ray) so nearer boxes occlude farther ones.
func drawBin(dc *gg.Context, ox, oy, bw, bh float64, dim [3]float64, boxes []rbox) {
	X, Y, Z := dim[0], dim[1], dim[2]
	wProj := (X + Y) * isoC
	hProj := (X+Y)*isoS + Z
	if wProj <= 0 || hProj <= 0 {
		return
	}
	scale := math.Min(bw/wProj, bh/hProj) * 0.94
	// Map the projection's min corner (−Y·c, −Z) to the centred render rect.
	tx := ox + (bw-wProj*scale)/2 + Y*isoC*scale
	ty := oy + (bh-hProj*scale)/2 + Z*scale
	P := func(x, y, z float64) (float64, float64) {
		sx, sy := proj(x, y, z)
		return tx + sx*scale, ty + sy*scale
	}
	// Stroke widths proportional to the render scale (≈the size of one bin unit).
	edgeWidth = math.Max(0.6, scale*0.05)
	wireWidth = math.Max(0.8, scale*0.07)

	drawWire(dc, P, X, Y, Z)

	for _, i := range painterOrder(boxes) {
		drawBox(dc, P, boxes[i])
	}
}

// painterOrder returns box indices in back-to-front draw order. A simple
// min-corner-sum sort is wrong for AABBs of differing sizes (a large box at the
// back can be painted over a small box in front of it). Instead, for every pair
// whose screen projections overlap, a constraint "draw the farther one first" is
// derived from the separating axis (along the (1,1,1) view ray a box is in front
// when it sits entirely on the +side of some axis), and the constraints are
// topologically ordered. Min-corner sum breaks ties and resolves any rare cycle.
func painterOrder(bs []rbox) []int {
	const eps = 1e-6
	n := len(bs)
	// Screen-space bounding box of each box's isometric projection.
	type rect struct{ x0, x1, y0, y1 float64 }
	sb := make([]rect, n)
	for i, b := range bs {
		sb[i] = rect{
			x0: (b.x - (b.y + b.d)) * isoC, x1: ((b.x + b.w) - b.y) * isoC,
			y0: (b.x+b.y)*isoS - (b.z + b.h), y1: ((b.x+b.w)+(b.y+b.d))*isoS - b.z,
		}
	}
	overlap := func(a, b int) bool {
		return sb[a].x0 < sb[b].x1-eps && sb[b].x0 < sb[a].x1-eps &&
			sb[a].y0 < sb[b].y1-eps && sb[b].y0 < sb[a].y1-eps
	}
	adj := make([][]int, n) // u -> v: draw u (behind) before v (in front)
	indeg := make([]int, n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if !overlap(i, j) {
				continue
			}
			a, b := bs[i], bs[j]
			// b is in front of a if b sits entirely on the +side of some axis.
			bFront := b.x >= a.x+a.w-eps || b.y >= a.y+a.d-eps || b.z >= a.z+a.h-eps
			aFront := a.x >= b.x+b.w-eps || a.y >= b.y+b.d-eps || a.z >= b.z+b.h-eps
			switch {
			case bFront && !aFront:
				adj[i] = append(adj[i], j)
				indeg[j]++
			case aFront && !bFront:
				adj[j] = append(adj[j], i)
				indeg[i]++
			}
		}
	}
	sum := func(i int) float64 { return bs[i].x + bs[i].y + bs[i].z }
	order := make([]int, 0, n)
	done := make([]bool, n)
	for len(order) < n {
		best := -1
		for i := 0; i < n; i++ { // ready node with the farthest min corner
			if done[i] || indeg[i] > 0 {
				continue
			}
			if best < 0 || sum(i) < sum(best) {
				best = i
			}
		}
		if best < 0 { // cycle: force the farthest remaining node
			for i := 0; i < n; i++ {
				if !done[i] && (best < 0 || sum(i) < sum(best)) {
					best = i
				}
			}
		}
		done[best] = true
		order = append(order, best)
		for _, v := range adj[best] {
			if !done[v] {
				indeg[v]--
			}
		}
	}
	return order
}

// drawBox paints the three camera-facing faces of b (top brightest, then the +x
// and +y side faces) with a thin dark edge for definition.
func drawBox(dc *gg.Context, P func(x, y, z float64) (float64, float64), b rbox) {
	x0, y0, z0 := b.x, b.y, b.z
	x1, y1, z1 := b.x+b.w, b.y+b.d, b.z+b.h
	quad(dc, P, shade(b.col, 1.18), [4][3]float64{{x0, y0, z1}, {x1, y0, z1}, {x1, y1, z1}, {x0, y1, z1}}) // top  (z+)
	quad(dc, P, shade(b.col, 0.74), [4][3]float64{{x0, y1, z0}, {x1, y1, z0}, {x1, y1, z1}, {x0, y1, z1}}) // front(y+)
	quad(dc, P, shade(b.col, 0.94), [4][3]float64{{x1, y0, z0}, {x1, y1, z0}, {x1, y1, z1}, {x1, y0, z1}}) // right(x+)
}

func quad(dc *gg.Context, P func(x, y, z float64) (float64, float64), col color.RGBA, pts [4][3]float64) {
	for i, pt := range pts {
		x, y := P(pt[0], pt[1], pt[2])
		if i == 0 {
			dc.MoveTo(x, y)
		} else {
			dc.LineTo(x, y)
		}
	}
	dc.ClosePath()
	dc.SetColor(col)
	dc.FillPreserve()
	dc.SetRGBA(0, 0, 0, 0.28)
	dc.SetLineWidth(edgeWidth)
	dc.Stroke()
}

// edgeWidth/wireWidth are the box-edge and bin-wireframe stroke widths; set from
// the render scale in drawBin so lines stay proportional at any DPI.
var edgeWidth, wireWidth = 1.2, 1.6

// drawWire strokes the 12 edges of the bin faintly so the empty container is
// visible behind the packing.
func drawWire(dc *gg.Context, P func(x, y, z float64) (float64, float64), X, Y, Z float64) {
	c := [8][3]float64{
		{0, 0, 0}, {X, 0, 0}, {X, Y, 0}, {0, Y, 0},
		{0, 0, Z}, {X, 0, Z}, {X, Y, Z}, {0, Y, Z},
	}
	edges := [12][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0}, // bottom
		{4, 5}, {5, 6}, {6, 7}, {7, 4}, // top
		{0, 4}, {1, 5}, {2, 6}, {3, 7}, // verticals
	}
	dc.SetRGBA(0.16, 0.17, 0.22, 0.35)
	dc.SetLineWidth(wireWidth)
	for _, e := range edges {
		ax, ay := P(c[e[0]][0], c[e[0]][1], c[e[0]][2])
		bx, by := P(c[e[1]][0], c[e[1]][1], c[e[1]][2])
		dc.DrawLine(ax, ay, bx, by)
		dc.Stroke()
	}
}

func shade(c color.RGBA, f float64) color.RGBA {
	cl := func(v float64) uint8 {
		switch {
		case v < 0:
			return 0
		case v > 255:
			return 255
		default:
			return uint8(v)
		}
	}
	return color.RGBA{cl(float64(c.R) * f), cl(float64(c.G) * f), cl(float64(c.B) * f), 255}
}
