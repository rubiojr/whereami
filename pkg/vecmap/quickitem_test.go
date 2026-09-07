package vecmap

import (
	"math"
	"testing"

	quick "github.com/rubiojr/whereami/internal/miqtquick"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackgroundTrianglesCoverTile(t *testing.T) {
	triangles := backgroundTriangles()

	assert.Len(t, triangles, 6)
	assert.InDelta(t, tileSize*tileSize, triangleMeshArea(triangles), 1e-9)
}

func TestPointVerticesPreserveTileCoordinates(t *testing.T) {
	assert.Equal(t, []float32{-1, 2, 3.5, 4.25}, pointVertices([]roadPoint{
		{X: -1, Y: 2},
		{X: 3.5, Y: 4.25},
	}))
}

func TestSegmentPointsPreserveLineOrder(t *testing.T) {
	segments := []roadSegment{
		{Start: roadPoint{X: 1, Y: 2}, End: roadPoint{X: 3, Y: 4}},
		{Start: roadPoint{X: 5, Y: 6}, End: roadPoint{X: 7, Y: 8}},
	}

	assert.Equal(t, []roadPoint{
		{X: 1, Y: 2}, {X: 3, Y: 4},
		{X: 5, Y: 6}, {X: 7, Y: 8},
	}, segmentPoints(segments))
}

func TestAppendTileContentsBuildsEveryLayer(t *testing.T) {
	parent := quick.NewQSGNode()
	require.NotNil(t, parent)
	defer parent.Delete()
	triangle := []roadPoint{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}}
	bucket := &tileBucket{
		segments: []roadSegment{{Start: roadPoint{X: 0, Y: 0}, End: roadPoint{X: 1, Y: 1}}},
		land:     fillBucket{triangles: triangle, cutouts: triangle},
		water:    fillBucket{triangles: triangle, cutouts: triangle},
	}

	appendTileContents(parent, bucket)

	assert.Equal(t, 6, parent.ChildCount())
}

func TestAppendGeometryNodeRejectsIncompletePrimitives(t *testing.T) {
	parent := quick.NewQSGNode()
	require.NotNil(t, parent)
	defer parent.Delete()

	appendGeometryNode(parent, []roadPoint{{}, {}}, quick.QSGGeometry__DrawTriangles, mapLandColor)
	appendGeometryNode(parent, []roadPoint{{}}, quick.QSGGeometry__DrawLines, mapRoadColor)

	assert.Zero(t, parent.ChildCount())
}

func TestLibertyWorldWrapsCoverWideLowZoomViewport(t *testing.T) {
	lowZoom := NewCamera(Coordinate{}, 0, 0, 1300, 800)
	wraps := libertyWorldWraps(lowZoom)
	assert.Contains(t, wraps, -3)
	assert.Contains(t, wraps, 0)
	assert.Contains(t, wraps, 3)

	highZoom := NewCamera(Coordinate{}, 9, 0, 1300, 800)
	assert.Equal(t, []int{0}, libertyWorldWraps(highZoom))

	pathological := NewCamera(Coordinate{}, 0, 0, 1300, 800)
	pathological.Zoom = -20
	assert.LessOrEqual(t, len(libertyWorldWraps(pathological)), 17)

	belowThreshold := NewCamera(Coordinate{}, 1.49, 0, 256, 256)
	aboveThreshold := NewCamera(Coordinate{}, 1.51, 0, 256, 256)
	assert.Equal(t, math.Round(belowThreshold.Zoom*16), math.Round(aboveThreshold.Zoom*16))
	assert.NotEqual(t, libertyWorldWraps(belowThreshold), libertyWorldWraps(aboveThreshold))
}

func TestLibertySymbolPlacementTracksZoomAndBearingNotCenter(t *testing.T) {
	item := &Item{}
	camera := NewCamera(Coordinate{}, 10, 0, 256, 256)
	assert.True(t, item.libertySymbolPlacementChanged(camera))
	item.recordLibertySymbolPlacement(camera)

	smallPan := camera.Panned(libertyCollisionPanStep/2, 0)
	assert.False(t, item.libertySymbolPlacementChanged(smallPan))
	largePan := camera.Panned(libertyCollisionPanStep, 0)
	assert.True(t, item.libertySymbolPlacementChanged(largePan))

	smallZoom := camera
	smallZoom.Zoom += libertyCollisionZoomStep / 2
	assert.False(t, item.libertySymbolPlacementChanged(smallZoom))
	largeZoom := camera
	largeZoom.Zoom += libertyCollisionZoomStep
	assert.True(t, item.libertySymbolPlacementChanged(largeZoom))

	smallBearing := camera
	smallBearing.Bearing = libertyCollisionBearingStep / 2
	assert.False(t, item.libertySymbolPlacementChanged(smallBearing))
	largeBearing := camera
	largeBearing.Bearing = libertyCollisionBearingStep
	assert.True(t, item.libertySymbolPlacementChanged(largeBearing))
}

func TestWrappedRoadCameraTransformOffsetsOneWorld(t *testing.T) {
	camera := NewCamera(Coordinate{}, 2, 37, 300, 200)
	tile := vectorTileID{X: 0, Y: 2, Z: 2}
	base := wrappedRoadCameraTransform(camera, tile, 0).mapPoint(roadPoint{})
	wrapped := wrappedRoadCameraTransform(camera, tile, 1).mapPoint(roadPoint{})

	assert.InDelta(t, tileSize*math.Exp2(camera.Zoom)*math.Cos(-camera.Bearing*math.Pi/180), wrapped.X-base.X, 1e-8)
	assert.InDelta(t, tileSize*math.Exp2(camera.Zoom)*math.Sin(-camera.Bearing*math.Pi/180), wrapped.Y-base.Y, 1e-8)
}

func TestResetRetainedSceneGraphDropsInvalidatedPointers(t *testing.T) {
	item := &Item{
		sceneNodes:       []*quick.QSGNode{{}},
		symbolTransforms: []retainedLibertySymbolTransform{{node: &quick.QSGTransformNode{}}},
		tileNodes:        map[vectorTileID][]retainedTileTransform{{}: {{}}},
		retainedTiles:    map[vectorTileID]struct{}{{}: {}},
	}

	item.resetRetainedSceneGraph()

	assert.Empty(t, item.sceneNodes)
	assert.Empty(t, item.symbolTransforms)
	assert.Empty(t, item.tileNodes)
	assert.Empty(t, item.retainedTiles)
}

func TestRasterSelectsLibertyRenderer(t *testing.T) {
	tiles := []loadedRoadTile{{roads: &tileBucket{raster: naturalEarthRaster{rgba: []byte{1}}}}}
	assert.True(t, hasLibertyPrimitives(tiles))
}

func TestLibertyPatternPhaseContinuesAcrossTilesAndWraps(t *testing.T) {
	tile := vectorTileID{X: 3, Y: 1, Z: 2}
	x, y := libertyPatternPhase(tile, 1, 15, 15)
	assert.Equal(t, math.Mod(7*tileSize, 15), x)
	assert.Equal(t, math.Mod(tileSize, 15), y)

	x, _ = libertyPatternPhase(tile, -1, 15, 15)
	assert.Equal(t, math.Mod(-tileSize, 15), x)
}
