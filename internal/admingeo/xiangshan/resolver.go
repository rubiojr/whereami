// Package xiangshan resolves administrative paths from local Xiangshan XSCI
// v1 split index and polygon slab files.
package xiangshan

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"

	"github.com/rubiojr/whereami/internal/admingeo"
)

const (
	defaultMaxExpandedIndexBytes int64 = 64 << 20
	defaultMaxSlabChunkBytes     int64 = 64 << 20
	defaultCacheBytes            int64 = 4 << 20

	subtypeCountry     uint8 = 0
	subtypeDependency  uint8 = 1
	subtypeMacroRegion uint8 = 2
	subtypeRegion      uint8 = 3
	subtypeMacroCounty uint8 = 4
	subtypeCounty      uint8 = 5
	subtypeLocalAdmin  uint8 = 6
	subtypeLocality    uint8 = 7
)

// Option configures a Resolver.
type Option func(*config) error

type config struct {
	maxExpandedIndexBytes int64
	maxSlabChunkBytes     int64
	cacheBytes            int64
}

// WithCacheBytes sets the byte capacity of the concurrent slab LRU. Zero
// disables caching.
func WithCacheBytes(size int64) Option {
	return func(cfg *config) error {
		if size < 0 {
			return fmt.Errorf("xiangshan: cache size cannot be negative")
		}
		cfg.cacheBytes = size
		return nil
	}
}

// WithMaxExpandedIndexBytes sets the hard gzip expansion limit.
func WithMaxExpandedIndexBytes(size int64) Option {
	return func(cfg *config) error {
		if size < indexHeaderSize || size == math.MaxInt64 {
			return fmt.Errorf("xiangshan: expanded index limit must be at least %d bytes", indexHeaderSize)
		}
		cfg.maxExpandedIndexBytes = size
		return nil
	}
}

// WithMaxSlabChunkBytes limits any one allocation made for a slab record.
func WithMaxSlabChunkBytes(size int64) Option {
	return func(cfg *config) error {
		if size < 8 || size > math.MaxUint32 {
			return fmt.Errorf("xiangshan: slab chunk limit must be between 8 and %d bytes", uint64(math.MaxUint32))
		}
		cfg.maxSlabChunkBytes = size
		return nil
	}
}

// Resolver is a concurrency-safe local Xiangshan split-file resolver.
type Resolver struct {
	mu      sync.RWMutex
	slab    *os.File
	closed  bool
	version admingeo.DatasetVersion
	records []divisionRecord
	coarse  map[[2]int16][]uint32
	fine    map[[2]int16][]uint32
	cache   *slabCache
}

var _ admingeo.Resolver = (*Resolver)(nil)

// New opens a gzip-compressed XSCI v1 index and its local polygon slab.
func New(indexGzipPath, slabPath string, version admingeo.DatasetVersion, options ...Option) (*Resolver, error) {
	cfg := config{
		maxExpandedIndexBytes: defaultMaxExpandedIndexBytes,
		maxSlabChunkBytes:     defaultMaxSlabChunkBytes,
		cacheBytes:            defaultCacheBytes,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("xiangshan: nil option")
		}
		if err := option(&cfg); err != nil {
			return nil, err
		}
	}

	slab, err := os.Open(slabPath)
	if err != nil {
		return nil, fmt.Errorf("xiangshan: open polygon slab: %w", err)
	}
	keepSlab := false
	defer func() {
		if !keepSlab {
			_ = slab.Close()
		}
	}()
	slabInfo, err := slab.Stat()
	if err != nil {
		return nil, fmt.Errorf("xiangshan: stat polygon slab: %w", err)
	}
	if !slabInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("xiangshan: polygon slab is not a regular file")
	}

	indexData, err := readGzipIndex(indexGzipPath, cfg.maxExpandedIndexBytes)
	if err != nil {
		return nil, err
	}
	index, err := parseIndex(indexData, slabInfo.Size(), uint32(cfg.maxSlabChunkBytes))
	if err != nil {
		return nil, err
	}
	keepSlab = true
	return &Resolver{
		slab:    slab,
		version: version,
		records: index.records,
		coarse:  index.coarse,
		fine:    index.fine,
		cache:   newSlabCache(cfg.cacheBytes),
	}, nil
}

func readGzipIndex(path string, maxExpandedBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("xiangshan: open gzip index: %w", err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("xiangshan: open gzip index: %w", err)
	}
	limited := io.LimitReader(reader, maxExpandedBytes+1)
	var buffer bytes.Buffer
	buffer.Grow(int(min64(maxExpandedBytes, 1<<20)))
	if _, err := io.Copy(&buffer, limited); err != nil {
		_ = reader.Close()
		return nil, fmt.Errorf("xiangshan: expand gzip index: %w", err)
	}
	if int64(buffer.Len()) > maxExpandedBytes {
		_ = reader.Close()
		return nil, fmt.Errorf("xiangshan: expanded index exceeds %d bytes", maxExpandedBytes)
	}
	if err := reader.Close(); err != nil {
		return nil, fmt.Errorf("xiangshan: close gzip index: %w", err)
	}
	return buffer.Bytes(), nil
}

// Resolve returns the administrative hierarchy containing coordinate. A valid
// ocean coordinate returns an empty AdminPath and no error.
func (r *Resolver) Resolve(ctx context.Context, coordinate admingeo.Coordinate) (admingeo.AdminPath, error) {
	if ctx == nil {
		return admingeo.AdminPath{}, fmt.Errorf("xiangshan: nil context")
	}
	if err := validateCoordinate(coordinate); err != nil {
		return admingeo.AdminPath{}, err
	}
	if err := ctx.Err(); err != nil {
		return admingeo.AdminPath{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return admingeo.AdminPath{}, errors.New("xiangshan: resolver is closed")
	}

	var path admingeo.AdminPath
	coarseKey := [2]int16{int16(math.Floor(coordinate.Longitude)), int16(math.Floor(coordinate.Latitude))}
	if err := r.applyCandidates(ctx, &path, r.coarse[coarseKey], coordinate, true); err != nil {
		return admingeo.AdminPath{}, err
	}
	fineKey := [2]int16{int16(math.Floor(coordinate.Longitude * 4)), int16(math.Floor(coordinate.Latitude * 4))}
	if err := r.applyCandidates(ctx, &path, r.fine[fineKey], coordinate, false); err != nil {
		return admingeo.AdminPath{}, err
	}
	return path, nil
}

func (r *Resolver) applyCandidates(ctx context.Context, path *admingeo.AdminPath, candidates []uint32, coordinate admingeo.Coordinate, coarse bool) error {
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		record := r.records[candidate]
		if !bboxContains(record.bbox, coordinate.Longitude, coordinate.Latitude) {
			continue
		}
		data, err := r.readSlab(ctx, candidate, record)
		if err != nil {
			return err
		}
		division, err := decodeAndTest(ctx, data, record, coordinate.Longitude, coordinate.Latitude)
		if err != nil {
			return fmt.Errorf("xiangshan: division %d: %w", candidate, err)
		}
		if !division.contains {
			continue
		}
		if coarse {
			applyCoarse(path, division)
		} else {
			applyFine(path, division)
		}
	}
	return nil
}

func (r *Resolver) readSlab(ctx context.Context, candidate uint32, record divisionRecord) ([]byte, error) {
	if r.cache != nil {
		if data, ok := r.cache.get(candidate); ok {
			return data, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data := make([]byte, int(record.length))
	n, err := r.slab.ReadAt(data, int64(record.offset))
	if n != len(data) {
		return nil, fmt.Errorf("xiangshan: read slab range [%d,%d): %w", record.offset, record.offset+uint64(record.length), io.ErrUnexpectedEOF)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("xiangshan: read slab range [%d,%d): %w", record.offset, record.offset+uint64(record.length), err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.cache != nil {
		r.cache.put(candidate, data)
	}
	return data, nil
}

func applyCoarse(path *admingeo.AdminPath, division decodedDivision) {
	switch division.subtype {
	case subtypeCountry, subtypeDependency:
		if path.Country == "" {
			path.Country, path.CountryID = division.name, division.id
		}
	case subtypeMacroRegion, subtypeRegion:
		if path.Region == "" {
			path.Region, path.RegionID = division.name, division.id
		}
	case subtypeMacroCounty:
		if path.County == "" {
			path.County, path.CountyID = division.name, division.id
		}
	}
}

func applyFine(path *admingeo.AdminPath, division decodedDivision) {
	switch division.subtype {
	case subtypeCounty:
		if path.County == "" {
			path.County, path.CountyID = division.name, division.id
		}
	case subtypeLocalAdmin:
		if path.LocalAdmin == "" {
			path.LocalAdmin, path.LocalAdminID = division.name, division.id
		}
	case subtypeLocality:
		if path.Locality == "" {
			path.Locality, path.LocalityID = division.name, division.id
		}
	}
}

func validateCoordinate(coordinate admingeo.Coordinate) error {
	if !finite(coordinate.Longitude) || !finite(coordinate.Latitude) {
		return fmt.Errorf("xiangshan: coordinate must be finite")
	}
	if coordinate.Longitude < -180 || coordinate.Longitude > 180 {
		return fmt.Errorf("xiangshan: longitude %g is outside [-180,180]", coordinate.Longitude)
	}
	if coordinate.Latitude < -90 || coordinate.Latitude > 90 {
		return fmt.Errorf("xiangshan: latitude %g is outside [-90,90]", coordinate.Latitude)
	}
	return nil
}

// Version returns the caller-supplied immutable dataset version.
func (r *Resolver) Version() admingeo.DatasetVersion {
	return r.version
}

// Close waits for active queries and closes the local slab. It is idempotent.
func (r *Resolver) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return r.slab.Close()
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
