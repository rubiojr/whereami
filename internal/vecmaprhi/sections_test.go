//go:build vecmap_rhi && integration

package vecmaprhi

import (
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testVertexSections renders fills, an extruded line and a dashed line from one
// mesh whose vertices carry every attribute, and from one that stores them in
// sections, float or packed. Draws alternate between the sections. Float
// sections must draw the identical picture; packed ones round line directions
// to 1/4096, which may move a few edge pixels. The sectioned mesh is one upload
// of fewer bytes.
func testVertexSections(t *testing.T, indexed, packed bool) {
	t.Helper()
	const scale, width = 2.0, 4.0
	paths := [][]geometry.Point{{{X: 12, Y: 12}, {X: 40, Y: 14}, {X: 44, Y: 36}}}
	line, err := geometry.TessellateExtrudedLines(paths, geometry.ExtrudedLineStyle{Cap: "round", Join: "round"}, 1024, indexed)
	require.NoError(t, err)
	dashes, err := geometry.TessellateDashedLines([][]geometry.Point{{{X: 6, Y: 44}, {X: 50, Y: 30}}}, 1024, indexed)
	require.NoError(t, err)
	pattern, ok := geometry.NewDashPattern([]float64{2, 1}, width)
	require.True(t, ok)
	quad := func(x, y float32) ([]scene.PositionVertex, []uint32) {
		corners := []scene.PositionVertex{{X: x, Y: y}, {X: x + 14, Y: y}, {X: x + 14, Y: y + 10}, {X: x, Y: y + 10}}
		if indexed {
			return corners, []uint32{0, 1, 2, 0, 2, 3}
		}
		return []scene.PositionVertex{corners[0], corners[1], corners[2], corners[0], corners[2], corners[3]}, nil
	}

	green, blue, red := scene.Material{Color: [4]float32{0, 1, 0, 1}}, scene.Material{Color: [4]float32{0, 0, 1, 1}}, [4]float32{1, 0, 0, 1}
	extruded := scene.Material{Color: red, MapAligned: true, OffsetScale: float32(width / 2 * scale)}
	dashed := scene.Material{Kind: scene.Dashed, Color: red, MapAligned: true, OffsetScale: float32(width / 2 * scale), DashUnit: float32(width)}
	for i, dash := range pattern {
		dashed.Dashes[i] = float32(dash)
	}

	full := scene.Mesh{ID: 1, Revision: 1}
	sections := scene.Mesh{ID: 2, Revision: 1}
	var fullDraws, sectionDraws []scene.Draw
	// add appends one draw to both meshes. Indices count from the start of the
	// section; the sectioned mesh orders its index buffer by layout below.
	var sectionIndices [scene.LayoutCount][]uint32
	var sectionFirst [scene.LayoutCount][]int // draws of each layout, by position in sectionDraws
	add := func(layout scene.Layout, material scene.Material, vertices []scene.Vertex, indices []uint32) {
		if packed {
			layout = map[scene.Layout]scene.Layout{scene.FullLayout: scene.PackedDashedLayout, scene.OffsetLayout: scene.PackedOffsetLayout,
				scene.PositionLayout: scene.PackedPositionLayout}[layout]
		}
		count := len(vertices)
		if indexed {
			count = len(indices)
		}
		first := len(full.Vertices)
		if indexed {
			first = len(full.Indices)
			for _, index := range indices {
				full.Indices = append(full.Indices, uint32(len(full.Vertices))+index)
			}
		}
		full.Vertices = append(full.Vertices, vertices...)
		fullDraws = append(fullDraws, scene.Draw{Mesh: 1, First: uint32(first), Count: uint32(count), Material: material})

		base := sections.Len(layout)
		first = base
		if indexed {
			first = len(sectionIndices[layout])
			for _, index := range indices {
				sectionIndices[layout] = append(sectionIndices[layout], uint32(base)+index)
			}
		}
		for _, v := range vertices {
			x, y, ok := scene.PackPosition(float64(v.X), float64(v.Y))
			require.True(t, ok)
			dx, dy, ok := scene.PackOffset(float64(v.OffsetX), float64(v.OffsetY))
			require.True(t, ok)
			switch layout {
			case scene.PackedPositionLayout:
				sections.PackedPositions = append(sections.PackedPositions, scene.PackedPositionVertex{X: x, Y: y})
			case scene.PackedOffsetLayout:
				sections.PackedOffsets = append(sections.PackedOffsets, scene.PackedOffsetVertex{X: x, Y: y, OffsetX: dx, OffsetY: dy})
			case scene.PackedDashedLayout:
				sections.PackedDashed = append(sections.PackedDashed, scene.PackedDashedVertex{X: x, Y: y, OffsetX: dx, OffsetY: dy, U: v.U})
			case scene.OffsetLayout:
				sections.Offsets = append(sections.Offsets, scene.OffsetVertex{X: v.X, Y: v.Y, OffsetX: v.OffsetX, OffsetY: v.OffsetY})
			case scene.PositionLayout:
				sections.Positions = append(sections.Positions, scene.PositionVertex{X: v.X, Y: v.Y})
			default:
				sections.Vertices = append(sections.Vertices, v)
			}
		}
		sectionFirst[layout] = append(sectionFirst[layout], len(sectionDraws))
		sectionDraws = append(sectionDraws, scene.Draw{Mesh: 2, First: uint32(first), Count: uint32(count), Material: material, Layout: layout})
	}
	fill := func(x, y float32, material scene.Material) {
		positions, indices := quad(x, y)
		vertices := make([]scene.Vertex, len(positions))
		for i, p := range positions {
			vertices[i] = scene.Vertex{X: p.X, Y: p.Y}
		}
		add(scene.PositionLayout, material, vertices, indices)
	}
	fill(4, 4, green)
	lineVertices := make([]scene.Vertex, len(line.Vertices))
	for i, v := range line.Vertices {
		lineVertices[i] = scene.Vertex{X: float32(v.Anchor.X), Y: float32(v.Anchor.Y), OffsetX: float32(v.Direction.X), OffsetY: float32(v.Direction.Y)}
	}
	add(scene.OffsetLayout, extruded, lineVertices, line.Indices)
	fill(30, 20, blue)
	dashVertices := make([]scene.Vertex, len(dashes.Vertices))
	for i, v := range dashes.Vertices {
		dashVertices[i] = scene.Vertex{X: float32(v.Anchor.X), Y: float32(v.Anchor.Y), OffsetX: float32(v.Direction.X), OffsetY: float32(v.Direction.Y), U: float32(v.Distance)}
	}
	add(scene.FullLayout, dashed, dashVertices, dashes.Indices)
	fill(8, 28, green)
	if indexed {
		for layout := range sectionIndices {
			for _, draw := range sectionFirst[layout] {
				sectionDraws[draw].First += uint32(len(sections.Indices))
			}
			sections.Indices = append(sections.Indices, sectionIndices[layout]...)
		}
	}
	plain := &scene.Scene{Meshes: []scene.Mesh{full}, Draws: fullDraws}
	compact := &scene.Scene{Meshes: []scene.Mesh{sections}, Draws: sectionDraws}
	require.NoError(t, plain.Validate())
	require.NoError(t, compact.Validate())
	require.Less(t, sections.BufferBytes(), full.BufferBytes())
	if packed {
		require.NotEmpty(t, sections.PackedPositions)
		require.NotEmpty(t, sections.PackedOffsets)
		require.NotEmpty(t, sections.PackedDashed)
	} else {
		require.NotEmpty(t, sections.Vertices)
		require.NotEmpty(t, sections.Offsets)
		require.NotEmpty(t, sections.Positions)
	}

	sin, cos := math.Sincos(12 * math.Pi / 180)
	transform := scene.Affine{M11: float32(scale * cos), M12: float32(-scale * sin), DX: 20, M21: float32(scale * sin), M22: float32(scale * cos), DY: 4}
	grab, stats, done := sceneGrabber(t, plain, transform)
	defer done()
	reference := grab(plain)
	defer reference.Delete()
	uploads, bytes := stats.MeshUploads, stats.UploadedBytes
	sectioned := grab(compact)
	defer sectioned.Delete()
	assert.Equal(t, uploads+1, stats.MeshUploads, "sections are one resource")
	assert.Equal(t, bytes+sections.BufferBytes(), stats.UploadedBytes)

	colors := map[[3]int]int{}
	different, drawn := 0, 0
	for y := range reference.Height() {
		for x := range reference.Width() {
			a, b := reference.PixelColor(x, y), sectioned.PixelColor(x, y)
			if a.Red() != b.Red() || a.Green() != b.Green() || a.Blue() != b.Blue() {
				different++
			}
			if a.Red() > 127 || a.Green() > 127 || a.Blue() > 127 {
				colors[[3]int{a.Red() / 128, a.Green() / 128, a.Blue() / 128}]++
				drawn++
			}
		}
	}
	t.Logf("indexed=%t packed=%t: %d of %d drawn pixels differ", indexed, packed, different, drawn)
	if packed {
		assert.LessOrEqual(t, different, drawn/200, "quantized directions move at most a few edge pixels")
	} else {
		assert.Zero(t, different, "the same vertices draw the same pixels")
	}
	assert.Greater(t, colors[[3]int{0, 1, 0}], 300, "fills are drawn")
	assert.Greater(t, colors[[3]int{0, 0, 1}], 300)
	assert.Greater(t, colors[[3]int{1, 0, 0}], 300, "lines are drawn")
}
