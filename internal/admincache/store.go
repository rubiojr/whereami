// Package admincache persists administrative geocoding results and warms them
// from indexed observations.
package admincache

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rubiojr/whereami/internal/admingeo"
	_ "modernc.org/sqlite"
)

const schemaVersion = 1

var errStoreClosed = errors.New("admincache: store is closed")

// Entry is one successfully resolved coordinate. An empty Path is a negative
// result and is persisted like any other successful resolution.
type Entry struct {
	Coordinate admingeo.Coordinate
	Path       admingeo.AdminPath
}

// Store is a concurrency-safe private SQLite resolution cache.
type Store struct {
	mu       sync.RWMutex
	db       *sql.DB
	closed   bool
	closeErr error
}

// Open opens or creates a cache database at path.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("admincache: database path is empty")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("admincache: resolve database path: %w", err)
	}
	if err := prepareParent(filepath.Dir(absPath)); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", sqliteDSN(absPath))
	if err != nil {
		return nil, fmt.Errorf("admincache: open database: %w", err)
	}
	db.SetMaxOpenConns(4)
	keepDB := false
	defer func() {
		if !keepDB {
			_ = db.Close()
		}
	}()
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("admincache: connect to database: %w", err)
	}
	if err := os.Chmod(absPath, 0o600); err != nil {
		return nil, fmt.Errorf("admincache: secure database: %w", err)
	}

	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode = WAL`).Scan(&journalMode); err != nil {
		return nil, fmt.Errorf("admincache: enable WAL: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return nil, fmt.Errorf("admincache: enable WAL: SQLite selected %q", journalMode)
	}
	if err := initializeSchema(db); err != nil {
		return nil, err
	}

	keepDB = true
	return &Store{db: db}, nil
}

// Lookup returns the cached result for an exact coordinate and dataset.
func (s *Store) Lookup(ctx context.Context, version admingeo.DatasetVersion, coordinate admingeo.Coordinate) (admingeo.AdminPath, bool, error) {
	if err := validateContextAndVersion(ctx, version); err != nil {
		return admingeo.AdminPath{}, false, err
	}
	longitude, latitude := coordinateKey(coordinate)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return admingeo.AdminPath{}, false, errStoreClosed
	}

	var path admingeo.AdminPath
	err := s.db.QueryRowContext(ctx, `SELECT
		country, country_id, region, region_id, county, county_id,
		local_admin, local_admin_id, locality, locality_id
		FROM resolutions
		WHERE dataset_version = ? AND longitude = ? AND latitude = ?`,
		string(version), longitude[:], latitude[:]).Scan(
		&path.Country, &path.CountryID, &path.Region, &path.RegionID,
		&path.County, &path.CountyID, &path.LocalAdmin, &path.LocalAdminID,
		&path.Locality, &path.LocalityID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return admingeo.AdminPath{}, false, nil
	}
	if err != nil {
		return admingeo.AdminPath{}, false, fmt.Errorf("admincache: lookup resolution: %w", err)
	}
	return path, true, nil
}

// Put atomically stores a batch of successful resolutions for one dataset.
func (s *Store) Put(ctx context.Context, version admingeo.DatasetVersion, entries []Entry) error {
	if err := validateContextAndVersion(ctx, version); err != nil {
		return err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return errStoreClosed
	}
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("admincache: begin resolution batch: %w", err)
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, putResolutionSQL)
	if err != nil {
		return fmt.Errorf("admincache: prepare resolution batch: %w", err)
	}
	defer statement.Close()

	for _, entry := range entries {
		longitude, latitude := coordinateKey(entry.Coordinate)
		path := entry.Path
		if _, err := statement.ExecContext(ctx,
			string(version), longitude[:], latitude[:],
			path.Country, path.CountryID, path.Region, path.RegionID,
			path.County, path.CountyID, path.LocalAdmin, path.LocalAdminID,
			path.Locality, path.LocalityID,
		); err != nil {
			return fmt.Errorf("admincache: store resolution batch: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("admincache: commit resolution batch: %w", err)
	}
	return nil
}

// WarmComplete reports whether every resolvable coordinate in an observation
// revision has been processed for a dataset version.
func (s *Store) WarmComplete(ctx context.Context, version admingeo.DatasetVersion, observationRevision string) (bool, error) {
	if err := validateWarmKey(ctx, version, observationRevision); err != nil {
		return false, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return false, errStoreClosed
	}

	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM warm_completions
		WHERE dataset_version = ? AND observation_revision = ?
	)`, string(version), observationRevision).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("admincache: lookup warm completion: %w", err)
	}
	return exists != 0, nil
}

// MarkWarmComplete records a fully successful warm scan.
func (s *Store) MarkWarmComplete(ctx context.Context, version admingeo.DatasetVersion, observationRevision string) error {
	if err := validateWarmKey(ctx, version, observationRevision); err != nil {
		return err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return errStoreClosed
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO warm_completions(
		dataset_version, observation_revision
	) VALUES(?, ?)`, string(version), observationRevision); err != nil {
		return fmt.Errorf("admincache: mark warm completion: %w", err)
	}
	return nil
}

// PruneVersions removes cached data for datasets that can no longer be active
// or rolled back to. An empty keep list clears the cache.
func (s *Store) PruneVersions(ctx context.Context, keep ...admingeo.DatasetVersion) error {
	if ctx == nil {
		return errors.New("admincache: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	versions := make([]string, 0, len(keep))
	seen := make(map[admingeo.DatasetVersion]struct{}, len(keep))
	for _, version := range keep {
		if version == "" {
			continue
		}
		if _, exists := seen[version]; exists {
			continue
		}
		seen[version] = struct{}{}
		versions = append(versions, string(version))
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return errStoreClosed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("admincache: begin version pruning: %w", err)
	}
	defer tx.Rollback()
	for _, table := range []string{"resolutions", "warm_completions"} {
		query := `DELETE FROM ` + table
		arguments := make([]any, len(versions))
		if len(versions) > 0 {
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(versions)), ",")
			query += ` WHERE dataset_version NOT IN (` + placeholders + `)`
			for index, version := range versions {
				arguments[index] = version
			}
		}
		if _, err := tx.ExecContext(ctx, query, arguments...); err != nil {
			return fmt.Errorf("admincache: prune %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("admincache: commit version pruning: %w", err)
	}
	return nil
}

// Close closes the cache. It is idempotent.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	s.closeErr = s.db.Close()
	return s.closeErr
}

func prepareParent(parent string) error {
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("admincache: create database directory: %w", err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("admincache: inspect database directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("admincache: database parent %q is not a directory", parent)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return fmt.Errorf("admincache: secure database directory: %w", err)
	}
	return nil
}

func sqliteDSN(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := u.Query()
	query.Set("mode", "rwc")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "synchronous(NORMAL)")
	query.Set("_txlock", "immediate")
	u.RawQuery = query.Encode()
	return u.String()
}

func initializeSchema(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("admincache: read schema version: %w", err)
	}
	if version == 0 {
		var tableCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_schema
			WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&tableCount); err != nil {
			return fmt.Errorf("admincache: inspect unversioned schema: %w", err)
		}
		if tableCount != 0 {
			return errors.New("admincache: unversioned database is not empty")
		}
		if err := createSchema(db); err != nil {
			return err
		}
		version = schemaVersion
	}
	if version != schemaVersion {
		return fmt.Errorf("admincache: unsupported schema version %d", version)
	}
	if err := validateSchema(db); err != nil {
		return fmt.Errorf("admincache: validate schema version %d: %w", version, err)
	}
	return nil
}

func createSchema(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("admincache: begin schema initialization: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(cacheSchema); err != nil {
		return fmt.Errorf("admincache: initialize schema: %w", err)
	}
	if _, err := tx.Exec(`PRAGMA user_version = 1`); err != nil {
		return fmt.Errorf("admincache: set schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("admincache: commit schema initialization: %w", err)
	}
	return nil
}

func validateSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_list`)
	if err != nil {
		return err
	}
	defer rows.Close()
	expected := map[string]bool{"resolutions": false, "warm_completions": false}
	for rows.Next() {
		var schema, name, tableType string
		var columns, withoutRowID, strict int
		if err := rows.Scan(&schema, &name, &tableType, &columns, &withoutRowID, &strict); err != nil {
			return err
		}
		if schema != "main" || tableType != "table" || strings.HasPrefix(name, "sqlite_") {
			continue
		}
		if _, ok := expected[name]; !ok {
			return fmt.Errorf("unexpected table %q", name)
		}
		if strict != 1 || withoutRowID != 1 {
			return fmt.Errorf("table %q is not STRICT WITHOUT ROWID", name)
		}
		expected[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for name, found := range expected {
		if !found {
			return fmt.Errorf("missing table %q", name)
		}
	}
	if err := validateColumns(db, "resolutions", resolutionColumns); err != nil {
		return err
	}
	return validateColumns(db, "warm_completions", warmCompletionColumns)
}

type schemaColumn struct {
	name       string
	typeName   string
	primaryKey int
}

func validateColumns(db *sql.DB, table string, expected []schemaColumn) error {
	rows, err := db.Query(`PRAGMA table_xinfo(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var id, notNull, primaryKey, hidden int
		var name, typeName string
		var defaultValue sql.NullString
		if err := rows.Scan(&id, &name, &typeName, &notNull, &defaultValue, &primaryKey, &hidden); err != nil {
			return err
		}
		if index >= len(expected) {
			return fmt.Errorf("table %q has unexpected column %q", table, name)
		}
		want := expected[index]
		if id != index || name != want.name || typeName != want.typeName || notNull != 1 || primaryKey != want.primaryKey || hidden != 0 {
			return fmt.Errorf("table %q column %d does not match schema", table, index)
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if index != len(expected) {
		return fmt.Errorf("table %q has %d columns, expected %d", table, index, len(expected))
	}
	return nil
}

func validateContextAndVersion(ctx context.Context, version admingeo.DatasetVersion) error {
	if ctx == nil {
		return errors.New("admincache: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if version == "" {
		return errors.New("admincache: empty dataset version")
	}
	return nil
}

func validateWarmKey(ctx context.Context, version admingeo.DatasetVersion, observationRevision string) error {
	if err := validateContextAndVersion(ctx, version); err != nil {
		return err
	}
	if observationRevision == "" {
		return errors.New("admincache: empty observation revision")
	}
	return nil
}

func coordinateKey(coordinate admingeo.Coordinate) ([8]byte, [8]byte) {
	var longitude, latitude [8]byte
	binary.BigEndian.PutUint64(longitude[:], math.Float64bits(coordinate.Longitude))
	binary.BigEndian.PutUint64(latitude[:], math.Float64bits(coordinate.Latitude))
	return longitude, latitude
}

var resolutionColumns = []schemaColumn{
	{name: "dataset_version", typeName: "TEXT", primaryKey: 1},
	{name: "longitude", typeName: "BLOB", primaryKey: 2},
	{name: "latitude", typeName: "BLOB", primaryKey: 3},
	{name: "country", typeName: "TEXT"},
	{name: "country_id", typeName: "TEXT"},
	{name: "region", typeName: "TEXT"},
	{name: "region_id", typeName: "TEXT"},
	{name: "county", typeName: "TEXT"},
	{name: "county_id", typeName: "TEXT"},
	{name: "local_admin", typeName: "TEXT"},
	{name: "local_admin_id", typeName: "TEXT"},
	{name: "locality", typeName: "TEXT"},
	{name: "locality_id", typeName: "TEXT"},
}

var warmCompletionColumns = []schemaColumn{
	{name: "dataset_version", typeName: "TEXT", primaryKey: 1},
	{name: "observation_revision", typeName: "TEXT", primaryKey: 2},
}

const cacheSchema = `
	CREATE TABLE resolutions (
		dataset_version TEXT NOT NULL CHECK(length(dataset_version) > 0),
		longitude BLOB NOT NULL CHECK(typeof(longitude) = 'blob' AND length(longitude) = 8),
		latitude BLOB NOT NULL CHECK(typeof(latitude) = 'blob' AND length(latitude) = 8),
		country TEXT NOT NULL,
		country_id TEXT NOT NULL,
		region TEXT NOT NULL,
		region_id TEXT NOT NULL,
		county TEXT NOT NULL,
		county_id TEXT NOT NULL,
		local_admin TEXT NOT NULL,
		local_admin_id TEXT NOT NULL,
		locality TEXT NOT NULL,
		locality_id TEXT NOT NULL,
		PRIMARY KEY(dataset_version, longitude, latitude)
	) STRICT, WITHOUT ROWID;
	CREATE TABLE warm_completions (
		dataset_version TEXT NOT NULL CHECK(length(dataset_version) > 0),
		observation_revision TEXT NOT NULL CHECK(length(observation_revision) > 0),
		PRIMARY KEY(dataset_version, observation_revision)
	) STRICT, WITHOUT ROWID;`

const putResolutionSQL = `INSERT INTO resolutions(
	dataset_version, longitude, latitude,
	country, country_id, region, region_id, county, county_id,
	local_admin, local_admin_id, locality, locality_id
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(dataset_version, longitude, latitude) DO UPDATE SET
	country = excluded.country,
	country_id = excluded.country_id,
	region = excluded.region,
	region_id = excluded.region_id,
	county = excluded.county,
	county_id = excluded.county_id,
	local_admin = excluded.local_admin,
	local_admin_id = excluded.local_admin_id,
	locality = excluded.locality,
	locality_id = excluded.locality_id`
