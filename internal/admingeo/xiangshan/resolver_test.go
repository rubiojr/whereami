package xiangshan

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	xs "github.com/ringsaturn/xiangshan/generated/xiangshan/v1"
	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPoint struct {
	lng float64
	lat float64
}

type testPolygon struct {
	exterior []testPoint
	holes    [][]testPoint
}

type testDivision struct {
	name     string
	id       string
	subtype  uint8
	bbox     [4]float32
	polygons []testPolygon
}

type testIndex struct {
	records  []divisionRecord
	coarse   map[[2]int16][]uint32
	fine     map[[2]int16][]uint32
	preindex map[[2]int16]uint32
}

func TestResolveHierarchyAndCoordinateOrder(t *testing.T) {
	outer := rectangle(29, 9, 31, 11)
	divisions := []testDivision{
		{name: "Country", id: "country-id", subtype: subtypeCountry, bbox: [4]float32{29, 31, 9, 11}, polygons: []testPolygon{{exterior: outer}}},
		{name: "Region", id: "region-id", subtype: subtypeRegion, bbox: [4]float32{29, 31, 9, 11}, polygons: []testPolygon{{exterior: outer}}},
		{name: "County", id: "county-id", subtype: subtypeCounty, bbox: [4]float32{29, 31, 9, 11}, polygons: []testPolygon{{exterior: outer}}},
		{name: "Local admin", id: "local-admin-id", subtype: subtypeLocalAdmin, bbox: [4]float32{29, 31, 9, 11}, polygons: []testPolygon{{exterior: outer}}},
		{name: "Locality", id: "locality-id", subtype: subtypeLocality, bbox: [4]float32{29, 31, 9, 11}, polygons: []testPolygon{{exterior: outer}}},
	}
	resolver := openFixture(t, divisions, map[[2]int16][]uint32{{30, 10}: {0, 1}}, map[[2]int16][]uint32{{120, 40}: {2, 3, 4}}, nil)

	path, err := resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 30, Latitude: 10})
	require.NoError(t, err)
	assert.Equal(t, admingeo.AdminPath{
		Country: "Country", CountryID: "country-id",
		Region: "Region", RegionID: "region-id",
		County: "County", CountyID: "county-id",
		LocalAdmin: "Local admin", LocalAdminID: "local-admin-id",
		Locality: "Locality", LocalityID: "locality-id",
	}, path)
	assert.Equal(t, admingeo.DatasetVersion("fixture-v1"), resolver.Version())

	reversed, err := resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 10, Latitude: 30})
	require.NoError(t, err)
	assert.Empty(t, reversed)
}

func TestResolveRequiresContainmentForSingleCandidateAndPreindex(t *testing.T) {
	t.Run("hole", func(t *testing.T) {
		division := testDivision{
			name: "Donut", id: "donut", subtype: subtypeCountry, bbox: [4]float32{0, 10, 0, 10},
			polygons: []testPolygon{{exterior: rectangle(0, 0, 10, 10), holes: [][]testPoint{rectangle(4, 4, 6, 6)}}},
		}
		resolver := openFixture(t, []testDivision{division}, map[[2]int16][]uint32{{5, 5}: {0}}, nil, map[[2]int16]uint32{{5, 5}: 0})
		path, err := resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 5, Latitude: 5})
		require.NoError(t, err)
		assert.Empty(t, path, "the country preindex and single candidate must not bypass the hole")
	})

	t.Run("inside bbox but outside polygon", func(t *testing.T) {
		division := testDivision{
			name: "Triangle", id: "triangle", subtype: subtypeCountry, bbox: [4]float32{0, 10, 0, 10},
			polygons: []testPolygon{{exterior: []testPoint{{0, 0}, {10, 0}, {0, 10}}}},
		}
		resolver := openFixture(t, []testDivision{division}, map[[2]int16][]uint32{{9, 9}: {0}}, nil, nil)
		path, err := resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 9, Latitude: 9})
		require.NoError(t, err)
		assert.Empty(t, path)
	})
}

func TestResolveOceanAndBoundaries(t *testing.T) {
	division := testDivision{
		name: "Donut", id: "donut", subtype: subtypeCountry, bbox: [4]float32{0, 10, 0, 10},
		polygons: []testPolygon{{exterior: rectangle(0, 0, 10, 10), holes: [][]testPoint{rectangle(4, 4, 6, 6)}}},
	}
	coarse := map[[2]int16][]uint32{{0, 5}: {0}, {4, 5}: {0}}
	resolver := openFixture(t, []testDivision{division}, coarse, nil, nil)

	ocean, err := resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: -30, Latitude: 0})
	require.NoError(t, err)
	assert.Empty(t, ocean)

	exteriorBoundary, err := resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 0, Latitude: 5})
	require.NoError(t, err)
	assert.Equal(t, "Donut", exteriorBoundary.Country, "exterior boundaries are included")

	holeBoundary, err := resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 4, Latitude: 5})
	require.NoError(t, err)
	assert.Empty(t, holeBoundary, "hole boundaries are excluded")
}

func TestResolveRejectsInvalidCoordinates(t *testing.T) {
	resolver := openFixture(t, nil, nil, nil, nil)
	tests := []admingeo.Coordinate{
		{Longitude: math.NaN()},
		{Latitude: math.Inf(1)},
		{Longitude: -180.0001},
		{Longitude: 180.0001},
		{Latitude: -90.0001},
		{Latitude: 90.0001},
	}
	for _, coordinate := range tests {
		_, err := resolver.Resolve(context.Background(), coordinate)
		assert.Error(t, err, "%+v", coordinate)
	}
}

func TestNewRejectsGzipAndIndexBombs(t *testing.T) {
	t.Run("expanded gzip", func(t *testing.T) {
		directory := t.TempDir()
		indexPath := filepath.Join(directory, "index.gz")
		slabPath := filepath.Join(directory, "slab")
		require.NoError(t, writeGzip(indexPath, bytes.Repeat([]byte{0}, 4096)))
		require.NoError(t, os.WriteFile(slabPath, nil, 0o600))
		_, err := New(indexPath, slabPath, "v1", WithMaxExpandedIndexBytes(64))
		assert.ErrorContains(t, err, "expanded index exceeds")
	})

	t.Run("header count", func(t *testing.T) {
		raw := make([]byte, indexHeaderSize)
		copy(raw, "XSCI")
		raw[4] = 1
		binary.LittleEndian.PutUint32(raw[8:12], math.MaxUint32)
		_, err := openRawFixture(t, raw, nil)
		assert.ErrorContains(t, err, "header counts exceed file size")
	})

	t.Run("cell candidate allocation", func(t *testing.T) {
		raw := encodeTestIndex(testIndex{coarse: map[[2]int16][]uint32{{0, 0}: nil}})
		binary.LittleEndian.PutUint16(raw[indexHeaderSize+4:indexHeaderSize+6], math.MaxUint16)
		_, err := openRawFixture(t, raw, nil)
		assert.ErrorContains(t, err, "candidates exceed remaining index")
	})
}

func TestNewRejectsInvalidRecordsAndCandidates(t *testing.T) {
	validRecord := divisionRecord{subtype: subtypeCountry, bbox: [4]float32{0, 1, 0, 1}, length: 8}
	tests := []struct {
		name      string
		index     testIndex
		slabBytes int
		contains  string
	}{
		{name: "subtype", index: testIndex{records: []divisionRecord{{subtype: 8, bbox: validRecord.bbox, length: 8}}}, slabBytes: 8, contains: "invalid subtype"},
		{name: "bbox nan", index: testIndex{records: []divisionRecord{{subtype: 0, bbox: [4]float32{0, float32(math.NaN()), 0, 1}, length: 8}}}, slabBytes: 8, contains: "non-finite"},
		{name: "bbox order", index: testIndex{records: []divisionRecord{{subtype: 0, bbox: [4]float32{2, 1, 0, 1}, length: 8}}}, slabBytes: 8, contains: "invalid WGS84 bbox"},
		{name: "offset overflow", index: testIndex{records: []divisionRecord{{subtype: 0, bbox: validRecord.bbox, offset: math.MaxUint64 - 3, length: 8}}}, slabBytes: 8, contains: "overflows uint64"},
		{name: "range beyond slab", index: testIndex{records: []divisionRecord{{subtype: 0, bbox: validRecord.bbox, offset: 4, length: 8}}}, slabBytes: 8, contains: "exceeds file size"},
		{name: "coarse candidate", index: testIndex{records: []divisionRecord{validRecord}, coarse: map[[2]int16][]uint32{{0, 0}: {1}}}, slabBytes: 8, contains: "candidate 1 is out of bounds"},
		{name: "fine candidate", index: testIndex{records: []divisionRecord{validRecord}, fine: map[[2]int16][]uint32{{0, 0}: {1}}}, slabBytes: 8, contains: "candidate 1 is out of bounds"},
		{name: "preindex candidate", index: testIndex{records: []divisionRecord{validRecord}, preindex: map[[2]int16]uint32{{0, 0}: 1}}, slabBytes: 8, contains: "candidate 1 is out of bounds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := openRawFixture(t, encodeTestIndex(test.index), make([]byte, test.slabBytes))
			assert.ErrorContains(t, err, test.contains)
		})
	}
}

func TestResolveRejectsShortSlabAndMalformedFlatBuffer(t *testing.T) {
	division := testDivision{
		name: "Square", id: "square", subtype: subtypeCountry, bbox: [4]float32{0, 1, 0, 1},
		polygons: []testPolygon{{exterior: rectangle(0, 0, 1, 1)}},
	}

	t.Run("short read", func(t *testing.T) {
		resolver, _, slabPath := newFixture(t, []testDivision{division}, map[[2]int16][]uint32{{0, 0}: {0}}, nil, nil)
		require.NoError(t, os.Truncate(slabPath, 4))
		_, err := resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 0.5, Latitude: 0.5})
		assert.ErrorContains(t, err, "unexpected EOF")
	})

	t.Run("malformed flatbuffer panic becomes error", func(t *testing.T) {
		chunk := buildDivisionChunk(division)
		rootPosition := 4 + int(binary.LittleEndian.Uint32(chunk[4:8]))
		binary.LittleEndian.PutUint32(chunk[rootPosition:rootPosition+4], math.MaxUint32)
		record := divisionRecord{subtype: subtypeCountry, bbox: division.bbox, length: uint32(len(chunk))}
		index := encodeTestIndex(testIndex{records: []divisionRecord{record}, coarse: map[[2]int16][]uint32{{0, 0}: {0}}})
		resolver, err := openRawFixture(t, index, chunk)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, resolver.Close()) })
		assert.NotPanics(t, func() {
			_, err = resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 0.5, Latitude: 0.5})
		})
		assert.ErrorContains(t, err, "malformed slab FlatBuffer")
	})

	t.Run("invalid size prefix", func(t *testing.T) {
		chunk := buildDivisionChunk(division)
		binary.LittleEndian.PutUint32(chunk[:4], uint32(len(chunk)))
		record := divisionRecord{subtype: subtypeCountry, bbox: division.bbox, length: uint32(len(chunk))}
		index := encodeTestIndex(testIndex{records: []divisionRecord{record}, coarse: map[[2]int16][]uint32{{0, 0}: {0}}})
		resolver, err := openRawFixture(t, index, chunk)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, resolver.Close()) })
		_, err = resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 0.5, Latitude: 0.5})
		assert.ErrorContains(t, err, "size prefix")
	})
}

func TestResolveCancellationCloseAndConcurrency(t *testing.T) {
	division := testDivision{
		name: "Square", id: "square", subtype: subtypeCountry, bbox: [4]float32{0, 1, 0, 1},
		polygons: []testPolygon{{exterior: rectangle(0, 0, 1, 1)}},
	}
	resolver := openFixture(t, []testDivision{division}, map[[2]int16][]uint32{{0, 0}: {0}}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := resolver.Resolve(ctx, admingeo.Coordinate{Longitude: 0.5, Latitude: 0.5})
	assert.ErrorIs(t, err, context.Canceled)

	const goroutines = 32
	const queries = 50
	var wait sync.WaitGroup
	errors := make(chan error, goroutines)
	for range goroutines {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range queries {
				path, err := resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 0.5, Latitude: 0.5})
				if err != nil {
					errors <- err
					return
				}
				if path.Country != "Square" {
					errors <- assert.AnError
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	assert.Empty(t, errors)

	require.NoError(t, resolver.Close())
	require.NoError(t, resolver.Close())
	_, err = resolver.Resolve(context.Background(), admingeo.Coordinate{Longitude: 0.5, Latitude: 0.5})
	assert.ErrorContains(t, err, "closed")
}

func TestSlabCacheIsByteBounded(t *testing.T) {
	cache := newSlabCache(5)
	cache.put(1, []byte("abc"))
	cache.put(2, []byte("def"))
	_, firstPresent := cache.get(1)
	second, secondPresent := cache.get(2)
	assert.False(t, firstPresent)
	assert.True(t, secondPresent)
	assert.Equal(t, []byte("def"), second)

	cache.put(3, []byte("too large"))
	_, oversizedPresent := cache.get(3)
	assert.False(t, oversizedPresent)
	assert.LessOrEqual(t, cache.bytes, cache.maxBytes)
	assert.Nil(t, newSlabCache(0))
}

func openFixture(t *testing.T, divisions []testDivision, coarse, fine map[[2]int16][]uint32, preindex map[[2]int16]uint32, options ...Option) *Resolver {
	t.Helper()
	resolver, _, _ := newFixture(t, divisions, coarse, fine, preindex, options...)
	return resolver
}

func newFixture(t *testing.T, divisions []testDivision, coarse, fine map[[2]int16][]uint32, preindex map[[2]int16]uint32, options ...Option) (*Resolver, string, string) {
	t.Helper()
	directory := t.TempDir()
	indexPath := filepath.Join(directory, "divisions.xs-index.gz")
	slabPath := filepath.Join(directory, "divisions.xs-poly")
	var slab []byte
	records := make([]divisionRecord, len(divisions))
	for i, division := range divisions {
		chunk := buildDivisionChunk(division)
		records[i] = divisionRecord{subtype: division.subtype, bbox: division.bbox, offset: uint64(len(slab)), length: uint32(len(chunk))}
		slab = append(slab, chunk...)
	}
	require.NoError(t, os.WriteFile(slabPath, slab, 0o600))
	require.NoError(t, writeGzip(indexPath, encodeTestIndex(testIndex{records: records, coarse: coarse, fine: fine, preindex: preindex})))
	resolver, err := New(indexPath, slabPath, "fixture-v1", options...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resolver.Close()) })
	return resolver, indexPath, slabPath
}

func openRawFixture(t *testing.T, rawIndex, slab []byte) (*Resolver, error) {
	t.Helper()
	directory := t.TempDir()
	indexPath := filepath.Join(directory, "index.gz")
	slabPath := filepath.Join(directory, "slab")
	require.NoError(t, writeGzip(indexPath, rawIndex))
	require.NoError(t, os.WriteFile(slabPath, slab, 0o600))
	return New(indexPath, slabPath, "test", WithCacheBytes(0))
}

func writeGzip(path string, data []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := gzip.NewWriter(file)
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		_ = file.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func encodeTestIndex(index testIndex) []byte {
	var buffer bytes.Buffer
	header := make([]byte, indexHeaderSize)
	copy(header, "XSCI")
	header[4] = 1
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(index.records)))
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(index.coarse)))
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(index.fine)))
	binary.LittleEndian.PutUint32(header[20:24], uint32(len(index.preindex)))
	buffer.Write(header)
	for _, record := range index.records {
		raw := make([]byte, recordSize)
		raw[0] = record.subtype
		for i, value := range record.bbox {
			binary.LittleEndian.PutUint32(raw[1+i*4:5+i*4], math.Float32bits(value))
		}
		binary.LittleEndian.PutUint64(raw[17:25], record.offset)
		binary.LittleEndian.PutUint32(raw[25:29], record.length)
		buffer.Write(raw)
	}
	writeTestGrid(&buffer, index.coarse)
	writeTestGrid(&buffer, index.fine)
	for key, candidate := range index.preindex {
		raw := make([]byte, 8)
		binary.LittleEndian.PutUint16(raw[0:2], uint16(key[0]))
		binary.LittleEndian.PutUint16(raw[2:4], uint16(key[1]))
		binary.LittleEndian.PutUint32(raw[4:8], candidate)
		buffer.Write(raw)
	}
	return buffer.Bytes()
}

func writeTestGrid(buffer *bytes.Buffer, grid map[[2]int16][]uint32) {
	for key, candidates := range grid {
		raw := make([]byte, 6)
		binary.LittleEndian.PutUint16(raw[0:2], uint16(key[0]))
		binary.LittleEndian.PutUint16(raw[2:4], uint16(key[1]))
		binary.LittleEndian.PutUint16(raw[4:6], uint16(len(candidates)))
		buffer.Write(raw)
		for _, candidate := range candidates {
			binary.Write(buffer, binary.LittleEndian, candidate)
		}
	}
}

func buildDivisionChunk(division testDivision) []byte {
	builder := flatbuffers.NewBuilder(256)
	name := builder.CreateString(division.name)
	id := builder.CreateString(division.id)
	polygons := make([]flatbuffers.UOffsetT, len(division.polygons))
	for i, polygon := range division.polygons {
		polygons[i] = buildPolygon(builder, polygon)
	}
	xs.CompressedDivisionStartPolygonsVector(builder, len(polygons))
	for i := len(polygons) - 1; i >= 0; i-- {
		builder.PrependUOffsetT(polygons[i])
	}
	polygonVector := builder.EndVector(len(polygons))
	xs.CompressedDivisionStart(builder)
	xs.CompressedDivisionAddId(builder, id)
	xs.CompressedDivisionAddName(builder, name)
	xs.CompressedDivisionAddSubtype(builder, xs.Subtype(division.subtype))
	xs.CompressedDivisionAddPolygons(builder, polygonVector)
	bbox := xs.CreateBBox(builder, division.bbox[0], division.bbox[1], division.bbox[2], division.bbox[3])
	xs.CompressedDivisionAddBbox(builder, bbox)
	root := xs.CompressedDivisionEnd(builder)
	xs.FinishSizePrefixedCompressedDivisionBuffer(builder, root)
	return bytes.Clone(builder.FinishedBytes())
}

func buildPolygon(builder *flatbuffers.Builder, polygon testPolygon) flatbuffers.UOffsetT {
	exterior := buildRing(builder, polygon.exterior)
	holes := make([]flatbuffers.UOffsetT, len(polygon.holes))
	for i, hole := range polygon.holes {
		holes[i] = buildRing(builder, hole)
	}
	var holeVector flatbuffers.UOffsetT
	if len(holes) != 0 {
		xs.CompressedPolygonStartHolesVector(builder, len(holes))
		for i := len(holes) - 1; i >= 0; i-- {
			builder.PrependUOffsetT(holes[i])
		}
		holeVector = builder.EndVector(len(holes))
	}
	xs.CompressedPolygonStart(builder)
	xs.CompressedPolygonAddExterior(builder, exterior)
	if holeVector != 0 {
		xs.CompressedPolygonAddHoles(builder, holeVector)
	}
	return xs.CompressedPolygonEnd(builder)
}

func buildRing(builder *flatbuffers.Builder, points []testPoint) flatbuffers.UOffsetT {
	data := encodeRing(points)
	dataVector := builder.CreateByteVector(data)
	xs.CompressedRingStart(builder)
	xs.CompressedRingAddData(builder, dataVector)
	xs.CompressedRingAddPointCount(builder, uint32(len(points)))
	return xs.CompressedRingEnd(builder)
}

func encodeRing(points []testPoint) []byte {
	var data []byte
	var previousLng, previousLat int32
	var scratch [binary.MaxVarintLen32]byte
	for _, point := range points {
		lng := int32(math.Round(point.lng * compressedCoordinateScale))
		lat := int32(math.Round(point.lat * compressedCoordinateScale))
		data = appendZigZag(data, scratch[:], lng-previousLng)
		data = appendZigZag(data, scratch[:], lat-previousLat)
		previousLng, previousLat = lng, lat
	}
	return data
}

func appendZigZag(data, scratch []byte, value int32) []byte {
	encoded := uint32(value<<1) ^ uint32(value>>31)
	n := binary.PutUvarint(scratch, uint64(encoded))
	return append(data, scratch[:n]...)
}

func rectangle(xmin, ymin, xmax, ymax float64) []testPoint {
	return []testPoint{{xmin, ymin}, {xmax, ymin}, {xmax, ymax}, {xmin, ymax}}
}
