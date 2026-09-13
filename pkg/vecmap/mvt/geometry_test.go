package mvt_test

import (
	"math"
	"slices"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodePoints(t *testing.T) {
	// Two MoveTo groups, with negative deltas and positions outside tile bounds.
	commands := []uint32{17, 31, 64, 64, 31, 9, 8192, 0}
	before := slices.Clone(commands)
	points, err := mvt.DecodePoints(commands, 4096)
	require.NoError(t, err)
	assert.Equal(t, []geometry.Point{{X: -1, Y: 2}, {X: 1, Y: 1}, {X: 257, Y: 1}}, points)
	assert.Equal(t, before, commands)
	clear(commands)
	assert.Equal(t, geometry.Point{X: -1, Y: 2}, points[0], "result must own its data")

	// The cursor must be int64: consecutive maximum positive deltas exceed int32.
	points, err = mvt.DecodePoints([]uint32{17, math.MaxUint32 - 1, math.MaxUint32, math.MaxUint32 - 1, 0}, 256)
	require.NoError(t, err)
	assert.Equal(t, []geometry.Point{{X: math.MaxInt32, Y: math.MinInt32}, {X: 2 * float64(math.MaxInt32), Y: math.MinInt32}}, points)
}

func TestDecodePaths(t *testing.T) {
	// MoveTo does not reset the delta cursor; repeated points are retained.
	lineCommands := []uint32{9, 32, 64, 18, 32, 0, 0, 0, 9, 63, 31, 10, 16, 16}
	lineBefore := slices.Clone(lineCommands)
	lines, err := mvt.DecodeLineStrings(lineCommands, 4096)
	require.NoError(t, err)
	wantLines := [][]geometry.Point{
		{{X: 1, Y: 2}, {X: 2, Y: 2}, {X: 2, Y: 2}},
		{{X: 0, Y: 1}, {X: .5, Y: 1.5}},
	}
	assert.Equal(t, wantLines, lines)
	assert.Equal(t, lineBefore, lineCommands)
	clear(lineCommands)
	assert.Equal(t, wantLines, lines)

	// Two rings of opposite winding. ClosePath neither emits nor resets a point.
	ringCommands := []uint32{9, 0, 0, 18, 128, 0, 0, 128, 15, 9, 95, 95, 18, 0, 32, 32, 0, 15}
	ringBefore := slices.Clone(ringCommands)
	rings, err := mvt.DecodePolygonRings(ringCommands, 256)
	require.NoError(t, err)
	wantRings := [][]geometry.Point{
		{{X: 0, Y: 0}, {X: 64, Y: 0}, {X: 64, Y: 64}},
		{{X: 16, Y: 16}, {X: 16, Y: 32}, {X: 32, Y: 32}},
	}
	assert.Equal(t, wantRings, rings)
	assert.Positive(t, geometry.SignedRingArea(rings[0]))
	assert.Negative(t, geometry.SignedRingArea(rings[1]))
	assert.Equal(t, ringBefore, ringCommands)
	clear(ringCommands)
	assert.Equal(t, wantRings, rings)
}

func TestDecodeMalformedCommands(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		commands []uint32
		message  string
	}{
		{"pointZeroCount", "points", []uint32{1}, "invalid command"},
		{"pointLineTo", "points", []uint32{10, 0, 0}, "invalid command"},
		{"pointTruncated", "points", []uint32{9, 0}, "truncated"},
		{"pointHugeCount", "points", []uint32{0xfffffff9}, "truncated"},
		{"pointPartial", "points", []uint32{9, 0, 0, 10}, "invalid command"},
		{"lineZeroCount", "lines", []uint32{1}, "zero count"},
		{"lineClosePath", "lines", []uint32{15}, "unsupported command"},
		{"lineMoveCount", "lines", []uint32{17, 0, 0, 0, 0}, "count is not one"},
		{"lineBeforeMove", "lines", []uint32{10, 0, 0}, "before MoveTo"},
		{"lineTruncated", "lines", []uint32{9, 0}, "truncated"},
		{"lineShort", "lines", []uint32{9, 0, 0}, "fewer than two"},
		{"lineShortBeforeNext", "lines", []uint32{9, 0, 0, 9, 0, 0}, "fewer than two"},
		{"linePartial", "lines", []uint32{9, 0, 0, 10, 2, 2, 9, 2, 2}, "fewer than two"},
		{"polygonZeroCount", "rings", []uint32{1}, "zero count"},
		{"polygonUnknown", "rings", []uint32{11}, "unsupported command"},
		{"polygonMoveCount", "rings", []uint32{17, 0, 0, 0, 0}, "count is not one"},
		{"polygonBeforeMove", "rings", []uint32{10, 0, 0}, "before MoveTo"},
		{"polygonNestedMove", "rings", []uint32{9, 0, 0, 9, 0, 0}, "before closing"},
		{"polygonTruncated", "rings", []uint32{9, 0, 0, 18, 2, 2}, "truncated"},
		{"polygonCloseCount", "rings", []uint32{23}, "count is not one"},
		{"polygonShort", "rings", []uint32{9, 0, 0, 15}, "fewer than three"},
		{"polygonOpen", "rings", []uint32{9, 0, 0, 18, 2, 0, 0, 2}, "not closed"},
		{"polygonPartial", "rings", []uint32{9, 0, 0, 18, 2, 0, 0, 2, 15, 9, 0, 0}, "not closed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decode(tt.kind, tt.commands, 4096)
			require.ErrorContains(t, err, tt.message)
			assert.Nil(t, result, "failure must not publish partial paths")
			assert.NotErrorIs(t, err, mvt.ErrFeatureResourceLimit)
			assert.NotErrorIs(t, err, geometry.ErrResourceLimit)
		})
	}
}

func TestDecodeEmptyAndInvalidExtent(t *testing.T) {
	for _, kind := range []string{"points", "lines", "rings"} {
		t.Run(kind, func(t *testing.T) {
			result, err := decode(kind, nil, 4096)
			require.NoError(t, err)
			assert.Nil(t, result)
			result, err = decode(kind, []uint32{9, 0, 0}, 0)
			require.ErrorContains(t, err, "invalid point")
			assert.Nil(t, result)
		})
	}
}

func TestDecodePointLimits(t *testing.T) {
	for _, kind := range []string{"points", "lines", "rings"} {
		t.Run(kind, func(t *testing.T) {
			limit := mvt.MaxGeometryPoints
			limitErr := mvt.ErrFeatureResourceLimit
			if kind == "rings" {
				limit = mvt.MaxPolygonPoints
				limitErr = geometry.ErrResourceLimit
			}
			for _, extra := range []int{0, 1} {
				commands := repeatedPoints(kind, limit+extra)
				result, err := decode(kind, commands, 4096)
				if extra != 0 {
					require.ErrorIs(t, err, limitErr)
					assert.Nil(t, result)
					continue
				}
				require.NoError(t, err)
				require.Len(t, result, 1)
				assert.Len(t, result[0], limit)
			}
			// Budgets span MoveTo groups/paths/rings, not just one command.
			commands := repeatedPoints(kind, limit-2)
			commands = append(commands, repeatedPoints(kind, 3)...)
			result, err := decode(kind, commands, 4096)
			require.ErrorIs(t, err, limitErr)
			assert.Nil(t, result)
		})
	}
}

// DecodePoints is adapted to the path shape solely for shared failure checks.
func decode(kind string, commands []uint32, extent uint32) ([][]geometry.Point, error) {
	switch kind {
	case "points":
		points, err := mvt.DecodePoints(commands, extent)
		if points == nil {
			return nil, err
		}
		return [][]geometry.Point{points}, err
	case "lines":
		return mvt.DecodeLineStrings(commands, extent)
	default:
		return mvt.DecodePolygonRings(commands, extent)
	}
}

func repeatedPoints(kind string, count int) []uint32 {
	if kind == "points" {
		commands := make([]uint32, 1+2*count)
		commands[0] = uint32(count)<<3 | 1
		return commands
	}
	commands := make([]uint32, 4+2*(count-1))
	commands[0] = 9
	commands[3] = uint32(count-1)<<3 | 2
	if kind == "rings" {
		commands = append(commands, 15)
	}
	return commands
}

func BenchmarkDecodeGeometry(b *testing.B) {
	b.Run("points", func(b *testing.B) {
		benchmarkDecode(b, "points", mvt.DecodePoints)
	})
	b.Run("lines", func(b *testing.B) {
		benchmarkDecode(b, "lines", mvt.DecodeLineStrings)
	})
	b.Run("rings", func(b *testing.B) {
		benchmarkDecode(b, "rings", mvt.DecodePolygonRings)
	})
}

func benchmarkDecode[T any](b *testing.B, kind string, decode func([]uint32, uint32) (T, error)) {
	b.Helper()
	commands := repeatedPoints(kind, 1024)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := decode(commands, 4096)
		if err != nil {
			b.Fatal(err)
		}
	}
}
