package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneDiskEnforcesEntryLimitWithoutTTL(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.png")
	newPath := filepath.Join(dir, "new.png")
	for _, path := range []string{oldPath, newPath} {
		if err := os.WriteFile(path, []byte("tile"), 0o644); err != nil {
			t.Fatalf("write tile fixture: %v", err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(oldPath, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("set old tile time: %v", err)
	}
	if err := os.Chtimes(newPath, now, now); err != nil {
		t.Fatalf("set new tile time: %v", err)
	}

	proxy := tileProxy{
		diskDir:    dir,
		diskTTL:    0,
		maxEntries: 1,
		maxBytes:   1024,
	}
	proxy.pruneDisk()

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old tile still exists after pruning: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new tile was removed: %v", err)
	}
}

func TestCopyImportedGPXClosesFilesAndRemovesPartialDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.gpx")
	destination := filepath.Join(root, "destination.gpx")
	require.NoError(t, os.WriteFile(source, []byte("gpx data"), 0o600))
	require.NoError(t, copyImportedGPX(source, destination))
	content, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, []byte("gpx data"), content)
	info, err := os.Stat(destination)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	err = copyImportedGPX(source, destination)
	require.ErrorIs(t, err, os.ErrExist)
	content, err = os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, []byte("gpx data"), content)

	partial := filepath.Join(root, "partial.gpx")
	err = copyImportedGPX(root, partial)
	require.Error(t, err)
	_, statErr := os.Stat(partial)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestImportGPXDirectoryPreservesRelativePathsAndContentChanges(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	imports := filepath.Join(root, "imports")
	require.NoError(t, os.MkdirAll(filepath.Join(source, "2024"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(source, "2025"), 0o700))
	require.NoError(t, os.MkdirAll(imports, 0o700))
	writeImportGPX(t, filepath.Join(source, "2024", "track.gpx"), "first", 41)
	writeImportGPX(t, filepath.Join(source, "2025", "track.gpx"), "second", 42)

	result, err := importGPXDirectory(source, imports, true)
	require.NoError(t, err)
	require.Len(t, result.imported, 2)
	assert.NotNil(t, result.duplicates)
	assert.NotNil(t, result.unsupported)
	assert.NotNil(t, result.failed)
	assert.Empty(t, result.duplicates)
	assert.Empty(t, result.unsupported)
	assert.Empty(t, result.failed)
	assert.FileExists(t, filepath.Join(imports, "2024", "track.gpx"))
	assert.FileExists(t, filepath.Join(imports, "2025", "track.gpx"))
	for _, importedFile := range result.imported {
		info, statErr := os.Stat(importedFile.path)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	info, err := os.Stat(filepath.Join(imports, "2024"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	result, err = importGPXDirectory(source, imports, true)
	require.NoError(t, err)
	assert.Empty(t, result.imported)
	assert.ElementsMatch(t, []string{"2024/track.gpx", "2025/track.gpx"}, result.duplicates)

	writeImportGPX(t, filepath.Join(source, "2024", "track.gpx"), "updated", 43)
	result, err = importGPXDirectory(source, imports, true)
	require.NoError(t, err)
	require.Len(t, result.imported, 1)
	assert.Equal(t, []string{"2025/track.gpx"}, result.duplicates)
	assert.Equal(t, filepath.Join(imports, "2024", "track.gpx"), result.imported[0].path)
	assert.True(t, result.imported[0].replaced)
	assert.Equal(t, "updated", result.imported[0].waypoints[0].Name)
	waypoints, err := parseGPXFile(filepath.Join(imports, "2024", "track.gpx"))
	require.NoError(t, err)
	require.Len(t, waypoints, 1)
	assert.Equal(t, "updated", waypoints[0].Name)
	entries, err := os.ReadDir(filepath.Join(imports, "2024"))
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestImportGPXDirectoryReportsInvalidFilesAndCommitsValidFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	imports := filepath.Join(root, "imports")
	require.NoError(t, os.MkdirAll(source, 0o700))
	require.NoError(t, os.MkdirAll(imports, 0o700))
	writeImportGPX(t, filepath.Join(source, "a-valid.gpx"), "valid", 41)
	require.NoError(t, os.WriteFile(filepath.Join(source, "z-invalid.gpx"), []byte("not GPX"), 0o600))

	result, err := importGPXDirectory(source, imports, true)
	require.NoError(t, err)
	require.Len(t, result.imported, 1)
	assert.Equal(t, []string{"z-invalid.gpx"}, result.failed)
	assert.FileExists(t, filepath.Join(imports, "a-valid.gpx"))
	assert.NoFileExists(t, filepath.Join(imports, "z-invalid.gpx"))
}

func TestImportGPXDirectorySkipsSymlinkedGPX(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	imports := filepath.Join(root, "imports")
	require.NoError(t, os.MkdirAll(source, 0o700))
	require.NoError(t, os.MkdirAll(imports, 0o700))
	target := filepath.Join(root, "private.gpx")
	writeImportGPX(t, target, "private", 41)
	link := filepath.Join(source, "linked.gpx")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	result, err := importGPXDirectory(source, imports, true)
	require.NoError(t, err)
	assert.Empty(t, result.imported)
	assert.Equal(t, []string{"linked.gpx"}, result.unsupported)
	assert.NoFileExists(t, filepath.Join(imports, "linked.gpx"))
}

func TestImportGPXDirectoryCleansStaleStagingAndRejectsDestinationSymlink(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	imports := filepath.Join(root, "imports")
	require.NoError(t, os.MkdirAll(source, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(imports, ".staging-orphan"), 0o700))
	writeImportGPX(t, filepath.Join(imports, ".staging-orphan", "orphan.gpx"), "orphan", 40)
	writeImportGPX(t, filepath.Join(source, "track.gpx"), "source", 41)
	target := filepath.Join(root, "target.gpx")
	writeImportGPX(t, target, "target", 42)
	require.NoError(t, os.Symlink(target, filepath.Join(imports, "track.gpx")))

	result, err := importGPXDirectory(source, imports, true)
	require.NoError(t, err)
	assert.Empty(t, result.imported)
	assert.Equal(t, []string{"track.gpx"}, result.unsupported)
	assert.NoDirExists(t, filepath.Join(imports, ".staging-orphan"))
	waypoints, parseErr := parseGPXFile(target)
	require.NoError(t, parseErr)
	assert.Equal(t, "target", waypoints[0].Name)
}

func TestImportGPXDirectoryRejectsSymlinkedDestinationParent(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	imports := filepath.Join(root, "imports")
	external := filepath.Join(root, "external")
	writeImportGPX(t, filepath.Join(source, "nested", "track.gpx"), "source", 41)
	require.NoError(t, os.MkdirAll(imports, 0o700))
	require.NoError(t, os.MkdirAll(external, 0o700))
	require.NoError(t, os.Symlink(external, filepath.Join(imports, "nested")))

	_, err := importGPXDirectory(source, imports, true)
	require.ErrorContains(t, err, "parent is not a directory")
	assert.NoFileExists(t, filepath.Join(external, "track.gpx"))
}

func TestImportGPXDirectoryClassifiesCaseCollisions(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	imports := filepath.Join(root, "imports")
	writeImportGPX(t, filepath.Join(source, "track.gpx"), "lower", 41)
	writeImportGPX(t, filepath.Join(source, "TRACK.GPX"), "upper", 42)
	require.NoError(t, os.MkdirAll(imports, 0o700))

	result, err := importGPXDirectory(source, imports, true)
	require.NoError(t, err)
	assert.Len(t, result.imported, 1)
	assert.Len(t, result.unsupported, 1)
}

func TestCanonicalImportPathsRejectsSymlinkedImportDirectory(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	external := filepath.Join(root, "external")
	require.NoError(t, os.MkdirAll(dataRoot, 0o700))
	require.NoError(t, os.MkdirAll(external, 0o700))
	require.NoError(t, os.Symlink(external, filepath.Join(dataRoot, "imports")))

	_, _, err := canonicalImportPaths(dataRoot, filepath.Join(dataRoot, "imports"))
	require.ErrorContains(t, err, "outside the data root")
}

func writeImportGPX(t *testing.T, path, name string, latitude float64) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	content := fmt.Sprintf(`<?xml version="1.0"?><gpx version="1.1"><wpt lat="%g" lon="2"><name>%s</name></wpt></gpx>`, latitude, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
