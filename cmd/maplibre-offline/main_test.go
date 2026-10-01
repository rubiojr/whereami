package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/producer"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOfflineStyleKeepsLayers(t *testing.T) {
	data, _, _ := liberty.Files()
	offline, err := offlineStyle(data, "file:///cache/{z}/{x}/{y}.pbf", "/assets")
	require.NoError(t, err)
	var original, rewritten map[string]any
	require.NoError(t, json.Unmarshal(data, &original))
	require.NoError(t, json.Unmarshal(offline, &rewritten))
	assert.Equal(t, original["layers"], rewritten["layers"])
	assert.Equal(t, "file:///assets/sprites/ofm", rewritten["sprite"])
	assert.Equal(t, "file:///assets/fonts/{fontstack}/{range}.pbf", rewritten["glyphs"])
	sources := rewritten["sources"].(map[string]any)
	assert.Equal(t, map[string]any{"type": "vector", "minzoom": 0.0, "maxzoom": 14.0,
		"tiles": []any{"file:///cache/{z}/{x}/{y}.pbf"}}, sources["openmaptiles"])
	assert.Equal(t, []any{"file:///assets/missing/{z}/{x}/{y}.png"}, sources["ne2_shaded"].(map[string]any)["tiles"])
	assert.NotContains(t, string(offline), "https://")
}

func TestOfflineStyleRejectsOtherSources(t *testing.T) {
	_, err := offlineStyle([]byte(`{"sources":{"openmaptiles":{}}}`), "", "/assets")
	assert.Error(t, err)
}

func TestParseTile(t *testing.T) {
	tile, ok := parseTile("/tiles/10/501/387.pbf")
	assert.True(t, ok)
	assert.Equal(t, view.TileID{Z: 10, X: 501, Y: 387}, tile)
	for _, path := range []string{"/tiles/10/501.pbf", "/tiles/15/0/0.pbf", "/tiles/2/4/0.pbf", "/tiles/a/0/0.pbf", "/tiles/1/0/0.png", "/other/1/0/0.pbf"} {
		_, ok := parseTile(path)
		assert.False(t, ok, path)
	}
}

func TestTileHandlerStatus(t *testing.T) {
	handler := tileHandler(func(_ context.Context, key producer.Key, w io.Writer) error {
		if key.Tile.Z == 1 {
			return producer.ErrMissing
		}
		_, err := w.Write([]byte("tile"))
		return err
	})
	for path, status := range map[string]int{"/tiles/0/0/0.pbf": http.StatusOK, "/tiles/1/0/0.pbf": http.StatusNotFound, "/style.json": http.StatusNotFound} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, status, recorder.Code, path)
	}
}

func TestWriteAssetsNeedsPinnedTile(t *testing.T) {
	glyphs := t.TempDir()
	for _, stack := range fontStacks {
		require.NoError(t, os.WriteFile(filepath.Join(glyphs, url.PathEscape(stack)+".pbf"), []byte("glyphs"), 0o644))
	}
	out := t.TempDir()
	err := writeAssets(t.TempDir(), glyphs, out)
	assert.ErrorContains(t, err, "pinned tile")
	data, err := os.ReadFile(filepath.Join(out, "fonts", "Noto Sans Bold", "0-255.pbf"))
	require.NoError(t, err)
	assert.Equal(t, "glyphs", string(data))
	empty, err := os.ReadFile(filepath.Join(out, "fonts", "Noto Sans Italic", "65280-65535.pbf"))
	require.NoError(t, err)
	assert.Empty(t, empty)
}
