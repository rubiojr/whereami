package main

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
