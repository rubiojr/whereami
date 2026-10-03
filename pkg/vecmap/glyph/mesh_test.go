package glyph

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quadFixture(t *testing.T) (*TextLayout, *Atlas) {
	t.Helper()
	g := bitmapGlyph(2, 2, 90)
	g.ID, g.Top, g.Advance = 'A', -2, 4
	key := Key{FontStack: "font", ID: 'A'}
	atlas, err := BuildAtlas(map[Key]Glyph{key: g})
	require.NoError(t, err)
	return &TextLayout{Glyphs: []PositionedGlyph{{Key: key, Glyph: g}}, Scale: 1}, atlas
}

func TestLayoutMeshExactTopologyUVsAndOwnership(t *testing.T) {
	layout, atlas := quadFixture(t)
	require.True(t, FitsAtlas(layout, atlas))
	expanded, err := BuildLayoutMesh(layout, atlas, false)
	require.NoError(t, err)
	indexed, err := BuildLayoutMesh(layout, atlas, true)
	require.NoError(t, err)
	assert.Equal(t, 4*len(layout.Glyphs), cap(indexed.Vertices), "room for every glyph's quad, reserved once")
	assert.Equal(t, 6*len(layout.Glyphs), cap(indexed.Indices))
	assert.Equal(t, []float32{
		-4, -2, 0, 0, 6, -2, 10.0 / 256, 0, 6, 8, 10.0 / 256, 10.0 / 256,
		-4, -2, 0, 0, 6, 8, 10.0 / 256, 10.0 / 256, -4, 8, 0, 10.0 / 256,
	}, expanded.Expanded)
	assert.Empty(t, expanded.Vertices)
	assert.Empty(t, expanded.Indices)
	assert.Empty(t, indexed.Expanded)
	require.Len(t, indexed.Vertices, 4)
	assert.Equal(t, []uint32{0, 1, 2, 0, 2, 3}, indexed.Indices)
	assertMeshExpansion(t, expanded, indexed)
	saved := slices.Clone(expanded.Expanded)
	layout.Glyphs[0].X = 999
	clear(atlas.Positions)
	clear(atlas.Pixels)
	assert.Equal(t, saved, expanded.Expanded)
	assertMeshExpansion(t, expanded, indexed)
}

func assertMeshExpansion(t *testing.T, expanded, indexed LayoutMesh) {
	t.Helper()
	require.Len(t, expanded.Expanded, len(indexed.Indices)*4)
	for i, index := range indexed.Indices {
		vertex := indexed.Vertices[index]
		for component, value := range [...]float32{vertex.X, vertex.Y, vertex.U, vertex.V} {
			assert.Equal(t, math.Float32bits(expanded.Expanded[i*4+component]), math.Float32bits(value))
		}
	}
}

func TestLayoutMeshPartialCoverageEmptyGlyphsAndNil(t *testing.T) {
	layout, atlas := quadFixture(t)
	space := PositionedGlyph{Key: Key{ID: 32}, Glyph: Glyph{ID: 32, Advance: 5}}
	layout.Glyphs = append(layout.Glyphs, space)
	assert.True(t, FitsAtlas(layout, atlas))
	atlas.Positions[space.Key] = Rect{X: math.MaxInt} // Unused space metadata is ignored.
	missing := layout.Glyphs[0]
	missing.Key.ID = 'B'
	layout.Glyphs = append(layout.Glyphs, missing)
	assert.False(t, FitsAtlas(layout, atlas))
	mesh, err := BuildLayoutMesh(layout, atlas, true)
	require.NoError(t, err)
	assert.Len(t, mesh.Indices, 6) // Low-level partial output; callers gate complete labels.
	assert.False(t, FitsAtlas(nil, atlas))
	assert.False(t, FitsAtlas(layout, nil))
	for _, input := range []struct {
		layout *TextLayout
		atlas  *Atlas
	}{{nil, atlas}, {layout, nil}} {
		mesh, err := BuildLayoutMesh(input.layout, input.atlas, false)
		require.NoError(t, err)
		assert.Equal(t, LayoutMesh{}, mesh)
		require.NoError(t, EmitLayoutQuads(input.layout, input.atlas, nil))
	}
	mesh, err = BuildLayoutMesh(&TextLayout{}, &Atlas{}, false)
	require.NoError(t, err)
	assert.Empty(t, mesh.Expanded)
}

func TestLayoutMeshGlyphLimitAndSignedZeros(t *testing.T) {
	layout, atlas := quadFixture(t)
	first := layout.Glyphs[0]
	layout.Glyphs = make([]PositionedGlyph, MaxTextRunes)
	for i := range layout.Glyphs {
		layout.Glyphs[i] = first
	}
	mesh, err := BuildLayoutMesh(layout, atlas, true)
	require.NoError(t, err)
	assert.Len(t, mesh.Vertices, MaxTextRunes*4)
	assert.Len(t, mesh.Indices, MaxTextRunes*6)
	layout.Glyphs = append(layout.Glyphs, first)
	assert.False(t, FitsAtlas(layout, atlas))
	mesh, err = BuildLayoutMesh(layout, atlas, false)
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	assert.Equal(t, LayoutMesh{}, mesh)
	assert.ErrorIs(t, EmitLayoutQuads(layout, atlas, func([4]geometry.TextVertex) error { t.Fatal("unexpected emission"); return nil }), geometry.ErrGeometryLimit)
	layout.Glyphs = layout.Glyphs[:1]
	layout.Scale = 0
	expanded, err := BuildLayoutMesh(layout, atlas, false)
	require.NoError(t, err)
	indexed, err := BuildLayoutMesh(layout, atlas, true)
	require.NoError(t, err)
	assert.True(t, math.Signbit(float64(expanded.Expanded[0])))
	assertMeshExpansion(t, expanded, indexed)
}

func TestLayoutMeshValidationIsAtomic(t *testing.T) {
	layout, atlas := quadFixture(t)
	for _, scale := range []float64{-1, math.NaN(), math.Inf(1)} {
		bad := *layout
		bad.Scale = scale
		mesh, err := BuildLayoutMesh(&bad, atlas, false)
		assert.ErrorIs(t, err, ErrLayoutGeometry)
		assert.Equal(t, LayoutMesh{}, mesh)
	}
	for _, size := range [][2]int{{0, 256}, {256, 0}, {-1, 256}, {MaxAtlasSize + 1, 256}, {256, MaxAtlasSize + 1}, {math.MaxInt, 256}} {
		bad := *atlas
		bad.Width, bad.Height = size[0], size[1]
		mesh, err := BuildLayoutMesh(layout, &bad, true)
		assert.ErrorIs(t, err, ErrLayoutGeometry)
		assert.Equal(t, LayoutMesh{}, mesh)
	}
	key := layout.Glyphs[0].Key
	for _, rect := range []Rect{
		{X: -1, Width: 10, Height: 10}, {Y: -1, Width: 10, Height: 10}, {X: 257, Width: 10, Height: 10}, {Y: 257, Width: 10, Height: 10},
		{Width: 0, Height: 10}, {Width: 10, Height: 0}, {X: 1, Width: math.MaxInt, Height: 10}, {Y: 1, Width: 10, Height: math.MaxInt},
	} {
		atlas.Positions[key] = rect
		mesh, err := BuildLayoutMesh(layout, atlas, false)
		assert.ErrorIs(t, err, ErrLayoutGeometry)
		assert.Equal(t, LayoutMesh{}, mesh)
	}
	atlas.Positions[key] = Rect{X: 246, Y: 246, Width: 10, Height: 10}
	mesh, err := BuildLayoutMesh(layout, atlas, true)
	require.NoError(t, err)
	assert.Equal(t, float32(1), mesh.Vertices[2].U)
	assert.Equal(t, float32(1), mesh.Vertices[2].V)
	badGlyph := layout.Glyphs[0]
	badGlyph.X = math.MaxFloat64 // Conversion to float32 must not publish infinity.
	layout.Glyphs = append(layout.Glyphs, badGlyph)
	for _, indexed := range []bool{false, true} {
		mesh, err := BuildLayoutMesh(layout, atlas, indexed)
		assert.ErrorIs(t, err, ErrLayoutGeometry)
		assert.Equal(t, LayoutMesh{}, mesh)
	}
	calls := 0
	err = EmitLayoutQuads(layout, atlas, func([4]geometry.TextVertex) error { calls++; return nil })
	assert.ErrorIs(t, err, ErrLayoutGeometry)
	assert.Equal(t, 1, calls) // Streaming callers retain prior successful emissions.
	assert.Equal(t, math.MaxFloat64, layout.Glyphs[1].X)
	assert.ErrorIs(t, EmitLayoutQuads(layout, atlas, nil), ErrLayoutGeometry)
	want := errors.New("sink stopped")
	assert.ErrorIs(t, EmitLayoutQuads(layout, atlas, func([4]geometry.TextVertex) error { return want }), want)
}

func FuzzLayoutMesh(f *testing.F) {
	f.Add(0.0, 0.0, 1.0, 0, 0, 10, 10)
	f.Fuzz(func(t *testing.T, x, y, scale float64, rx, ry, width, height int) {
		key := Key{ID: 65}
		layout := TextLayout{Glyphs: []PositionedGlyph{{Key: key, Glyph: Glyph{Bitmap: []byte{1}}, X: x, Y: y}}, Scale: scale}
		atlas := Atlas{Width: 256, Height: 256, Positions: map[Key]Rect{key: {X: rx, Y: ry, Width: width, Height: height}}}
		expanded, err := BuildLayoutMesh(&layout, &atlas, false)
		if err != nil {
			require.Equal(t, LayoutMesh{}, expanded)
			return
		}
		indexed, err := BuildLayoutMesh(&layout, &atlas, true)
		require.NoError(t, err)
		assertMeshExpansion(t, expanded, indexed)
	})
}
