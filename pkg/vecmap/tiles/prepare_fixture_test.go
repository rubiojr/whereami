package tiles

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/fixture"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type renderKey struct {
	material      scene.Material
	clip          [4]float32
	transform     int
	texture       [32]byte
	width, height int
}

type renderRun struct {
	key      renderKey
	vertices uint64
	digest   [32]byte
}

// Compare ordered rendering rather than unused packed vertices, local resource
// IDs or source-boundary draw splits. Vertex hashing retains exact float32 bits.
func renderRuns(s *scene.Scene) []renderRun {
	meshes := make(map[uint64]scene.Mesh)
	textures := make(map[uint64]renderKey)
	for _, mesh := range s.Meshes {
		meshes[mesh.ID] = mesh
	}
	for _, texture := range s.Textures {
		textures[texture.ID] = renderKey{texture: sha256.Sum256(texture.RGBA), width: texture.Width, height: texture.Height}
	}
	var result []renderRun
	hash := sha256.New()
	var current renderRun
	flush := func() {
		if current.vertices != 0 {
			copy(current.digest[:], hash.Sum(nil))
			result = append(result, current)
		}
		hash.Reset()
	}
	var encoded [24]byte
	for _, draw := range s.Draws {
		key := textures[draw.Material.Texture]
		key.material, key.clip, key.transform = draw.Material, draw.Clip, draw.Transform
		key.material.Texture = 0
		if current.key != key {
			flush()
			current = renderRun{key: key}
		}
		mesh := meshes[draw.Mesh]
		for i := draw.First; i < draw.First+draw.Count; i++ {
			index := i
			if len(mesh.Indices) > 0 {
				index = mesh.Indices[i]
			}
			vertex := mesh.Vertices[index]
			for j, value := range [...]float32{vertex.X, vertex.Y, vertex.OffsetX, vertex.OffsetY, vertex.U, vertex.V} {
				binary.LittleEndian.PutUint32(encoded[j*4:], math.Float32bits(value))
			}
			_, _ = hash.Write(encoded[:])
			current.vertices++
		}
	}
	flush()
	return result
}

func TestPreparedPinnedRenderingParity(t *testing.T) {
	path, dir := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE"), os.Getenv("WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	if path == "" || dir == "" {
		t.Skip("set pinned tile and glyph fixture environment variables")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	layers, err := liberty.Layers()
	require.NoError(t, err)
	for _, indexed := range []bool{false, true} {
		prepared, err := Prepare(data, layers, PrepareOptions{Tile: fixture.Tile(), Zoom: 10, Indexed: indexed})
		require.NoError(t, err)
		ranges := make(map[string][]byte)
		for _, font := range compiler.FontStacks(prepared.TextRequests()) {
			ranges[font], err = os.ReadFile(filepath.Join(dir, url.PathEscape(font)+".pbf"))
			require.NoError(t, err)
		}
		for _, fonts := range []map[string][]byte{ranges, {"Noto Sans Regular": ranges["Noto Sans Regular"]}, nil} {
			decoded, err := compiler.DecodeFontRanges(fonts, 0)
			require.NoError(t, err)
			built, err := prepared.Build(Assets{Fonts: decoded, Sprite: liberty.Sprite, SpriteEntry: liberty.SpriteEntry, FallbackEligible: glyph.LegacyFallbackEligible})
			require.NoError(t, err)
			owned, err := prepared.BuildOwned(Assets{Fonts: decoded, Sprite: liberty.Sprite, SpriteEntry: liberty.SpriteEntry, FallbackEligible: glyph.LegacyFallbackEligible}, 512<<20)
			require.NoError(t, err)
			assert.Equal(t, renderRuns(built.Fragment.Scene), renderRuns(owned.Fragment.Scene), "owning pixels must preserve ordered rendering exactly")
			assert.Equal(t, built.Fragment.Draws, owned.Fragment.Draws)
			assert.Equal(t, built.Fragment.Symbols, owned.Fragment.Symbols)
			reference, err := fixture.Compile(data, fonts, fixture.Options{DirectIndexed: indexed})
			require.NoError(t, err)
			assert.Equal(t, reference.MissingFonts, built.MissingFonts)
			assert.Equal(t, reference.Limits, built.Limits)
			set := newSet(t, retained.Limits{})
			require.NoError(t, set.Apply([]Change{{fixture.Tile(), built.Fragment}}))
			selected := selectTiles(t, set, []view.TileID{fixture.Tile()}, nil, reference.Camera)
			labels := 0
			for _, draw := range selected.Scene.Draws {
				if draw.Material.Kind == scene.SDFFill {
					labels++
				}
			}
			assert.Equal(t, reference.Labels, labels)
			assert.Equal(t, renderRuns(reference.Scene), renderRuns(selected.Scene), "indexed=%t font stacks=%d", indexed, len(fonts))
			assert.Same(t, &built.Fragment.Scene.Meshes[0].Vertices[0], &selected.Scene.Meshes[0].Vertices[0])
			t.Logf("indexed=%t fonts=%d candidates=%d packed_draws=%d selected_draws=%d labels=%d", indexed, len(fonts), len(built.Fragment.Symbols), len(built.Fragment.Draws), len(selected.Scene.Draws), labels)
		}
	}
}
