package geometry

import "fmt"

// MaxLineTriangles is the existing per-tile rendered-geometry policy.
const MaxLineTriangles = 2_000_000

// LineStyle contains evaluated, tile-local paint rather than style expressions.
// Dash lengths are multiples of Width; Cap is butt/square/round and Join is
// miter/round. Unknown cap/join values retain the existing butt/miter fallback.
type LineStyle struct {
	Width, Offset float64
	Dashes        []float64
	Cap, Join     string
}

// TessellateLines reuses vecmap's bounded CPU line preparation. Input positions
// and paint must be finite, as validated by the decoder/style evaluator. Input
// paths and dash arrays are never modified or retained. The result owns its
// slices; publish only after success. maximumTriangles bounds all emitted
// triangles, including joins/caps, and also bounds dash work per path.
func TessellateLines(paths [][]Point, style LineStyle, maximumTriangles int, indexed bool) (Mesh, error) {
	if maximumTriangles < 0 || maximumTriangles > MaxLineTriangles {
		return Mesh{}, fmt.Errorf("%w: invalid line triangle limit", ErrGeometryLimit)
	}
	mesh := NewBuilder[Point](indexed, maximumTriangles*3)
	capacity := lineVertexCapacity(paths, style, maximumTriangles)
	if indexed {
		mesh.Indices = make([]uint32, 0, capacity)
		capacity /= 2
	}
	mesh.Vertices = make([]Point, 0, capacity)
	if err := appendLines(paths, style, maximumTriangles, &mesh); err != nil {
		return Mesh{}, err
	}
	return Mesh{Vertices: mesh.Vertices, Indices: mesh.Indices}, nil
}

func appendLines(paths [][]Point, style LineStyle, maximumTriangles int, mesh *Builder[Point]) error {
	if style.Width <= Epsilon {
		return nil
	}
	for _, rawPath := range paths {
		path := cleanLine(rawPath)
		if len(path) < 2 {
			continue
		}
		path = OffsetLine(path, style.Offset)
		segments, err := dashedSegments(path, style.Dashes, style.Width, maximumTriangles)
		if err != nil {
			return err
		}
		if len(segments) == 0 {
			continue
		}
		for _, segment := range segments {
			if err := AppendLineSegment(mesh, segment.Start, segment.End, style.Width, style.Cap, len(style.Dashes) > 0); err != nil {
				return err
			}
		}
		if len(style.Dashes) == 0 {
			if err := appendLineJoins(path, style, mesh); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendLineJoins(path []Point, style LineStyle, mesh *Builder[Point]) error {
	for index := 1; index+1 < len(path); index++ {
		var err error
		if style.Join == "round" {
			err = AppendDisk(mesh, path[index], style.Width/2)
		} else {
			err = appendMiterJoin(path[index-1], path[index], path[index+1], style.Width/2, mesh)
		}
		if err != nil {
			return err
		}
	}
	if style.Cap == "round" && path[0] != path[len(path)-1] {
		if err := AppendDisk(mesh, path[0], style.Width/2); err != nil {
			return err
		}
		return AppendDisk(mesh, path[len(path)-1], style.Width/2)
	}
	return nil
}

func lineVertexCapacity(paths [][]Point, style LineStyle, maximumTriangles int) int {
	if len(style.Dashes) > 0 {
		return 0
	}
	limit := maximumTriangles * 3
	vertices := 0
	for _, path := range paths {
		points := len(path)
		if points < 2 {
			continue
		}
		vertices = addLineCapacity(vertices, points-1, 6, limit)
		if style.Join == "round" && points > 2 {
			vertices = addLineCapacity(vertices, points-2, DiskSections*3, limit)
		}
		if style.Cap == "round" && path[0] != path[points-1] {
			vertices = addLineCapacity(vertices, 2, DiskSections*3, limit)
		}
		if vertices == limit {
			return limit
		}
	}
	return vertices
}

// Saturate before multiplying so a caller's repeated/shared input paths cannot
// overflow an allocation estimate, including on 32-bit Go targets.
func addLineCapacity(current, count, perPoint, limit int) int {
	if count > (limit-current)/perPoint {
		return limit
	}
	return current + count*perPoint
}

func cleanLine(path []Point) []Point {
	cleaned := make([]Point, 0, len(path))
	for _, point := range path {
		if len(cleaned) == 0 || cleaned[len(cleaned)-1] != point {
			cleaned = append(cleaned, point)
		}
	}
	return cleaned
}
