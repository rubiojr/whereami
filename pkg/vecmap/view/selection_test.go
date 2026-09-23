package view

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func children(tile TileID) []TileID {
	return []TileID{{X: tile.X * 2, Y: tile.Y * 2, Z: tile.Z + 1}, {X: tile.X*2 + 1, Y: tile.Y * 2, Z: tile.Z + 1},
		{X: tile.X * 2, Y: tile.Y*2 + 1, Z: tile.Z + 1}, {X: tile.X*2 + 1, Y: tile.Y*2 + 1, Z: tile.Z + 1}}
}

func TestSelectCover(t *testing.T) {
	root := TileID{}
	parent := TileID{X: 1, Y: 1, Z: 2}
	targets := children(parent)
	deep := []TileID{targets[0]}
	for _, tile := range targets[1:] {
		deep = append(deep, children(tile)...)
	}
	ancestor, _ := parent.Parent()
	for _, tt := range []struct {
		name                           string
		targets, ready, previous, want []TileID
		fallbacks                      int
	}{
		{name: "empty"},
		{name: "missing", targets: targets},
		{name: "root", targets: []TileID{root}, ready: []TileID{root}, want: []TileID{root}},
		{name: "parent", targets: targets, ready: []TileID{parent, targets[0]}, want: []TileID{parent}, fallbacks: 1},
		{name: "complete siblings", targets: targets, ready: append([]TileID{parent}, targets...), previous: []TileID{parent}, want: targets},
		{name: "requested subset", targets: targets[:2], ready: append([]TileID{parent}, targets[:2]...), want: targets[:2]},
		{name: "partial children", targets: []TileID{parent}, ready: targets[:2], previous: targets[:2], want: targets[:2], fallbacks: 2},
		{name: "detailed continuity before parent", targets: []TileID{parent}, ready: append([]TileID{ancestor}, deep...), previous: deep, want: deep, fallbacks: len(deep)},
		{name: "nearest continuity", targets: targets, ready: []TileID{root, ancestor}, previous: []TileID{root, ancestor}, want: []TileID{ancestor}, fallbacks: 1},
		{name: "stale continuity", targets: targets, ready: targets[:1], previous: []TileID{parent}, want: targets[:1]},
		{name: "retained target is not fallback", targets: targets[:2], ready: targets[:1], previous: targets[:1], want: targets[:1]},
		{name: "partial ancestors deduplicate", targets: []TileID{targets[0], {X: 6, Y: 6, Z: 3}}, ready: []TileID{ancestor}, previous: []TileID{ancestor}, want: []TileID{ancestor}, fallbacks: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ready := make(map[TileID]bool)
			for _, tile := range tt.ready {
				ready[tile] = true
			}
			got, fallbacks := SelectCover(tt.targets, tt.previous, func(tile TileID) bool { return ready[tile] })
			assert.Equal(t, tt.want, append([]TileID(nil), got...))
			assert.Equal(t, tt.fallbacks, fallbacks)
			for i, tile := range got {
				assert.True(t, ready[tile])
				for _, other := range got[:i] {
					assert.False(t, TilesOverlap(tile, other))
				}
			}
		})
	}
	got, count := SelectCover(targets, nil, nil)
	assert.Nil(t, got)
	assert.Zero(t, count)
}
