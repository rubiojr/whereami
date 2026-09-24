package vecmap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"net/http"
	"path/filepath"

	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
	"github.com/rubiojr/whereami/pkg/vecmap/tileio"
)

const (
	openFreeMapSnapshot = tileio.OpenFreeMapSnapshot
	openFreeMapBaseURL  = tileio.OpenFreeMapBaseURL
	naturalEarthBaseURL = "https://tiles.openfreemap.org/natural_earth/ne2sr"
	naturalEarthMaxZoom = 6
	pinnedTileURL       = openFreeMapBaseURL + "/9/250/193.pbf"
	pinnedTileCacheName = tileio.PinnedCacheName
	pinnedTileSHA256    = tileio.PinnedSHA256
	maxTileBytes        = mvt.MaxTileBytes
	maxRoadSegments     = 100_000
	maxFillTriangles    = 100_000
	maxFillRingPoints   = mvt.MaxPolygonPoints
	maxTriangulationOps = 10_000_000
)

var (
	pinnedTile     = vectorTileID{X: 250, Y: 193, Z: 9}
	tileHTTPClient = tileio.NewClient()
	errRasterData  = errors.New("invalid Natural Earth raster")
)

func checkTileRedirect(request *http.Request, via []*http.Request) error {
	return tileio.CheckRedirect(request, via)
}

type tileSource struct {
	url       string
	cacheName string
	sha256    string
	tile      vectorTileID
}

type roadTileLoader func(context.Context, vectorTileID) (*tileBucket, error)

type naturalEarthRaster struct {
	rgba          []byte
	width, height int
}

func openFreeMapRoadLoader(cacheDir string) roadTileLoader {
	return func(ctx context.Context, tile vectorTileID) (*tileBucket, error) {
		bucket, err := loadRoadBucket(ctx, tileHTTPClient, openFreeMapRoadSource(tile), cacheDir)
		if err != nil {
			return nil, err
		}
		if tile.Z <= naturalEarthMaxZoom {
			bucket.raster, err = loadNaturalEarthRaster(ctx, tileHTTPClient, tile, cacheDir)
			if err != nil {
				return nil, err
			}
		}
		return bucket, nil
	}
}

func openFreeMapRoadSource(tile vectorTileID) tileSource {
	s := tileio.OpenFreeMap(tile)
	return tileSource{url: s.URL, cacheName: s.CacheName, sha256: s.SHA256, tile: tile}
}

func loadRoadBucket(ctx context.Context, client *http.Client, source tileSource, cacheDir string) (*tileBucket, error) {
	cachePath := filepath.Join(cacheDir, source.cacheName)
	if cacheDir != "" {
		data, expected, err := readCachedTile(cachePath, source.sha256)
		if err == nil && verifyTileChecksum(data, expected) == nil {
			return decodeRoadBucket(data, source.tile)
		}
	}

	data, err := fetchTile(ctx, client, source.url)
	if err != nil {
		return nil, err
	}
	checksum := tileChecksum(data)
	if source.sha256 != "" {
		if err := verifyTileChecksum(data, source.sha256); err != nil {
			return nil, err
		}
		checksum = source.sha256
	}
	bucket, err := decodeRoadBucket(data, source.tile)
	if err != nil {
		return nil, err
	}
	if cacheDir != "" {
		_ = writeCachedTilePair(cacheDir, source.cacheName, data, checksum)
	}
	return bucket, nil
}

func fetchTile(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	return fetchBounded(ctx, client, url, "application/vnd.mapbox-vector-tile, application/x-protobuf")
}

func fetchBounded(ctx context.Context, client *http.Client, url, accept string) ([]byte, error) {
	return tileio.Fetch(ctx, client, url, accept)
}

func loadNaturalEarthRaster(
	ctx context.Context,
	client *http.Client,
	tile vectorTileID,
	cacheDir string,
) (naturalEarthRaster, error) {
	name := filepath.Join(
		"openfreemap-natural-earth",
		fmt.Sprintf("%d", tile.Z),
		fmt.Sprintf("%d", tile.X),
		fmt.Sprintf("%d.png", tile.Y),
	)
	cachePath := filepath.Join(cacheDir, name)
	if cacheDir != "" {
		data, expected, err := readCachedTile(cachePath, "")
		if err == nil && verifyTileChecksum(data, expected) == nil {
			raster, decodeErr := decodeNaturalEarthRaster(data)
			if decodeErr == nil {
				return raster, nil
			}
		}
	}
	url := fmt.Sprintf("%s/%d/%d/%d.png", naturalEarthBaseURL, tile.Z, tile.X, tile.Y)
	data, err := fetchBounded(ctx, client, url, "image/png")
	if err != nil {
		return naturalEarthRaster{}, fmt.Errorf("load Natural Earth raster: %w", err)
	}
	raster, err := decodeNaturalEarthRaster(data)
	if err != nil {
		return naturalEarthRaster{}, err
	}
	if cacheDir != "" {
		checksum := tileChecksum(data)
		_ = writeCachedTilePair(cacheDir, name, data, checksum)
	}
	return raster, nil
}

func validateNaturalEarthRaster(data []byte) error {
	configuration, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: decode PNG: %v", errRasterData, err)
	}
	validSize := configuration.Width == int(tileSize) || configuration.Width == int(tileSize)*2
	if !validSize || configuration.Height != configuration.Width {
		return fmt.Errorf(
			"%w: image is %dx%d, want square %d or %d physical pixels",
			errRasterData,
			configuration.Width,
			configuration.Height,
			int(tileSize),
			int(tileSize)*2,
		)
	}
	return nil
}

func decodeNaturalEarthRaster(data []byte) (naturalEarthRaster, error) {
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return naturalEarthRaster{}, fmt.Errorf("%w: decode PNG: %v", errRasterData, err)
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	validSize := width == int(tileSize) || width == int(tileSize)*2
	if !validSize || height != width {
		return naturalEarthRaster{}, fmt.Errorf(
			"%w: image is %dx%d, want square %d or %d physical pixels",
			errRasterData,
			width,
			height,
			int(tileSize),
			int(tileSize)*2,
		)
	}
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(rgba, rgba.Bounds(), decoded, bounds.Min, draw.Src)
	return naturalEarthRaster{rgba: rgba.Pix, width: width, height: height}, nil
}

func verifyTileChecksum(data []byte, expected string) error {
	return tileio.Verify(data, expected)
}

func tileChecksum(data []byte) string {
	return tileio.Checksum(data)
}

func readCachedTile(path, expected string) ([]byte, string, error) {
	return tileio.ReadCached(path, expected)
}

func writeCachedTilePair(cacheDir, name string, data []byte, checksum string) error {
	return tileio.WritePair(cacheDir, name, data, checksum, pinnedTileCacheName)
}

func enforceTileCacheLimit(cacheDir string, maximumBytes int64) error {
	return tileio.Enforce(cacheDir, pinnedTileCacheName, maximumBytes)
}

func writeTileAtomically(cacheDir, name string, data []byte) error {
	return tileio.WriteAtomic(cacheDir, name, data)
}

func supportedRoadClass(class string) bool {
	switch class {
	case "motorway", "trunk", "primary", "secondary", "tertiary",
		"minor", "service", "track", "path", "raceway", "residential",
		"unclassified", "living_street", "construction":
		return true
	default:
		return false
	}
}
