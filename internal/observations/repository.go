// Package observations maintains a rebuildable SQLite index of GPX waypoints.
package observations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const revisionKey = "revision"

// TimeStatus describes whether a waypoint's original GPX time was usable.
type TimeStatus string

const (
	TimeValid   TimeStatus = "valid"
	TimeMissing TimeStatus = "missing"
	TimeInvalid TimeStatus = "invalid"
)

// Observation is one GPX waypoint. Source and Ordinal form its stable identity
// within one rebuild; Ordinal is zero-based and follows document order.
type Observation struct {
	Source           string
	Ordinal          int
	Name             string
	Description      string
	Elevation        string
	Symbol           string
	Type             string
	RawLatitude      string
	RawLongitude     string
	Latitude         float64
	Longitude        float64
	CoordinatesValid bool
	RawTime          string
	TimeStatus       TimeStatus
	Time             time.Time
}

// TimeStatusCounts reports indexed waypoints by timestamp status.
type TimeStatusCounts struct {
	Valid   int64
	Missing int64
	Invalid int64
}

// Repository owns the writable index connection. Source GPX files remain the
// authority; Rebuild reconciles the index with their current contents.
type Repository struct {
	mu     sync.Mutex
	db     *sql.DB
	dbPath string
	closed bool
}

// Open opens or creates an observation index at dbPath.
func Open(dbPath string) (*Repository, error) {
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := prepareDBParent(filepath.Dir(absPath)); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", sqliteDSN(absPath, "rwc"))
	if err != nil {
		return nil, fmt.Errorf("open observation database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to observation database: %w", err)
	}
	if err := os.Chmod(absPath, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("secure observation database: %w", err)
	}
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Repository{db: db, dbPath: absPath}, nil
}

// NewRepository is an explicit-name alias for Open.
func NewRepository(dbPath string) (*Repository, error) {
	return Open(dbPath)
}

// Close closes the writable repository connection. Existing snapshots remain
// usable until they are closed.
func (r *Repository) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return r.db.Close()
}

// Rebuild recursively reconciles all .gpx files under dataRoot, excluding the
// explicit bookmarksPath. Stored source names are slash-separated paths
// relative to dataRoot. A relative bookmarksPath is interpreted from dataRoot.
func (r *Repository) Rebuild(dataRoot, bookmarksPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("observation repository is closed")
	}

	sources, err := discoverSources(dataRoot, bookmarksPath)
	if err != nil {
		return err
	}

	tx, err := r.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin observation rebuild: %w", err)
	}
	defer tx.Rollback()

	existing, err := sourceHashes(tx)
	if err != nil {
		return err
	}
	for _, source := range sources {
		if existingHash, ok := existing[source.path]; ok && existingHash == source.hash {
			delete(existing, source.path)
			continue
		}
		content, err := os.ReadFile(source.absolutePath)
		if err != nil {
			return fmt.Errorf("read observation source %q: %w", source.path, err)
		}
		if sha256.Sum256(content) != source.hash {
			return fmt.Errorf("observation source %q changed during rebuild", source.path)
		}
		observations, err := parseSource(source.path, content)
		if err != nil {
			return err
		}
		if err := replaceSource(tx, source, observations); err != nil {
			return err
		}
		delete(existing, source.path)
	}
	for path := range existing {
		if _, err := tx.Exec(`DELETE FROM sources WHERE path = ?`, path); err != nil {
			return fmt.Errorf("remove deleted observation source %q: %w", path, err)
		}
	}

	revision := calculateRevision(sources)
	if _, err := tx.Exec(`INSERT INTO metadata(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, revisionKey, revision); err != nil {
		return fmt.Errorf("store observation revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit observation rebuild: %w", err)
	}
	return nil
}

// AddSources indexes newly added GPX files without rescanning the data root.
// Each path must resolve to a regular non-bookmark file beneath dataRoot.
func (r *Repository) AddSources(dataRoot, bookmarksPath string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	root, err := filepath.EvalSymlinks(dataRoot)
	if err != nil {
		return fmt.Errorf("resolve observation data root symlinks: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve observation data root: %w", err)
	}
	excluded := ""
	if bookmarksPath != "" {
		excluded = bookmarksPath
		if !filepath.IsAbs(excluded) {
			excluded = filepath.Join(root, excluded)
		}
		if resolved, resolveErr := filepath.EvalSymlinks(excluded); resolveErr == nil {
			excluded = resolved
		}
		excluded, err = filepath.Abs(excluded)
		if err != nil {
			return fmt.Errorf("resolve bookmarks path: %w", err)
		}
	}

	prepared := make([]struct {
		source       sourceFile
		observations []Observation
	}, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		absolutePath := path
		if !filepath.IsAbs(absolutePath) {
			absolutePath = filepath.Join(root, absolutePath)
		}
		absolutePath, err = filepath.Abs(absolutePath)
		if err != nil {
			return fmt.Errorf("resolve observation source: %w", err)
		}
		relativePath, err := filepath.Rel(root, absolutePath)
		if err != nil || relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
			return errors.New("observation source is outside the data root")
		}
		if excluded != "" && filepath.Clean(absolutePath) == filepath.Clean(excluded) {
			return errors.New("bookmarks cannot be added as an observation source")
		}
		relativePath = filepath.ToSlash(relativePath)
		if _, duplicate := seen[relativePath]; duplicate {
			continue
		}
		seen[relativePath] = struct{}{}
		info, err := os.Lstat(absolutePath)
		if err != nil {
			return fmt.Errorf("inspect observation source %q: %w", relativePath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("observation source %q is not a regular file", relativePath)
		}
		content, err := os.ReadFile(absolutePath)
		if err != nil {
			return fmt.Errorf("read observation source %q: %w", relativePath, err)
		}
		source := sourceFile{
			path:         relativePath,
			absolutePath: absolutePath,
			hash:         sha256.Sum256(content),
		}
		observations, err := parseSource(relativePath, content)
		if err != nil {
			return err
		}
		prepared = append(prepared, struct {
			source       sourceFile
			observations []Observation
		}{source: source, observations: observations})
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("observation repository is closed")
	}
	tx, err := r.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin observation source update: %w", err)
	}
	defer tx.Rollback()
	for _, item := range prepared {
		if err := replaceSource(tx, item.source, item.observations); err != nil {
			return err
		}
	}
	hashes, err := sourceHashes(tx)
	if err != nil {
		return err
	}
	sources := make([]sourceFile, 0, len(hashes))
	for path, hash := range hashes {
		sources = append(sources, sourceFile{path: path, hash: hash})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].path < sources[j].path })
	revision := calculateRevision(sources)
	if _, err := tx.Exec(`INSERT INTO metadata(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, revisionKey, revision); err != nil {
		return fmt.Errorf("store observation revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit observation source update: %w", err)
	}
	return nil
}

// Snapshot opens an isolated, read-only view of the current repository state.
func (r *Repository) Snapshot() (*Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("observation repository is closed")
	}

	db, err := sql.Open("sqlite", sqliteDSN(r.dbPath, "ro"))
	if err != nil {
		return nil, fmt.Errorf("open observation snapshot: %w", err)
	}
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin observation snapshot: %w", err)
	}
	var revision string
	if err := tx.QueryRow(`SELECT value FROM metadata WHERE key = ?`, revisionKey).Scan(&revision); err != nil {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("read observation revision: %w", err)
	}
	return &Snapshot{db: db, tx: tx, revision: revision}, nil
}

// Snapshot is a consistent read transaction. It must be closed after use.
type Snapshot struct {
	mu       sync.Mutex
	db       *sql.DB
	tx       *sql.Tx
	revision string
	closed   bool
}

// Revision returns the deterministic SHA-256 revision captured by the snapshot.
func (s *Snapshot) Revision() string {
	return s.revision
}

// ScanPeriod visits valid-time observations in UTC timestamp, source, ordinal
// order. The period is strict [start,end); invalid and missing times are absent.
func (s *Snapshot) ScanPeriod(start, end time.Time, visit func(Observation) error) error {
	if visit == nil {
		return errors.New("observation scan callback is nil")
	}
	if end.Before(start) {
		return errors.New("observation scan end precedes start")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("observation snapshot is closed")
	}

	start = start.UTC()
	end = end.UTC()
	rows, err := s.tx.Query(scanPeriodSQL,
		start.Unix(), start.Unix(), start.Nanosecond(),
		end.Unix(), end.Unix(), end.Nanosecond())
	if err != nil {
		return fmt.Errorf("query observation period: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		observation, err := scanObservation(rows)
		if err != nil {
			return err
		}
		if err := visit(observation); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan observation period: %w", err)
	}
	return nil
}

// ScanResolvableCoordinates visits each distinct coordinate which can
// participate in a report, ordered by longitude and then latitude.
func (s *Snapshot) ScanResolvableCoordinates(ctx context.Context, visit func(longitude, latitude float64) error) error {
	if ctx == nil {
		return errors.New("observation coordinate scan context is nil")
	}
	if visit == nil {
		return errors.New("observation coordinate scan callback is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("observation snapshot is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	rows, err := s.tx.QueryContext(ctx, scanResolvableCoordinatesSQL)
	if err != nil {
		return fmt.Errorf("query resolvable observation coordinates: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var longitude, latitude float64
		if err := rows.Scan(&longitude, &latitude); err != nil {
			return fmt.Errorf("scan resolvable observation coordinate: %w", err)
		}
		if err := visit(longitude, latitude); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan resolvable observation coordinates: %w", err)
	}
	return nil
}

// TimeStatusCounts counts every indexed observation, including observations
// which cannot participate in period scans.
func (s *Snapshot) TimeStatusCounts() (TimeStatusCounts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return TimeStatusCounts{}, errors.New("observation snapshot is closed")
	}

	var counts TimeStatusCounts
	err := s.tx.QueryRow(`SELECT
		COUNT(*) FILTER (WHERE time_status = 'valid'),
		COUNT(*) FILTER (WHERE time_status = 'missing'),
		COUNT(*) FILTER (WHERE time_status = 'invalid')
		FROM observations`).Scan(&counts.Valid, &counts.Missing, &counts.Invalid)
	if err != nil {
		return TimeStatusCounts{}, fmt.Errorf("query observation time statuses: %w", err)
	}
	return counts, nil
}

// Close ends the snapshot read transaction and closes its connection.
func (s *Snapshot) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	txErr := s.tx.Rollback()
	dbErr := s.db.Close()
	if txErr != nil && !errors.Is(txErr, sql.ErrTxDone) {
		return txErr
	}
	return dbErr
}

type sourceFile struct {
	path         string
	absolutePath string
	hash         [sha256.Size]byte
}

type gpxDocument struct {
	Waypoints []gpxWaypoint `xml:"wpt"`
}

type gpxWaypoint struct {
	Latitude  string `xml:"lat,attr"`
	Longitude string `xml:"lon,attr"`
	Elevation string `xml:"ele"`
	Time      string `xml:"time"`
	Name      string `xml:"name"`
	Desc      string `xml:"desc"`
	Symbol    string `xml:"sym"`
	Type      string `xml:"type"`
}

func prepareDBParent(parent string) error {
	info, err := os.Stat(parent)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return fmt.Errorf("create observation database directory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect observation database directory: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("observation database parent %q is not a directory", parent)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return fmt.Errorf("secure observation database directory: %w", err)
	}
	return nil
}

func sqliteDSN(path, mode string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := u.Query()
	query.Set("mode", mode)
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	u.RawQuery = query.Encode()
	return u.String()
}

func createSchema(db *sql.DB) error {
	const schema = `
		PRAGMA journal_mode = WAL;
		CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS sources (
			path TEXT PRIMARY KEY,
			sha256 BLOB NOT NULL CHECK(length(sha256) = 32)
		);
		CREATE TABLE IF NOT EXISTS observations (
			source_path TEXT NOT NULL REFERENCES sources(path) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL CHECK(ordinal >= 0),
			name TEXT NOT NULL,
			description TEXT NOT NULL,
			elevation_raw TEXT NOT NULL,
			symbol TEXT NOT NULL,
			type TEXT NOT NULL,
			latitude_raw TEXT NOT NULL,
			longitude_raw TEXT NOT NULL,
			latitude REAL,
			longitude REAL,
			coordinates_valid INTEGER NOT NULL CHECK(coordinates_valid IN (0, 1)),
			time_raw TEXT NOT NULL,
			time_status TEXT NOT NULL CHECK(time_status IN ('valid', 'missing', 'invalid')),
			time_utc TEXT,
			time_unix_seconds INTEGER,
			time_nanosecond INTEGER,
			PRIMARY KEY(source_path, ordinal)
		);
		CREATE INDEX IF NOT EXISTS observations_period
			ON observations(time_unix_seconds, time_nanosecond, source_path, ordinal)
			WHERE time_status = 'valid';
		CREATE INDEX IF NOT EXISTS observations_resolvable_coordinates
			ON observations(longitude, latitude)
			WHERE coordinates_valid = 1 AND time_status = 'valid';`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("initialize observation database: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO metadata(key, value) VALUES(?, ?)`, revisionKey, calculateRevision(nil)); err != nil {
		return fmt.Errorf("initialize observation revision: %w", err)
	}
	return nil
}

func discoverSources(dataRoot, bookmarksPath string) ([]sourceFile, error) {
	root, err := filepath.EvalSymlinks(dataRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve observation data root symlinks: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve observation data root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect observation data root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("observation data root %q is not a directory", dataRoot)
	}

	excluded := ""
	if bookmarksPath != "" {
		if filepath.IsAbs(bookmarksPath) {
			excluded = filepath.Clean(bookmarksPath)
		} else {
			excluded = filepath.Join(root, bookmarksPath)
		}
		if resolved, resolveErr := filepath.EvalSymlinks(excluded); resolveErr == nil {
			excluded = resolved
		}
		excluded, err = filepath.Abs(excluded)
		if err != nil {
			return nil, fmt.Errorf("resolve bookmarks path: %w", err)
		}
	}

	var sources []sourceFile
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".staging-") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".gpx") {
			return nil
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if excluded != "" && filepath.Clean(absPath) == excluded {
			return nil
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !entryInfo.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(absPath)
		if err != nil {
			return err
		}
		hasher := sha256.New()
		_, copyErr := io.Copy(hasher, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		relative, err := filepath.Rel(root, absPath)
		if err != nil {
			return err
		}
		sources = append(sources, sourceFile{
			path:         filepath.ToSlash(relative),
			absolutePath: absPath,
		})
		copy(sources[len(sources)-1].hash[:], hasher.Sum(nil))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover observation sources: %w", err)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].path < sources[j].path })
	return sources, nil
}

func sourceHashes(tx *sql.Tx) (map[string][sha256.Size]byte, error) {
	rows, err := tx.Query(`SELECT path, sha256 FROM sources`)
	if err != nil {
		return nil, fmt.Errorf("query observation sources: %w", err)
	}
	defer rows.Close()

	hashes := make(map[string][sha256.Size]byte)
	for rows.Next() {
		var path string
		var rawHash []byte
		if err := rows.Scan(&path, &rawHash); err != nil {
			return nil, fmt.Errorf("scan observation source: %w", err)
		}
		if len(rawHash) != sha256.Size {
			return nil, fmt.Errorf("observation source %q has invalid stored hash", path)
		}
		var hash [sha256.Size]byte
		copy(hash[:], rawHash)
		hashes[path] = hash
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan observation sources: %w", err)
	}
	return hashes, nil
}

func parseSource(path string, content []byte) ([]Observation, error) {
	var document gpxDocument
	decoder := xml.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parse observation source %q: %w", path, err)
	}

	observations := make([]Observation, 0, len(document.Waypoints))
	for ordinal, waypoint := range document.Waypoints {
		latitude, latitudeOK := parseCoordinate(waypoint.Latitude, -90, 90)
		longitude, longitudeOK := parseCoordinate(waypoint.Longitude, -180, 180)
		parsedTime, status := parseTime(waypoint.Time)
		observations = append(observations, Observation{
			Source:           path,
			Ordinal:          ordinal,
			Name:             waypoint.Name,
			Description:      waypoint.Desc,
			Elevation:        waypoint.Elevation,
			Symbol:           waypoint.Symbol,
			Type:             waypoint.Type,
			RawLatitude:      waypoint.Latitude,
			RawLongitude:     waypoint.Longitude,
			Latitude:         latitude,
			Longitude:        longitude,
			CoordinatesValid: latitudeOK && longitudeOK,
			RawTime:          waypoint.Time,
			TimeStatus:       status,
			Time:             parsedTime,
		})
	}
	return observations, nil
}

func parseCoordinate(raw string, minimum, maximum float64) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, value >= minimum && value <= maximum
}

func parseTime(raw string) (time.Time, TimeStatus) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, TimeMissing
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return time.Time{}, TimeInvalid
	}
	return parsed.UTC(), TimeValid
}

func replaceSource(tx *sql.Tx, source sourceFile, observations []Observation) error {
	if _, err := tx.Exec(`DELETE FROM sources WHERE path = ?`, source.path); err != nil {
		return fmt.Errorf("replace observation source %q: %w", source.path, err)
	}
	if _, err := tx.Exec(`INSERT INTO sources(path, sha256) VALUES(?, ?)`, source.path, source.hash[:]); err != nil {
		return fmt.Errorf("store observation source %q: %w", source.path, err)
	}
	for _, observation := range observations {
		if err := insertObservation(tx, observation); err != nil {
			return err
		}
	}
	return nil
}

func insertObservation(tx *sql.Tx, observation Observation) error {
	var latitude any
	var longitude any
	if value, err := strconv.ParseFloat(strings.TrimSpace(observation.RawLatitude), 64); err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
		latitude = value
	}
	if value, err := strconv.ParseFloat(strings.TrimSpace(observation.RawLongitude), 64); err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
		longitude = value
	}

	var timeUTC any
	var unixSeconds any
	var nanosecond any
	if observation.TimeStatus == TimeValid {
		timeUTC = observation.Time.Format(time.RFC3339Nano)
		unixSeconds = observation.Time.Unix()
		nanosecond = observation.Time.Nanosecond()
	}
	_, err := tx.Exec(`INSERT INTO observations(
		source_path, ordinal, name, description, elevation_raw, symbol, type,
		latitude_raw, longitude_raw, latitude, longitude, coordinates_valid,
		time_raw, time_status, time_utc, time_unix_seconds, time_nanosecond
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		observation.Source, observation.Ordinal, observation.Name, observation.Description,
		observation.Elevation, observation.Symbol, observation.Type,
		observation.RawLatitude, observation.RawLongitude, latitude, longitude,
		observation.CoordinatesValid, observation.RawTime, observation.TimeStatus,
		timeUTC, unixSeconds, nanosecond)
	if err != nil {
		return fmt.Errorf("store observation %q ordinal %d: %w", observation.Source, observation.Ordinal, err)
	}
	return nil
}

func calculateRevision(sources []sourceFile) string {
	hash := sha256.New()
	hash.Write([]byte("whereami-observations-v1\x00"))
	var length [8]byte
	for _, source := range sources {
		binary.BigEndian.PutUint64(length[:], uint64(len(source.path)))
		hash.Write(length[:])
		hash.Write([]byte(source.path))
		hash.Write(source.hash[:])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

const scanPeriodSQL = `
	SELECT source_path, ordinal, name, description, elevation_raw, symbol, type,
		latitude_raw, longitude_raw, latitude, longitude, coordinates_valid,
		time_raw, time_status, time_utc
	FROM observations
	WHERE time_status = 'valid'
		AND (time_unix_seconds > ? OR (time_unix_seconds = ? AND time_nanosecond >= ?))
		AND (time_unix_seconds < ? OR (time_unix_seconds = ? AND time_nanosecond < ?))
	ORDER BY time_unix_seconds, time_nanosecond, source_path, ordinal`

const scanResolvableCoordinatesSQL = `
	SELECT DISTINCT longitude, latitude
	FROM observations
	WHERE coordinates_valid = 1 AND time_status = 'valid'
	ORDER BY longitude, latitude`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanObservation(row rowScanner) (Observation, error) {
	var observation Observation
	var latitude sql.NullFloat64
	var longitude sql.NullFloat64
	var timeUTC string
	if err := row.Scan(
		&observation.Source, &observation.Ordinal, &observation.Name,
		&observation.Description, &observation.Elevation, &observation.Symbol, &observation.Type,
		&observation.RawLatitude, &observation.RawLongitude, &latitude, &longitude,
		&observation.CoordinatesValid, &observation.RawTime, &observation.TimeStatus, &timeUTC,
	); err != nil {
		return Observation{}, fmt.Errorf("scan observation: %w", err)
	}
	if latitude.Valid {
		observation.Latitude = latitude.Float64
	}
	if longitude.Valid {
		observation.Longitude = longitude.Float64
	}
	parsed, err := time.Parse(time.RFC3339Nano, timeUTC)
	if err != nil {
		return Observation{}, fmt.Errorf("parse indexed UTC observation time: %w", err)
	}
	observation.Time = parsed.UTC()
	return observation, nil
}
