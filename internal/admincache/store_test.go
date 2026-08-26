package admincache

import (
	"context"
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorePersistsVersionedPositiveAndNegativeResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "administrative.sqlite")
	coordinate := admingeo.Coordinate{Longitude: 2.1734, Latitude: 41.3851}
	negative := admingeo.Coordinate{Longitude: -30, Latitude: 20}
	want := admingeo.AdminPath{
		Country: "Spain", CountryID: "ES",
		Region: "Catalonia", RegionID: "ES-CT",
		County: "Barcelona", CountyID: "county-id",
		LocalAdmin: "Barcelona", LocalAdminID: "admin-id",
		Locality: "Barcelona", LocalityID: "locality-id",
	}

	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Put(context.Background(), "dataset-v1", []Entry{
		{Coordinate: coordinate, Path: want},
		{Coordinate: negative, Path: admingeo.AdminPath{}},
	}))

	got, found, err := store.Lookup(context.Background(), "dataset-v1", coordinate)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, want, got)
	got, found, err = store.Lookup(context.Background(), "dataset-v1", negative)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, admingeo.AdminPath{}, got)
	_, found, err = store.Lookup(context.Background(), "dataset-v2", coordinate)
	require.NoError(t, err)
	assert.False(t, found)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	got, found, err = store.Lookup(context.Background(), "dataset-v1", coordinate)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, want, got)
}

func TestStoreKeysCoordinatesByExactFloatBits(t *testing.T) {
	store := openTestStore(t)
	base := admingeo.Coordinate{Longitude: 1, Latitude: 2}
	next := admingeo.Coordinate{Longitude: math.Nextafter(1, 2), Latitude: 2}
	positiveZero := admingeo.Coordinate{Longitude: 0, Latitude: 3}
	negativeZero := admingeo.Coordinate{Longitude: math.Copysign(0, -1), Latitude: 3}
	entries := []Entry{
		{Coordinate: base, Path: admingeo.AdminPath{Country: "base"}},
		{Coordinate: next, Path: admingeo.AdminPath{Country: "next"}},
		{Coordinate: positiveZero, Path: admingeo.AdminPath{Country: "positive-zero"}},
		{Coordinate: negativeZero, Path: admingeo.AdminPath{Country: "negative-zero"}},
	}
	require.NoError(t, store.Put(context.Background(), "dataset-v1", entries))

	for _, entry := range entries {
		got, found, err := store.Lookup(context.Background(), "dataset-v1", entry.Coordinate)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, entry.Path, got)
	}
}

func TestStoreWarmCompletionUsesDatasetAndObservationVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.sqlite")
	store, err := Open(path)
	require.NoError(t, err)

	complete, err := store.WarmComplete(context.Background(), "dataset-v1", "observations-v1")
	require.NoError(t, err)
	assert.False(t, complete)
	require.NoError(t, store.MarkWarmComplete(context.Background(), "dataset-v1", "observations-v1"))
	for _, key := range [][2]string{
		{"dataset-v1", "observations-v1"},
		{"dataset-v2", "observations-v1"},
		{"dataset-v1", "observations-v2"},
	} {
		complete, err = store.WarmComplete(context.Background(), admingeo.DatasetVersion(key[0]), key[1])
		require.NoError(t, err)
		assert.Equal(t, key == [2]string{"dataset-v1", "observations-v1"}, complete)
	}
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	complete, err = store.WarmComplete(context.Background(), "dataset-v1", "observations-v1")
	require.NoError(t, err)
	assert.True(t, complete)
}

func TestStorePrunesUnretainedDatasetVersions(t *testing.T) {
	store := openTestStore(t)
	coordinate := admingeo.Coordinate{Longitude: 2, Latitude: 41}
	for _, version := range []admingeo.DatasetVersion{"dataset-v1", "dataset-v2", "dataset-v3"} {
		require.NoError(t, store.Put(context.Background(), version, []Entry{{
			Coordinate: coordinate,
			Path:       admingeo.AdminPath{Country: string(version)},
		}}))
		require.NoError(t, store.MarkWarmComplete(context.Background(), version, "observations-v1"))
	}

	require.NoError(t, store.PruneVersions(context.Background(), "dataset-v2", "dataset-v3", "dataset-v3"))
	for _, version := range []admingeo.DatasetVersion{"dataset-v1", "dataset-v2", "dataset-v3"} {
		_, found, err := store.Lookup(context.Background(), version, coordinate)
		require.NoError(t, err)
		assert.Equal(t, version != "dataset-v1", found)
		complete, err := store.WarmComplete(context.Background(), version, "observations-v1")
		require.NoError(t, err)
		assert.Equal(t, version != "dataset-v1", complete)
	}
}

func TestStoreSecuresParentAndDatabase(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "private-cache")
	require.NoError(t, os.Mkdir(parent, 0o755))
	path := filepath.Join(parent, "administrative.sqlite")
	store, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	parentInfo, err := os.Stat(parent)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), parentInfo.Mode().Perm())
	databaseInfo, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), databaseInfo.Mode().Perm())
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecarInfo, statErr := os.Stat(path + suffix)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), sidecarInfo.Mode().Perm())
	}
}

func TestStoreUsesWALBusyTimeoutAndStrictVersionedSchema(t *testing.T) {
	store := openTestStore(t)
	var version, busyTimeout int
	var journalMode string
	require.NoError(t, store.db.QueryRow(`PRAGMA user_version`).Scan(&version))
	require.NoError(t, store.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode))
	require.NoError(t, store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout))
	assert.Equal(t, schemaVersion, version)
	assert.Equal(t, "wal", journalMode)
	assert.Equal(t, 5000, busyTimeout)

	strictTables := make(map[string]int)
	rows, err := store.db.Query(`PRAGMA table_list`)
	require.NoError(t, err)
	for rows.Next() {
		var schema, name, tableType string
		var columns, withoutRowID, strict int
		require.NoError(t, rows.Scan(&schema, &name, &tableType, &columns, &withoutRowID, &strict))
		if schema == "main" && (name == "resolutions" || name == "warm_completions") {
			strictTables[name] = strict
			assert.Equal(t, 1, withoutRowID)
		}
	}
	require.NoError(t, rows.Close())
	assert.Equal(t, map[string]int{"resolutions": 1, "warm_completions": 1}, strictTables)

	_, err = store.db.Exec(`INSERT INTO resolutions VALUES(
		'dataset', 1, X'0000000000000000', '', '', '', '', '', '', '', '', '', ''
	)`)
	assert.Error(t, err)
}

func TestStoreRejectsUnsupportedOrMalformedSchemas(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *sql.DB)
		want  string
	}{
		{
			name: "unsupported version",
			setup: func(t *testing.T, db *sql.DB) {
				t.Helper()
				_, err := db.Exec(`PRAGMA user_version = 99`)
				require.NoError(t, err)
			},
			want: "unsupported schema version 99",
		},
		{
			name: "unversioned foreign database",
			setup: func(t *testing.T, db *sql.DB) {
				t.Helper()
				_, err := db.Exec(`CREATE TABLE unrelated(value TEXT)`)
				require.NoError(t, err)
			},
			want: "unversioned database is not empty",
		},
		{
			name: "malformed current schema",
			setup: func(t *testing.T, db *sql.DB) {
				t.Helper()
				_, err := db.Exec(`CREATE TABLE resolutions(value TEXT) STRICT; PRAGMA user_version = 1`)
				require.NoError(t, err)
			},
			want: `table "resolutions" is not STRICT WITHOUT ROWID`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cache.sqlite")
			db, err := sql.Open("sqlite", path)
			require.NoError(t, err)
			test.setup(t, db)
			require.NoError(t, db.Close())

			store, err := Open(path)
			assert.Nil(t, store)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestStoreOperationsHonorContext(t *testing.T) {
	store := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := store.Lookup(ctx, "dataset-v1", admingeo.Coordinate{})
	require.ErrorIs(t, err, context.Canceled)
	err = store.Put(ctx, "dataset-v1", []Entry{{}})
	require.ErrorIs(t, err, context.Canceled)
	_, err = store.WarmComplete(ctx, "dataset-v1", "observations-v1")
	require.ErrorIs(t, err, context.Canceled)
	err = store.MarkWarmComplete(ctx, "dataset-v1", "observations-v1")
	require.ErrorIs(t, err, context.Canceled)
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "cache", "administrative.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}
