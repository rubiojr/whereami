package geometry

import (
	"fmt"
	"math"
)

// ExtrudedVertex separates a line's centerline anchor from its extrusion. The
// rendered position is Anchor + halfWidth*Direction, so one mesh serves every
// line width. Direction is a unit normal for segment edges and round cap/join
// rings, zero at disk centers and rejected miters, a normal plus or minus the
// tangent for square caps, and a miter vector of at most four half widths.
type ExtrudedVertex struct {
	Anchor, Direction Point
}

// Position applies a half width, reproducing TessellateLines' geometry.
func (v ExtrudedVertex) Position(halfWidth float64) Point {
	return Point{X: v.Anchor.X + v.Direction.X*halfWidth, Y: v.Anchor.Y + v.Direction.Y*halfWidth}
}

// ExtrudedMesh has TessellateLines' topology with width-independent vertices.
type ExtrudedMesh struct {
	Vertices []ExtrudedVertex
	Indices  []uint32
}

// ExtrudedLineStyle is the width-independent part of LineStyle. Dashes and path
// offsets depend on the evaluated width or offset and have no extruded form;
// tessellate those with TessellateLines.
type ExtrudedLineStyle struct {
	Cap, Join string
}

// TessellateExtrudedLines emits the same triangles, in the same order, as
// TessellateLines with a positive width, no dashes and no offset, but leaves
// extrusion to the consumer. Geometry is identical within floating-point
// rounding for every width, so the result stays valid while evaluated line
// widths change. Inputs are neither modified nor retained; the result owns its
// slices. maximumTriangles bounds all emitted triangles, including joins/caps.
func TessellateExtrudedLines(paths [][]Point, style ExtrudedLineStyle, maximumTriangles int, indexed bool) (ExtrudedMesh, error) {
	if maximumTriangles < 0 || maximumTriangles > MaxLineTriangles {
		return ExtrudedMesh{}, fmt.Errorf("%w: invalid line triangle limit", ErrGeometryLimit)
	}
	mesh := NewBuilder[ExtrudedVertex](indexed, maximumTriangles*3)
	capacity := lineVertexCapacity(paths, LineStyle{Cap: style.Cap, Join: style.Join}, maximumTriangles)
	if indexed {
		mesh.Indices = make([]uint32, 0, capacity)
		capacity /= 2
	}
	mesh.Vertices = make([]ExtrudedVertex, 0, capacity)
	for _, rawPath := range paths {
		path := cleanLine(rawPath)
		if len(path) < 2 {
			continue
		}
		for index := 1; index < len(path); index++ {
			if err := appendExtrudedSegment(&mesh, path[index-1], path[index], style.Cap); err != nil {
				return ExtrudedMesh{}, err
			}
		}
		if err := appendExtrudedJoins(path, style, &mesh); err != nil {
			return ExtrudedMesh{}, err
		}
	}
	return ExtrudedMesh{Vertices: mesh.Vertices, Indices: mesh.Indices}, nil
}

func appendExtrudedSegment(mesh *Builder[ExtrudedVertex], start, end Point, cap string) error {
	deltaX := end.X - start.X
	deltaY := end.Y - start.Y
	length := math.Hypot(deltaX, deltaY)
	if length <= Epsilon {
		return nil
	}
	tangent := Point{X: deltaX / length, Y: deltaY / length}
	normal := Point{X: -deltaY / length, Y: deltaX / length}
	var back, forward Point
	if cap == "square" {
		back, forward = Point{X: -tangent.X, Y: -tangent.Y}, tangent
	}
	return mesh.Append([]ExtrudedVertex{
		{Anchor: start, Direction: Point{X: back.X + normal.X, Y: back.Y + normal.Y}},
		{Anchor: start, Direction: Point{X: back.X - normal.X, Y: back.Y - normal.Y}},
		{Anchor: end, Direction: Point{X: forward.X + normal.X, Y: forward.Y + normal.Y}},
		{Anchor: end, Direction: Point{X: forward.X - normal.X, Y: forward.Y - normal.Y}},
	}, []uint32{0, 1, 2, 2, 1, 3})
}

func appendExtrudedJoins(path []Point, style ExtrudedLineStyle, mesh *Builder[ExtrudedVertex]) error {
	for index := 1; index+1 < len(path); index++ {
		var err error
		if style.Join == "round" {
			err = appendExtrudedDisk(mesh, path[index])
		} else {
			err = appendExtrudedMiter(path[index-1], path[index], path[index+1], mesh)
		}
		if err != nil {
			return err
		}
	}
	if style.Cap == "round" && path[0] != path[len(path)-1] {
		if err := appendExtrudedDisk(mesh, path[0]); err != nil {
			return err
		}
		return appendExtrudedDisk(mesh, path[len(path)-1])
	}
	return nil
}

func appendExtrudedDisk(mesh *Builder[ExtrudedVertex], center Point) error {
	var vertices [DiskSections + 1]ExtrudedVertex
	vertices[0] = ExtrudedVertex{Anchor: center}
	for i := range DiskSections {
		vertices[i+1] = ExtrudedVertex{Anchor: center, Direction: diskDirections[i]}
	}
	var indices [DiskSections * 3]uint32
	for i := range DiskSections {
		indices[i*3+1], indices[i*3+2] = uint32(i+1), uint32((i+1)%DiskSections+1)
	}
	return mesh.Append(vertices[:], indices[:])
}

// appendExtrudedMiter is appendMiterJoin at half width one. The miter vector and
// its four-half-width fallback scale linearly with width, so both are decided here.
func appendExtrudedMiter(previous, point, next Point, mesh *Builder[ExtrudedVertex]) error {
	firstX, firstY := point.X-previous.X, point.Y-previous.Y
	secondX, secondY := next.X-point.X, next.Y-point.Y
	firstLength := math.Hypot(firstX, firstY)
	secondLength := math.Hypot(secondX, secondY)
	if firstLength <= Epsilon || secondLength <= Epsilon {
		return nil
	}
	firstX, firstY = firstX/firstLength, firstY/firstLength
	secondX, secondY = secondX/secondLength, secondY/secondLength
	cross := firstX*secondY - firstY*secondX
	if math.Abs(cross) <= Epsilon {
		return nil
	}
	for _, side := range []float64{-1, 1} {
		firstCorner := Point{X: -firstY * side, Y: firstX * side}
		secondCorner := Point{X: -secondY * side, Y: secondX * side}
		deltaX := secondCorner.X - firstCorner.X
		deltaY := secondCorner.Y - firstCorner.Y
		factor := (deltaX*secondY - deltaY*secondX) / cross
		miter := Point{X: firstCorner.X + firstX*factor, Y: firstCorner.Y + firstY*factor}
		if math.Hypot(miter.X, miter.Y) > 4 {
			miter = Point{}
		}
		if err := mesh.Append([]ExtrudedVertex{{Anchor: point, Direction: firstCorner}, {Anchor: point, Direction: miter}, {Anchor: point, Direction: secondCorner}}, nil); err != nil {
			return err
		}
	}
	return nil
}
