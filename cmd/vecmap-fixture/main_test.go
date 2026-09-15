package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPinnedHeadless(t *testing.T) {
	path, dir := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE"), os.Getenv("WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	if path == "" || dir == "" {
		t.Skip("set pinned tile and glyph fixture environment variables")
	}
	out := filepath.Join(t.TempDir(), "scene.json")
	require.NoError(t, run(path, dir, out, false, true))
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	var document scene.Document
	require.NoError(t, json.Unmarshal(data, &document))
	require.NoError(t, document.Validate())
	assert.Equal(t, 62, document.Labels)
	assert.Len(t, document.Scene.Draws, 45)
	assert.Len(t, document.Scene.Meshes[0].Indices, 782409)
	assert.Empty(t, document.MissingFonts)
	assert.ErrorContains(t, run(path, dir, out, true, true), "mutually exclusive")
}

func TestLoadFixtureGlyphs(t *testing.T) {
	dir := t.TempDir()
	font := "Noto Sans/Regular"
	require.NoError(t, os.WriteFile(filepath.Join(dir, url.PathEscape(font)+".pbf"), []byte("range"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unused.pbf"), []byte("not a glyph range"), 0600))
	glyphs, err := loadFixtureGlyphs(dir, []string{font, "missing"})
	require.NoError(t, err)
	assert.Equal(t, map[string][]byte{font: []byte("range")}, glyphs)
	missing, err := loadFixtureGlyphs(filepath.Join(dir, "absent-directory"), []string{font})
	require.NoError(t, err)
	assert.Empty(t, missing)
}

func TestLoadFixtureGlyphsReadError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "broken.pbf"), 0700))
	glyphs, err := loadFixtureGlyphs(dir, []string{"broken"})
	require.Error(t, err)
	assert.Nil(t, glyphs)
}
