package tiles

import (
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func residentPBF() []byte {
	land := layerPBF("land", featurePBF(3, []uint32{9, 0, 0, 18, 512, 0, 511, 512, 15}))
	roads := layerPBF("roads", featurePBF(2, []uint32{9, 20, 20, 18, 200, 0, 0, 200}))
	labels := layerPBF("labels", featurePBF(1, []uint32{9, 256, 256}))
	return append(append(land, roads...), labels...)
}

func residentStyle(t *testing.T) []style.CompiledLayer {
	t.Helper()
	layers, err := style.Parse([]byte(`{"version":8,"layers":[
		{"id":"background","type":"background","paint":{"background-color":"white"}},
		{"id":"land","type":"fill","source-layer":"land","paint":{"fill-color":["interpolate",["linear"],["zoom"],2,"#203040",6,"#405060"],"fill-outline-color":"#000000"}},
		{"id":"casing","type":"line","source-layer":"roads","layout":{"line-cap":"round","line-join":"round"},
		 "paint":{"line-color":"#888888","line-width":["interpolate",["linear"],["zoom"],2,2,6,10]}},
		{"id":"rail","type":"line","source-layer":"roads","paint":{"line-color":"#222222","line-width":["interpolate",["linear"],["zoom"],2,1,6,3],"line-dasharray":[2,2]}},
		{"id":"labels","type":"symbol","source-layer":"labels",
		 "layout":{"text-field":"A","text-font":["Test"],"text-size":["interpolate",["linear"],["zoom"],2,10,6,18]},
		 "paint":{"text-color":"black"}}
	]}`))
	require.NoError(t, err)
	return layers
}

func residentFragment(t *testing.T, zoom float64, resident bool) *Fragment {
	t.Helper()
	p, err := Prepare(residentPBF(), residentStyle(t), PrepareOptions{Tile: testTile, Zoom: zoom, Indexed: true, ResidentGeometry: resident})
	require.NoError(t, err)
	result, err := p.BuildOwned(prepareAssets(), 1<<20)
	require.NoError(t, err)
	require.Empty(t, result.MissingFonts)
	require.NoError(t, result.Fragment.Scene.Validate())
	return result.Fragment
}

func fragmentMesh(f *Fragment, id uint64) *scene.Mesh {
	for i := range f.Scene.Meshes {
		if f.Scene.Meshes[i].ID == id {
			return &f.Scene.Meshes[i]
		}
	}
	return nil
}

func TestResidentGeometryAcrossStyleZooms(t *testing.T) {
	legacy, legacyNext := residentFragment(t, 3, false), residentFragment(t, 3.0625, false)
	require.Len(t, legacy.Scene.Meshes, 1)
	assert.NotEqual(t, legacy.Scene.Meshes[0].Vertices, legacyNext.Scene.Meshes[0].Vertices, "baked widths change the single mesh")

	first, next := residentFragment(t, 3, true), residentFragment(t, 3.0625, true)
	for _, pair := range [][2]*Fragment{{legacy, first}, {legacyNext, next}} {
		single, split := pair[0], pair[1]
		assert.Equal(t, single.Draws, split.Draws, "provenance and order are unchanged")
		assert.Equal(t, single.Symbols, split.Symbols)
		require.Len(t, split.Scene.Draws, len(single.Scene.Draws))
		var bytes uint64
		for _, mesh := range split.Scene.Meshes {
			bytes += mesh.BufferBytes()
		}
		assert.Equal(t, single.Scene.Meshes[0].BufferBytes(), bytes)
		for i, draw := range split.Scene.Draws {
			assert.Equal(t, single.Scene.Draws[i].Count, draw.Count)
			assert.Equal(t, single.Scene.Draws[i].Material.Color, draw.Material.Color)
			wantDynamic := split.Draws[i].Part != compiler.BaseDraw || single.Scene.Draws[i].Material.Color == [4]float32{34.0 / 255, 34.0 / 255, 34.0 / 255, 1}
			if wantDynamic {
				assert.Equal(t, uint64(compiler.DynamicMesh), draw.Mesh, "draw %d", i)
			} else {
				assert.Equal(t, uint64(compiler.StableMesh), draw.Mesh, "draw %d", i)
			}
		}
	}
	stable, dynamic := fragmentMesh(first, compiler.StableMesh), fragmentMesh(first, compiler.DynamicMesh)
	require.NotNil(t, stable)
	require.NotNil(t, dynamic)
	assert.Equal(t, stable, fragmentMesh(next, compiler.StableMesh), "fills, outlines and plain lines stay byte-identical")
	assert.NotEqual(t, dynamic.Vertices, fragmentMesh(next, compiler.DynamicMesh).Vertices, "dashes and labels follow the zoom")
	var widths []float32
	for _, f := range []*Fragment{first, next} {
		for _, draw := range f.Scene.Draws {
			if draw.Material.OffsetScale > 1 {
				assert.True(t, draw.Material.MapAligned)
				widths = append(widths, draw.Material.OffsetScale)
			}
		}
	}
	assert.Equal(t, []float32{2, 2.0625}, widths, "half of the casing width at each style zoom")

	// The retained store keeps the stable mesh's revision, so a style-zoom change
	// uploads only the dynamic mesh.
	set := newSet(t, retained.Limits{})
	camera := testCamera(testTile)
	require.NoError(t, set.Apply([]Change{{testTile, first}}))
	before := selectTiles(t, set, []view.TileID{testTile}, nil, camera)
	planner, err := retained.NewPlanner(retained.ResidencyLimits{})
	require.NoError(t, err)
	budget := retained.Budget{Bytes: 1 << 20, Resources: 8}
	settle := func() (uploads []retained.Version) {
		for {
			batch, err := planner.Next(budget)
			require.NoError(t, err)
			if batch == nil {
				return uploads
			}
			for _, resource := range batch.Uploads {
				uploads = append(uploads, resource.Version)
			}
			require.NoError(t, planner.Acknowledge(batch.Ticket, true))
		}
	}
	require.NoError(t, planner.SetTarget(before.Scene))
	initial := settle()
	require.Len(t, before.Scene.Meshes, 2)
	assert.Len(t, initial, len(before.Scene.Meshes)+len(before.Scene.Textures))
	reused := set.ReusedVersions()
	require.NoError(t, set.Apply([]Change{{testTile, next}}))
	after := selectTiles(t, set, []view.TileID{testTile}, before.Cover, camera)
	require.Len(t, after.Scene.Meshes, 2)
	assert.Equal(t, before.Scene.Meshes[0].ID, after.Scene.Meshes[0].ID)
	assert.Equal(t, before.Scene.Meshes[0].Revision, after.Scene.Meshes[0].Revision, "the stable mesh keeps its resident revision")
	assert.Equal(t, before.Scene.Meshes[1].ID, after.Scene.Meshes[1].ID)
	assert.NotEqual(t, before.Scene.Meshes[1].Revision, after.Scene.Meshes[1].Revision)
	assert.GreaterOrEqual(t, set.ReusedVersions()-reused, uint64(1))
	require.NoError(t, planner.SetTarget(after.Scene))
	assert.Same(t, before.Scene, planner.Current())
	uploads := settle()
	assert.Equal(t, []retained.Version{{Kind: retained.MeshResource, ID: after.Scene.Meshes[1].ID, Revision: after.Scene.Meshes[1].Revision}}, uploads)
	assert.Same(t, after.Scene, planner.Current())
	assert.NotEqual(t, before.Scene.Draws, after.Scene.Draws, "paint changes arrive as draws")
}

func TestResidentGeometryOptionDefaultsOff(t *testing.T) {
	plain := residentFragment(t, 3, false)
	for _, draw := range plain.Scene.Draws {
		assert.Equal(t, uint64(compiler.StableMesh), draw.Mesh)
		assert.Zero(t, draw.Material.OffsetScale)
	}
	// An extruded-only tile has no dynamic mesh at all.
	layers := residentStyle(t)[:3]
	p, err := Prepare(residentPBF(), layers, PrepareOptions{Tile: testTile, Zoom: 3, Indexed: true, ResidentGeometry: true})
	require.NoError(t, err)
	result, err := p.Build(prepareAssets())
	require.NoError(t, err)
	require.Len(t, result.Fragment.Scene.Meshes, 1)
	assert.Equal(t, uint64(compiler.StableMesh), result.Fragment.Scene.Meshes[0].ID)
}

func FuzzResidentTile(f *testing.F) {
	f.Add(residentPBF(), true, uint8(1))
	f.Add(preparePBF(), false, uint8(16))
	f.Add([]byte{0}, false, uint8(0))
	f.Fuzz(func(t *testing.T, data []byte, indexed bool, step uint8) {
		if len(data) > 32<<10 {
			return
		}
		layers := residentStyle(t)
		build := func(zoom float64, resident bool) *Fragment {
			p, err := Prepare(data, layers, PrepareOptions{Tile: testTile, Zoom: zoom, Indexed: indexed, ResidentGeometry: resident,
				TriangleLimit: 1024, CandidateLimit: 16, ElementLimit: 32768, DrawLimit: 256})
			if err != nil {
				require.Nil(t, p)
				return nil
			}
			built, err := p.Build(prepareAssets())
			if err != nil {
				require.Nil(t, built)
				return nil
			}
			require.NoError(t, built.Fragment.Scene.Validate())
			return built.Fragment
		}
		zoom := 3 + float64(step%64)/16
		legacy, split := build(zoom, false), build(zoom, true)
		if legacy == nil || split == nil {
			// Both forms share topology and limits, so they fail together.
			require.Nil(t, legacy)
			require.Nil(t, split)
			return
		}
		require.Equal(t, legacy.Draws, split.Draws)
		require.Len(t, split.Scene.Draws, len(legacy.Scene.Draws))
		var legacyBytes, splitBytes uint64
		for _, mesh := range legacy.Scene.Meshes {
			legacyBytes += mesh.BufferBytes()
		}
		for _, mesh := range split.Scene.Meshes {
			splitBytes += mesh.BufferBytes()
			require.Contains(t, []uint64{compiler.StableMesh, compiler.DynamicMesh}, mesh.ID)
		}
		require.Equal(t, legacyBytes, splitBytes)
		for i, draw := range split.Scene.Draws {
			require.Equal(t, legacy.Scene.Draws[i].Count, draw.Count)
			require.Equal(t, legacy.Scene.Draws[i].Material.Color, draw.Material.Color)
			if split.Draws[i].Part != compiler.BaseDraw {
				require.Equal(t, uint64(compiler.DynamicMesh), draw.Mesh)
			}
		}
		set := newSet(t, retained.Limits{})
		require.NoError(t, set.Apply([]Change{{testTile, split}}))
		selected := selectTiles(t, set, []view.TileID{testTile}, nil, testCamera(testTile))
		assert.LessOrEqual(t, len(selected.Scene.Draws), len(split.Draws))
	})
}
