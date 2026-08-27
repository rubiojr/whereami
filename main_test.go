package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapCacheDirFollowsCacheDir(t *testing.T) {
	previous := cacheDir
	t.Cleanup(func() { cacheDir = previous })

	cacheDir = filepath.Join(t.TempDir(), "custom")
	dir := mapCacheDir()

	assert.Equal(t, filepath.Join(cacheDir, "maplibre"), dir)
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestMapCacheDirReportsFailureAsEmpty(t *testing.T) {
	previous := cacheDir
	t.Cleanup(func() { cacheDir = previous })

	// A regular file where the cache directory must go makes creation fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a directory"), 0o600))
	cacheDir = blocker

	assert.Empty(t, mapCacheDir())
}
