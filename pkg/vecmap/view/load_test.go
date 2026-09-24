package view

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadOrder(t *testing.T) {
	a, b, c := TileID{Z: 3, X: 3, Y: 4}, TileID{Z: 3, X: 2, Y: 4}, TileID{Z: 3, X: 5, Y: 4}
	pa, _ := a.Parent()
	pc, _ := c.Parent()
	assert.Equal(t, []TileID{pa, pc, a, b, c}, LoadOrder([]TileID{a, b, c, a}))
	assert.Equal(t, []TileID{{}}, LoadOrder([]TileID{{}, {}}))
	assert.Empty(t, LoadOrder(nil))
}
