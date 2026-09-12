package geometry

import "math"

// AppendLineSegment reuses the compiler's CPU extrusion and cap arithmetic.
// It emits one four-vertex quad, followed by round caps for dashed segments.
// Non-dashed round caps are emitted at path endpoints by the caller.
func AppendLineSegment(mesh *Builder[Point], start, end Point, width float64, cap string, dashed bool) error {
	deltaX := end.X - start.X
	deltaY := end.Y - start.Y
	length := math.Hypot(deltaX, deltaY)
	if length <= Epsilon {
		return nil
	}
	if cap == "square" {
		extensionX := deltaX / length * width / 2
		extensionY := deltaY / length * width / 2
		start.X -= extensionX
		start.Y -= extensionY
		end.X += extensionX
		end.Y += extensionY
	}
	normalX := -deltaY / length * width / 2
	normalY := deltaX / length * width / 2
	first := Point{X: start.X + normalX, Y: start.Y + normalY}
	second := Point{X: start.X - normalX, Y: start.Y - normalY}
	third := Point{X: end.X + normalX, Y: end.Y + normalY}
	fourth := Point{X: end.X - normalX, Y: end.Y - normalY}
	if err := mesh.Append([]Point{first, second, third, fourth}, []uint32{0, 1, 2, 2, 1, 3}); err != nil {
		return err
	}
	if cap == "round" && dashed {
		if err := AppendDisk(mesh, start, width/2); err != nil {
			return err
		}
		return AppendDisk(mesh, end, width/2)
	}
	return nil
}
