package glyph

import (
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func preparedFixture(t *testing.T) (*PreparedLayout, *Atlas) {
	t.Helper()
	g := bitmapGlyph(2, 3, 90)
	key := Key{FontStack: "font", ID: 'A'}
	atlas, err := BuildAtlas(map[Key]Glyph{key: g})
	require.NoError(t, err)
	return &PreparedLayout{TextLayout: TextLayout{
		Glyphs: []PositionedGlyph{{Key: key, Glyph: g, X: math.Copysign(0, -1), Y: 2}},
		Scale:  0.5, Bounds: TextBounds{Left: -2, Top: -3, Right: 5, Bottom: 6},
	}}, atlas
}

func TestPrepareLayoutsSharesValuesAndSwitchesModes(t *testing.T) {
	layout, atlas := preparedFixture(t)
	type logicalKey struct{ tile, index int }
	key := logicalKey{tile: 12, index: 3}
	input := map[logicalKey]*PreparedLayout{key: layout}
	metrics := layout.TextLayout
	for _, indexed := range []bool{false, true, false} {
		result, err := PrepareLayouts(input, atlas, indexed)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Same(t, layout, result[key])
		assert.Equal(t, metrics, layout.TextLayout)
		assert.Same(t, &metrics.Glyphs[0], &layout.Glyphs[0])
		want, err := BuildLayoutMesh(&metrics, atlas, indexed)
		require.NoError(t, err)
		assert.Equal(t, want, layout.LayoutMesh)
		if indexed {
			assert.Nil(t, layout.Expanded)
		} else {
			assert.Nil(t, layout.Vertices)
			assert.Nil(t, layout.Indices)
		}
		delete(result, key)
		assert.Contains(t, input, key, "result map is owned, values are shared")
	}
}

func TestPrepareLayoutsCoverageAndEmptyOutput(t *testing.T) {
	layout, atlas := preparedFixture(t)
	oldMesh := LayoutMesh{Expanded: []float32{42}}
	missing := &PreparedLayout{TextLayout: TextLayout{Glyphs: []PositionedGlyph{
		layout.Glyphs[0], {Key: Key{FontStack: "missing", ID: 'B'}, Glyph: bitmapGlyph(2, 2, 80)},
	}, Scale: 1}, LayoutMesh: oldMesh}
	blank := &PreparedLayout{TextLayout: TextLayout{Glyphs: []PositionedGlyph{{Glyph: Glyph{Advance: 4}}}, Scale: 1}, LayoutMesh: oldMesh}
	input := map[int]*PreparedLayout{0: layout, 1: missing, 2: blank, 3: nil, 4: {LayoutMesh: oldMesh}}
	result, err := PrepareLayouts(input, atlas, false)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Same(t, layout, result[0])
	assert.Equal(t, oldMesh, missing.LayoutMesh, "missing coverage leaves prior mesh untouched")
	assert.Empty(t, blank.Expanded, "successful empty mesh replaces old geometry")
	assert.Empty(t, input[4].Expanded)
	delete(input, 0)
	result, err = PrepareLayouts(input, atlas, true)
	require.NoError(t, err)
	assert.Nil(t, result)
	result, err = PrepareLayouts(input, nil, false)
	require.NoError(t, err)
	assert.Nil(t, result)
	result, err = PrepareLayouts(map[int]*PreparedLayout(nil), atlas, false)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestPrepareLayoutsLimitsAndErrors(t *testing.T) {
	layout, atlas := preparedFixture(t)
	input := make(map[int]*PreparedLayout)
	for i := range MaxPreparedLayouts {
		input[i] = nil
	}
	input[0] = layout
	result, err := PrepareLayouts(input, atlas, true)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	old := layout.LayoutMesh
	input[MaxPreparedLayouts] = nil
	result, err = PrepareLayouts(input, atlas, false)
	assert.ErrorIs(t, err, geometry.ErrGeometryLimit)
	assert.Nil(t, result)
	assert.Equal(t, old, layout.LayoutMesh, "oversized maps fail before mutation")
	for _, indexed := range []bool{false, true} {
		bad, _ := preparedFixture(t)
		bad.Scale = math.MaxFloat64
		bad.LayoutMesh = LayoutMesh{Expanded: []float32{42}}
		result, err = PrepareLayouts(map[int]*PreparedLayout{0: bad}, atlas, indexed)
		assert.ErrorIs(t, err, ErrLayoutGeometry)
		assert.Nil(t, result)
		assert.Equal(t, []float32{42}, bad.Expanded, "failed mesh is not attached")
	}
}

func TestLayoutGlyphsOwnershipAndDeduplication(t *testing.T) {
	layout, _ := preparedFixture(t)
	key := layout.Glyphs[0].Key
	metrics := layout.Glyphs[0].Glyph
	layout.Glyphs = append(layout.Glyphs, PositionedGlyph{Key: Key{FontStack: "font", ID: ' '}, Glyph: Glyph{Advance: 5}})
	input := map[string]*PreparedLayout{"one": layout, "alias": layout, "nil": nil}
	result := LayoutGlyphs(input)
	require.Len(t, result, 1)
	assert.Equal(t, metrics, result[key])
	assert.Same(t, &metrics.Bitmap[0], &result[key].Bitmap[0])
	delete(result, key)
	assert.Len(t, layout.Glyphs, 2)
	assert.Len(t, input, 3)
	result = LayoutGlyphs(input)
	clear(input)
	assert.Equal(t, metrics, result[key])
	assert.NotNil(t, LayoutGlyphs(map[int]*PreparedLayout(nil)), "empty result is an owned empty map")
}

func FuzzPrepareLayouts(f *testing.F) {
	f.Add(1.0, 0.0, 0.0, false, true)
	f.Add(math.Inf(1), math.MaxFloat64, 0.0, true, false)
	f.Fuzz(func(t *testing.T, scale, x, y float64, indexed, complete bool) {
		layout, atlas := preparedFixture(t)
		layout.Scale, layout.Glyphs[0].X, layout.Glyphs[0].Y = scale, x, y
		if !complete {
			delete(atlas.Positions, layout.Glyphs[0].Key)
		}
		before := LayoutMesh{Expanded: []float32{42}}
		layout.LayoutMesh = before
		result, err := PrepareLayouts(map[int]*PreparedLayout{0: layout}, atlas, indexed)
		if !complete {
			require.NoError(t, err)
			require.Nil(t, result)
			require.Equal(t, before, layout.LayoutMesh)
			return
		}
		want, wantErr := BuildLayoutMesh(&layout.TextLayout, atlas, indexed)
		if wantErr != nil {
			require.ErrorIs(t, err, wantErr)
			require.Nil(t, result)
			require.Equal(t, before, layout.LayoutMesh)
			return
		}
		require.NoError(t, err)
		require.Same(t, layout, result[0])
		require.Equal(t, want, layout.LayoutMesh)
	})
}

func TestUnitLayoutMeshIsIndependentOfScale(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		layout, atlas := preparedFixture(t)
		unit := layout.TextLayout
		unit.Scale = 1
		want, err := BuildLayoutMesh(&unit, atlas, indexed)
		require.NoError(t, err)
		want.Unit = true
		for _, scale := range []float64{0.5, 1, 1.0625, 40} {
			layout.Scale = scale
			mesh, err := BuildUnitLayoutMesh(&layout.TextLayout, atlas, indexed)
			require.NoError(t, err)
			assert.Equal(t, want, mesh, "scale %v", scale)
			assert.Equal(t, scale, layout.Scale, "the layout is not modified")
		}
		// Scales float32 cannot carry as a positive factor keep the baked form.
		for _, scale := range []float64{0, 1e-50} {
			layout.Scale = scale
			baked, err := BuildLayoutMesh(&layout.TextLayout, atlas, indexed)
			require.NoError(t, err)
			mesh, err := BuildUnitLayoutMesh(&layout.TextLayout, atlas, indexed)
			require.NoError(t, err)
			assert.False(t, mesh.Unit)
			assert.Equal(t, baked, mesh)
		}
		for _, scale := range []float64{-1, math.MaxFloat64, math.NaN(), math.Inf(1)} {
			layout.Scale = scale
			mesh, err := BuildUnitLayoutMesh(&layout.TextLayout, atlas, indexed)
			assert.ErrorIs(t, err, ErrLayoutGeometry, "scale %v", scale)
			assert.Equal(t, LayoutMesh{}, mesh)
		}
		layout.Scale = 2
		layout.Glyphs[0].X = math.MaxFloat64
		mesh, err := BuildUnitLayoutMesh(&layout.TextLayout, atlas, indexed)
		assert.ErrorIs(t, err, ErrLayoutGeometry)
		assert.Equal(t, LayoutMesh{}, mesh)
		mesh, err = BuildUnitLayoutMesh(nil, atlas, indexed)
		require.NoError(t, err)
		assert.Equal(t, LayoutMesh{}, mesh)
		mesh, err = BuildUnitLayoutMesh(&layout.TextLayout, nil, indexed)
		require.NoError(t, err)
		assert.Equal(t, LayoutMesh{}, mesh)
	}
}

func TestPrepareUnitLayouts(t *testing.T) {
	layout, atlas := preparedFixture(t)
	metrics := layout.TextLayout
	result, err := PrepareUnitLayouts(map[int]*PreparedLayout{7: layout}, atlas, true)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Same(t, layout, result[7])
	assert.Equal(t, metrics, layout.TextLayout, "bounds and scale stay those of the evaluated size")
	assert.True(t, layout.Unit)
	want, err := BuildUnitLayoutMesh(&metrics, atlas, true)
	require.NoError(t, err)
	assert.Equal(t, want, layout.LayoutMesh)
	baked, err := BuildLayoutMesh(&metrics, atlas, true)
	require.NoError(t, err)
	for i, vertex := range layout.Vertices {
		assert.InDelta(t, baked.Vertices[i].X, float64(vertex.X)*metrics.Scale, 1e-6)
		assert.InDelta(t, baked.Vertices[i].Y, float64(vertex.Y)*metrics.Scale, 1e-6)
		assert.Equal(t, baked.Vertices[i].U, vertex.U)
		assert.Equal(t, baked.Vertices[i].V, vertex.V)
	}
	// Preparing in the baked form again replaces the whole mesh.
	_, err = PrepareLayouts(map[int]*PreparedLayout{7: layout}, atlas, true)
	require.NoError(t, err)
	assert.False(t, layout.Unit)
	assert.Equal(t, baked, layout.LayoutMesh)
}
