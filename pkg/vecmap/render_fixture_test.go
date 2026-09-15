package vecmap

import (
	"math"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixtureInputs(t testing.TB) ([]byte, map[string][]byte) {
	t.Helper()
	path, dir := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE"), os.Getenv("WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	if path == "" || dir == "" {
		t.Skip("set WHEREAMI_VECTOR_TILE_FIXTURE and WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR for the pinned full-scene comparison")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	probe, err := CompileRenderFixture(data, nil)
	require.NoError(t, err)
	glyphs := make(map[string][]byte)
	for _, font := range probe.MissingFonts {
		glyphs[font], err = os.ReadFile(filepath.Join(dir, url.PathEscape(font)+".pbf"))
		require.NoError(t, err)
	}
	return data, glyphs
}

func vertexBits(v scene.Vertex) [6]uint32 {
	return [6]uint32{math.Float32bits(v.X), math.Float32bits(v.Y), math.Float32bits(v.OffsetX), math.Float32bits(v.OffsetY), math.Float32bits(v.U), math.Float32bits(v.V)}
}

func requireMeshExpansion(t *testing.T, expanded, indexed scene.Mesh) {
	t.Helper()
	require.NoError(t, indexed.Validate())
	require.Len(t, indexed.Indices, len(expanded.Vertices))
	for i, index := range indexed.Indices {
		want, got := vertexBits(expanded.Vertices[i]), vertexBits(indexed.Vertices[index])
		if want != got {
			require.Equal(t, want, got, "expanded vertex %d, index %d", i, index)
		}
	}
}

func TestDirectRenderFixture(t *testing.T) {
	data, glyphs := fixtureInputs(t)
	expanded, err := CompileRenderFixture(data, glyphs)
	require.NoError(t, err)
	direct, err := CompileRenderFixtureWithOptions(data, glyphs, RenderFixtureOptions{DirectIndexed: true})
	require.NoError(t, err)
	assert.Equal(t, expanded.Camera, direct.Camera)
	assert.Equal(t, expanded.Scene.Draws, direct.Scene.Draws)
	assert.Equal(t, expanded.Scene.Textures, direct.Scene.Textures)
	assert.Equal(t, 62, direct.Labels)
	assert.Equal(t, expanded.Labels, direct.Labels)
	assert.Empty(t, direct.MissingFonts)
	requireMeshExpansion(t, expanded.Scene.Meshes[0], direct.Scene.Meshes[0])
	post, err := scene.IndexMesh(expanded.Scene.Meshes[0])
	require.NoError(t, err)
	requireMeshExpansion(t, expanded.Scene.Meshes[0], post)
	assert.Less(t, direct.Scene.Meshes[0].BufferBytes(), expanded.Scene.Meshes[0].BufferBytes())
	legacy, err := legacyCompileRenderFixtureWithOptions(data, glyphs, RenderFixtureOptions{DirectIndexed: true})
	require.NoError(t, err)
	assert.Equal(t, legacy.Scene, direct.Scene)
	assert.Equal(t, legacy.Frame(legacy.Camera), direct.Frame(direct.Camera))
	for _, layout := range legacy.sdf.layouts {
		assert.Empty(t, layout.Expanded, "direct glyph construction must not expand quads")
		assert.NotEmpty(t, layout.Indices)
	}
	for _, features := range legacy.bucket.sourceLayers {
		for _, feature := range features {
			for _, polygon := range feature.Polygons {
				if len(polygon.Vertices) != 0 {
					assert.NotNil(t, polygon.Indices, "retain Earcut topology in feature storage")
				}
			}
		}
	}
}

func BenchmarkCompileRenderFixture(b *testing.B) {
	data, glyphs := fixtureInputs(b)
	for _, mode := range []string{"expanded", "direct", "post-indexed"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			var mesh scene.Mesh
			for b.Loop() {
				fixture, err := CompileRenderFixtureWithOptions(data, glyphs, RenderFixtureOptions{DirectIndexed: mode == "direct"})
				if err != nil {
					b.Fatal(err)
				}
				mesh = fixture.Scene.Meshes[0]
				if mode == "post-indexed" {
					mesh, err = scene.IndexMesh(mesh)
					if err != nil {
						b.Fatal(err)
					}
				}
			}
			b.ReportMetric(float64(mesh.BufferBytes()), "geometry-B")
		})
	}
}

func TestDirectLineGeometry(t *testing.T) {
	paths := [][]roadPoint{{{X: -10, Y: -5}, {X: 1, Y: 0}, {X: 10, Y: 8}, {X: 10, Y: 8}, {X: 15, Y: 0}}}
	for _, cap := range []string{"butt", "square", "round"} {
		for _, join := range []string{"miter", "round"} {
			for _, dashes := range [][]float64{nil, {2, 1}, {0, 2}, {1, 2, 3}} {
				paint := libertyLinePaint{width: 2, offset: -1, lineCap: cap, lineJoin: join, dashes: dashes}
				expanded, err := tessellateLibertyGeometry(paths, paint, 10000, false)
				require.NoError(t, err)
				direct, err := tessellateLibertyGeometry(paths, paint, 10000, true)
				require.NoError(t, err)
				require.Len(t, direct.Indices, len(expanded.Vertices))
				for i, index := range direct.Indices {
					require.Equal(t, expanded.Vertices[i], direct.Vertices[index])
				}
				_, err = tessellateLibertyGeometry(paths, paint, 1, true)
				if len(expanded.Vertices) > 3 {
					assert.ErrorIs(t, err, errFeatureResourceLimit)
				} else {
					assert.NoError(t, err)
				}
			}
		}
	}
}
