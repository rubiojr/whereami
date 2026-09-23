package main

import (
	"errors"
	"os"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/fixture"
	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetainedFixturePins(t *testing.T) {
	result, err := compileRetainedFixture([]byte("not the fixture"), nil, fixture.Options{})
	assert.ErrorContains(t, err, "checksum mismatch")
	assert.Nil(t, result)
	result, err = compileRetainedFixture(make([]byte, mvt.MaxTileBytes+1), nil, fixture.Options{})
	assert.ErrorIs(t, err, mvt.ErrTileResourceLimit)
	assert.Nil(t, result)
}

func TestRetainedFixtureLoaderFailures(t *testing.T) {
	path := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE")
	if path == "" {
		t.Skip("set pinned tile fixture environment variable")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	failure := errors.New("glyph loader failed")
	result, err := compileRetainedFixture(data, func([]string) (map[string][]byte, error) { return nil, failure }, fixture.Options{})
	assert.ErrorIs(t, err, failure)
	assert.Nil(t, result)
	result, err = compileRetainedFixture(data, func([]string) (map[string][]byte, error) {
		return map[string][]byte{"Noto Sans Regular": {0}}, nil
	}, fixture.Options{})
	assert.Error(t, err)
	assert.Nil(t, result)
	result, err = compileRetainedFixture(data, nil, fixture.Options{DirectIndexed: true})
	require.NoError(t, err)
	assert.NotEmpty(t, result.MissingFonts)
}
