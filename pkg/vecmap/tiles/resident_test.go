package tiles

import (
	"math"
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

func residentSymbolFragment(t *testing.T, layers []style.CompiledLayer, zoom float64, resident bool) *Fragment {
	t.Helper()
	p, err := Prepare(residentPBF(), layers, PrepareOptions{Tile: testTile, Zoom: zoom, Indexed: true, ResidentGeometry: resident, ResidentSymbols: resident})
	require.NoError(t, err)
	result, err := p.BuildOwned(prepareAssets(), 1<<20)
	require.NoError(t, err)
	require.Empty(t, result.MissingFonts)
	require.NoError(t, result.Fragment.Scene.Validate())
	return result.Fragment
}

func TestResidentSymbolsAcrossStyleZooms(t *testing.T) {
	layers := residentStyle(t)
	plain := append(append([]style.CompiledLayer{}, layers[:3]...), layers[4]) // no dashed line
	for _, test := range []struct {
		name    string
		layers  []style.CompiledLayer
		meshes  []uint64
		uploads []uint64
	}{
		{"plain lines", plain, []uint64{compiler.StableMesh, compiler.SymbolMesh}, nil},
		{"dashed lines", layers, []uint64{compiler.StableMesh, compiler.DynamicMesh, compiler.SymbolMesh}, []uint64{compiler.DynamicMesh}},
	} {
		t.Run(test.name, func(t *testing.T) {
			legacy, legacyNext := residentSymbolFragment(t, test.layers, 3, false), residentSymbolFragment(t, test.layers, 3.0625, false)
			first, next := residentSymbolFragment(t, test.layers, 3, true), residentSymbolFragment(t, test.layers, 3.0625, true)
			sizes := []float32{12.0 / 24, 12.125 / 24}
			for n, pair := range [][2]*Fragment{{legacy, first}, {legacyNext, next}} {
				single, split := pair[0], pair[1]
				assert.Equal(t, single.Draws, split.Draws, "provenance and order are unchanged")
				assert.Equal(t, single.Symbols, split.Symbols, "collision input is unchanged")
				require.Len(t, split.Scene.Draws, len(single.Scene.Draws))
				var ids []uint64
				var bytes uint64
				for _, mesh := range split.Scene.Meshes {
					ids = append(ids, mesh.ID)
					bytes += mesh.BufferBytes()
				}
				assert.Equal(t, test.meshes, ids)
				assert.Equal(t, single.Scene.Meshes[0].BufferBytes(), bytes)
				baked := single.Scene.Meshes[0]
				symbols := fragmentMesh(split, compiler.SymbolMesh)
				labels := 0
				for i, draw := range split.Scene.Draws {
					if split.Draws[i].Part == compiler.BaseDraw {
						continue
					}
					labels++
					assert.Equal(t, uint64(compiler.SymbolMesh), draw.Mesh)
					assert.Equal(t, sizes[n], draw.Material.OffsetScale)
					assert.Equal(t, sizes[n], draw.Material.FontScale)
					for k := range draw.Count {
						want := baked.Vertices[baked.Indices[single.Scene.Draws[i].First+k]]
						got := symbols.Vertices[symbols.Indices[draw.First+k]]
						assert.Equal(t, want.X, got.X)
						assert.Equal(t, want.Y, got.Y)
						assert.Equal(t, want.U, got.U)
						assert.Equal(t, want.V, got.V)
						assert.InDelta(t, want.OffsetX, got.OffsetX*sizes[n], 1e-5)
						assert.InDelta(t, want.OffsetY, got.OffsetY*sizes[n], 1e-5)
					}
				}
				assert.Positive(t, labels)
			}
			for _, id := range []uint64{compiler.StableMesh, compiler.SymbolMesh} {
				require.NotNil(t, fragmentMesh(first, id))
				assert.Equal(t, fragmentMesh(first, id), fragmentMesh(next, id), "mesh %d stays byte-identical", id)
			}
			assert.Equal(t, first.Scene.Textures, next.Scene.Textures)

			set := newSet(t, retained.Limits{})
			camera := testCamera(testTile)
			require.NoError(t, set.Apply([]Change{{testTile, first}}))
			before := selectTiles(t, set, []view.TileID{testTile}, nil, camera)
			planner, err := retained.NewPlanner(retained.ResidencyLimits{})
			require.NoError(t, err)
			settle := func() (uploads []uint64) {
				for {
					batch, err := planner.Next(retained.Budget{Bytes: 1 << 20, Resources: 8})
					require.NoError(t, err)
					if batch == nil {
						return uploads
					}
					for _, resource := range batch.Uploads {
						require.Equal(t, retained.MeshResource, resource.Version.Kind)
						uploads = append(uploads, resource.Version.ID)
					}
					require.NoError(t, planner.Acknowledge(batch.Ticket, true))
				}
			}
			require.NoError(t, planner.SetTarget(before.Scene))
			for {
				batch, err := planner.Next(retained.Budget{Bytes: 1 << 20, Resources: 8})
				require.NoError(t, err)
				if batch == nil {
					break
				}
				require.NoError(t, planner.Acknowledge(batch.Ticket, true))
			}
			require.Same(t, before.Scene, planner.Current())
			reused := set.ReusedVersions()
			require.NoError(t, set.Apply([]Change{{testTile, next}}))
			after := selectTiles(t, set, []view.TileID{testTile}, before.Cover, camera)
			require.NotSame(t, before, after)
			assert.Equal(t, uint64(len(test.meshes)-len(test.uploads)+len(next.Scene.Textures)), set.ReusedVersions()-reused)
			require.NoError(t, planner.SetTarget(after.Scene))
			var want []uint64
			for i, mesh := range after.Scene.Meshes {
				if before.Scene.Meshes[i].Revision != mesh.Revision {
					want = append(want, mesh.ID)
				}
			}
			assert.Len(t, want, len(test.uploads))
			assert.Equal(t, want, settle(), "only zoom-baked line geometry is uploaded")
			assert.Same(t, after.Scene, planner.Current())
			assert.NotEqual(t, before.Scene.Draws, after.Scene.Draws, "sizes and widths arrive as draws")
		})
	}
}

func TestResidentSymbolsFollowLayoutChanges(t *testing.T) {
	layers, err := style.Parse([]byte(`{"version":8,"layers":[
		{"id":"labels","type":"symbol","source-layer":"labels",
		 "layout":{"text-field":"AA","text-font":["Test"],"text-size":["interpolate",["linear"],["zoom"],2,10,6,18],
		  "text-letter-spacing":["interpolate",["linear"],["zoom"],4,0,6,1]},
		 "paint":{"text-color":"black"}}
	]}`))
	require.NoError(t, err)
	build := func(zoom float64) *Fragment { return residentSymbolFragment(t, layers, zoom, true) }
	a, b, c := build(3.875), build(3.9375), build(4.0625)
	require.Len(t, a.Scene.Meshes, 1)
	assert.Equal(t, uint64(compiler.SymbolMesh), a.Scene.Meshes[0].ID)
	assert.Equal(t, a.Scene.Meshes, b.Scene.Meshes)
	assert.NotEqual(t, a.Scene.Draws, b.Scene.Draws)
	assert.NotEqual(t, b.Scene.Meshes[0].Vertices, c.Scene.Meshes[0].Vertices, "letter spacing in ems is a layout change")
	// Symbols alone are resident without split base geometry.
	p, err := Prepare(residentPBF(), residentStyle(t), PrepareOptions{Tile: testTile, Zoom: 3, Indexed: false, ResidentSymbols: true})
	require.NoError(t, err)
	result, err := p.Build(prepareAssets())
	require.NoError(t, err)
	require.Len(t, result.Fragment.Scene.Meshes, 2)
	assert.Equal(t, uint64(compiler.StableMesh), result.Fragment.Scene.Meshes[0].ID)
	assert.Equal(t, uint64(compiler.SymbolMesh), result.Fragment.Scene.Meshes[1].ID)
}

func FuzzResidentSymbolTile(f *testing.F) {
	f.Add(residentPBF(), true, uint8(1))
	f.Add(preparePBF(), false, uint8(16))
	f.Add([]byte{0}, false, uint8(0))
	f.Fuzz(func(t *testing.T, data []byte, indexed bool, step uint8) {
		if len(data) > 32<<10 {
			return
		}
		layers := residentStyle(t)
		build := func(zoom float64, resident bool) *Fragment {
			p, err := Prepare(data, layers, PrepareOptions{Tile: testTile, Zoom: zoom, Indexed: indexed, ResidentGeometry: resident, ResidentSymbols: resident,
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
		legacy, resident, next := build(zoom, false), build(zoom, true), build(zoom+1.0/16, true)
		if legacy == nil || resident == nil {
			require.Nil(t, legacy)
			require.Nil(t, resident)
			return
		}
		require.Equal(t, legacy.Draws, resident.Draws)
		require.Equal(t, legacy.Symbols, resident.Symbols)
		require.Len(t, resident.Scene.Draws, len(legacy.Scene.Draws))
		for i, draw := range resident.Scene.Draws {
			require.Equal(t, legacy.Scene.Draws[i].Count, draw.Count)
			if resident.Draws[i].Part != compiler.BaseDraw {
				require.Equal(t, uint64(compiler.SymbolMesh), draw.Mesh)
				require.Positive(t, draw.Material.OffsetScale)
			}
		}
		// The test style changes sizes only, so symbols outlive the zoom step.
		if next != nil && len(next.Symbols) == len(resident.Symbols) {
			require.Equal(t, fragmentMesh(resident, compiler.SymbolMesh), fragmentMesh(next, compiler.SymbolMesh))
		}
	})
}

func TestResidentDashesAcrossStyleZooms(t *testing.T) {
	layers := residentStyle(t)
	build := func(zoom float64, geometry, symbols, dashes bool) *Fragment {
		p, err := Prepare(residentPBF(), layers, PrepareOptions{Tile: testTile, Zoom: zoom, Indexed: true, ResidentGeometry: geometry, ResidentSymbols: symbols, ResidentDashes: dashes})
		require.NoError(t, err)
		result, err := p.BuildOwned(prepareAssets(), 1<<20)
		require.NoError(t, err)
		require.NoError(t, result.Fragment.Scene.Validate())
		return result.Fragment
	}
	legacy := build(3, false, false, false)
	first, next := build(3, true, true, true), build(3.0625, true, true, true)
	assert.Equal(t, legacy.Draws, first.Draws, "provenance and order are unchanged")
	assert.Equal(t, legacy.Symbols, first.Symbols)
	require.Len(t, first.Scene.Draws, len(legacy.Scene.Draws))
	dashes := 0
	for i, draw := range first.Scene.Draws {
		assert.Equal(t, legacy.Scene.Draws[i].Material.Color, draw.Material.Color)
		assert.Equal(t, legacy.Scene.Draws[i].Clip, draw.Clip)
		if draw.Material.Kind != scene.Dashed {
			assert.Equal(t, legacy.Scene.Draws[i].Count, draw.Count)
			continue
		}
		dashes++
		// One quad per segment replaces the baked dashes of the rail.
		assert.Equal(t, uint32(12), draw.Count)
		assert.Equal(t, uint64(compiler.StableMesh), draw.Mesh)
		assert.Equal(t, [4]float32{2, 2}, draw.Material.Dashes)
		assert.Equal(t, float32(0.75), draw.Material.OffsetScale, "half of the rail width at zoom 3")
		assert.Equal(t, float32(1.5), draw.Material.DashUnit, "the rail width in tile units at the source zoom")
		assert.Equal(t, scene.Material{Kind: scene.Dashed, Color: draw.Material.Color, MapAligned: true, OffsetScale: 1.53125 / 2, DashUnit: float32(1.53125 / math.Exp2(0.0625)), Dashes: [4]float32{2, 2}}, next.Scene.Draws[i].Material)
	}
	assert.Equal(t, 1, dashes)
	var ids []uint64
	for _, mesh := range first.Scene.Meshes {
		ids = append(ids, mesh.ID)
	}
	assert.Equal(t, []uint64{compiler.StableMesh, compiler.SymbolMesh}, ids, "no zoom-baked geometry remains")
	assert.Equal(t, first.Scene.Meshes, next.Scene.Meshes)
	assert.Equal(t, first.Scene.Textures, next.Scene.Textures)

	// A style-zoom change is published as draws, without any upload.
	set := newSet(t, retained.Limits{})
	camera := testCamera(testTile)
	require.NoError(t, set.Apply([]Change{{testTile, first}}))
	before := selectTiles(t, set, []view.TileID{testTile}, nil, camera)
	planner, err := retained.NewPlanner(retained.ResidencyLimits{})
	require.NoError(t, err)
	settle := func() (uploads int) {
		for {
			batch, err := planner.Next(retained.Budget{Bytes: 1 << 20, Resources: 8})
			require.NoError(t, err)
			if batch == nil {
				return uploads
			}
			uploads += len(batch.Uploads)
			require.NoError(t, planner.Acknowledge(batch.Ticket, true))
		}
	}
	require.NoError(t, planner.SetTarget(before.Scene))
	assert.Equal(t, len(before.Scene.Meshes)+len(before.Scene.Textures), settle())
	require.NoError(t, set.Apply([]Change{{testTile, next}}))
	after := selectTiles(t, set, []view.TileID{testTile}, before.Cover, camera)
	require.NoError(t, planner.SetTarget(after.Scene))
	assert.Zero(t, settle())
	assert.Same(t, after.Scene, planner.Current())
	assert.NotEqual(t, before.Scene.Draws, after.Scene.Draws)

	// Dashes alone are resident in the single mesh of the default layout, and
	// without the option nothing is dashed by the consumer.
	alone := build(3, false, false, true)
	require.Len(t, alone.Scene.Meshes, 1)
	assert.Equal(t, legacy.Draws, alone.Draws)
	for _, draw := range build(3, true, true, false).Scene.Draws {
		assert.NotEqual(t, scene.Dashed, draw.Material.Kind)
	}
}

func FuzzResidentDashTile(f *testing.F) {
	f.Add(residentPBF(), true, uint8(1))
	f.Add(preparePBF(), false, uint8(16))
	f.Add([]byte{0}, false, uint8(0))
	f.Fuzz(func(t *testing.T, data []byte, indexed bool, step uint8) {
		if len(data) > 32<<10 {
			return
		}
		layers := residentStyle(t)
		build := func(zoom float64, resident bool) *Fragment {
			p, err := Prepare(data, layers, PrepareOptions{Tile: testTile, Zoom: zoom, Indexed: indexed, ResidentGeometry: resident, ResidentSymbols: resident, ResidentDashes: resident,
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
		legacy, resident, next := build(zoom, false), build(zoom, true), build(zoom+1.0/16, true)
		if resident == nil {
			// One quad per segment never needs more than the baked dashes.
			require.Nil(t, legacy)
			return
		}
		if legacy != nil {
			require.Equal(t, legacy.Draws, resident.Draws)
			require.Equal(t, legacy.Symbols, resident.Symbols)
		}
		for _, draw := range resident.Scene.Draws {
			require.NotEqual(t, uint64(compiler.DynamicMesh), draw.Mesh, "the test style has no baked lines left")
		}
		if next != nil && len(next.Symbols) == len(resident.Symbols) {
			require.Equal(t, resident.Scene.Meshes, next.Scene.Meshes)
		}
	})
}

// drawnVertices lists what a fragment draws: for every draw its vertices in
// draw order with all attributes, whatever section stores them.
func drawnVertices(t *testing.T, f *Fragment) [][]scene.Vertex {
	t.Helper()
	require.NoError(t, f.Scene.Validate())
	result := make([][]scene.Vertex, 0, len(f.Scene.Draws))
	for _, draw := range f.Scene.Draws {
		mesh := fragmentMesh(f, draw.Mesh)
		require.NotNil(t, mesh)
		vertices := make([]scene.Vertex, 0, draw.Count)
		for i := draw.First; i < draw.First+draw.Count; i++ {
			index := int(i)
			if len(mesh.Indices) > 0 {
				index = int(mesh.Indices[i])
			}
			vertices = append(vertices, mesh.At(draw.Layout, index))
		}
		result = append(result, vertices)
	}
	return result
}

// Packed vertices draw the tile within half a packed step, in fewer retained
// bytes: positions on the MVT grid exactly, baked line outlines and line
// directions rounded.
func TestPackedVerticesDrawTheSameTile(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		for _, resident := range []bool{false, true} {
			build := func(zoom float64, packed bool) *Fragment {
				p, err := Prepare(residentPBF(), residentStyle(t), PrepareOptions{Tile: testTile, Zoom: zoom, Indexed: indexed,
					ResidentGeometry: resident, ResidentSymbols: resident, ResidentDashes: resident, CompactVertices: true, PackedVertices: packed})
				require.NoError(t, err)
				result, err := p.BuildOwned(prepareAssets(), 1<<20)
				require.NoError(t, err)
				return result.Fragment
			}
			compact, packed := build(3, false), build(3, true)
			want, got := drawnVertices(t, compact), drawnVertices(t, packed)
			require.Len(t, got, len(want))
			for i := range want {
				require.Len(t, got[i], len(want[i]))
				for k := range want[i] {
					w, g := want[i][k], got[i][k]
					assert.Equal(t, [2]float32{w.U, w.V}, [2]float32{g.U, g.V}, "draw %d vertex %d", i, k)
					assert.InDelta(t, w.X, g.X, 1.0/(2*scene.PositionUnits))
					assert.InDelta(t, w.Y, g.Y, 1.0/(2*scene.PositionUnits))
					assert.InDelta(t, w.OffsetX, g.OffsetX, 1.0/(2*scene.OffsetUnits))
					assert.InDelta(t, w.OffsetY, g.OffsetY, 1.0/(2*scene.OffsetUnits))
				}
			}
			assert.Equal(t, compact.Draws, packed.Draws)
			assert.Equal(t, compact.Symbols, packed.Symbols)
			assert.Equal(t, compact.Scene.Textures, packed.Scene.Textures)
			assert.Less(t, packed.RetainedBytes(), compact.RetainedBytes())
			var positions int
			for _, mesh := range packed.Scene.Meshes {
				positions += len(mesh.PackedPositions)
				assert.Empty(t, mesh.Positions, "every fill packs")
				assert.Empty(t, mesh.Offsets)
			}
			assert.NotZero(t, positions)

			// A style-zoom change keeps the packed stable mesh too.
			if resident && indexed {
				next := build(3.0625, true)
				assert.Equal(t, fragmentMesh(packed, compiler.StableMesh), fragmentMesh(next, compiler.StableMesh))
			}
		}
	}
}

func TestCompactVerticesDrawTheSameTile(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		for _, resident := range []bool{false, true} {
			build := func(zoom float64, compact bool) *Fragment {
				p, err := Prepare(residentPBF(), residentStyle(t), PrepareOptions{Tile: testTile, Zoom: zoom, Indexed: indexed,
					ResidentGeometry: resident, ResidentSymbols: resident, ResidentDashes: resident, CompactVertices: compact})
				require.NoError(t, err)
				result, err := p.BuildOwned(prepareAssets(), 1<<20)
				require.NoError(t, err)
				return result.Fragment
			}
			plain, compact := build(3, false), build(3, true)
			assert.Equal(t, drawnVertices(t, plain), drawnVertices(t, compact), "indexed=%t resident=%t", indexed, resident)
			assert.Equal(t, plain.Draws, compact.Draws)
			assert.Equal(t, plain.Symbols, compact.Symbols)
			assert.Equal(t, plain.Scene.Textures, compact.Scene.Textures)
			require.Len(t, compact.Scene.Draws, len(plain.Scene.Draws))
			for i, draw := range compact.Scene.Draws {
				assert.Equal(t, plain.Scene.Draws[i].Material, draw.Material)
				assert.Equal(t, plain.Scene.Draws[i].Mesh, draw.Mesh)
				assert.Equal(t, plain.Scene.Draws[i].Count, draw.Count)
			}
			require.Len(t, compact.Scene.Meshes, len(plain.Scene.Meshes), "sections add no resource to upload")
			assert.Less(t, compact.RetainedBytes(), plain.RetainedBytes())
			var positions, offsets int
			for _, mesh := range compact.Scene.Meshes {
				positions, offsets = positions+len(mesh.Positions), offsets+len(mesh.Offsets)
			}
			assert.NotZero(t, positions, "fills carry positions only")
			assert.Equal(t, resident, offsets > 0, "only extruded lines carry an offset without a texture coordinate")

			// A style-zoom change keeps what it kept without compact vertices.
			if resident && indexed {
				next := build(3.0625, true)
				assert.Equal(t, fragmentMesh(compact, compiler.StableMesh), fragmentMesh(next, compiler.StableMesh))
			}
		}
	}
}
