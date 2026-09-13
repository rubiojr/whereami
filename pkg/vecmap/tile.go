package vecmap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
)

const (
	openFreeMapSnapshot         = "20260823_080002_pt"
	openFreeMapBaseURL          = "https://tiles.openfreemap.org/planet/" + openFreeMapSnapshot
	naturalEarthBaseURL         = "https://tiles.openfreemap.org/natural_earth/ne2sr"
	naturalEarthMaxZoom         = 6
	pinnedTileURL               = openFreeMapBaseURL + "/9/250/193.pbf"
	pinnedTileCacheName         = "openfreemap-20260823-z9-250-193.pbf"
	pinnedTileSHA256            = "5007c887f3c99a2c0737b9a3afdf3813ef3e1ce939a63aa09a8407b7c3770c79"
	maxTileBytes                = mvt.MaxTileBytes
	maxRoadSegments             = 100_000
	maxFillTriangles            = 100_000
	maxFillRingPoints           = mvt.MaxPolygonPoints
	maxTriangulationOps         = 10_000_000
	defaultTileCacheBytes int64 = 256 << 20
)

var (
	pinnedTile     = vectorTileID{X: 250, Y: 193, Z: 9}
	tileHTTPClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: checkTileRedirect}
	errRasterData  = errors.New("invalid Natural Earth raster")
	tileCacheMu    sync.Mutex
)

func checkTileRedirect(request *http.Request, via []*http.Request) error {
	if len(via) == 0 || len(via) >= 10 {
		return errors.New("refuse invalid tile redirect chain")
	}
	original := via[0].URL
	if request.URL.Scheme != "https" || !strings.EqualFold(request.URL.Host, original.Host) {
		return fmt.Errorf("refuse tile redirect from %s to %s", original.Host, request.URL.Host)
	}
	return nil
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
	source := tileSource{
		url: fmt.Sprintf(
			"%s/%d/%d/%d.pbf",
			openFreeMapBaseURL,
			tile.Z,
			tile.X,
			tile.Y,
		),
		cacheName: filepath.Join(
			"openfreemap-"+openFreeMapSnapshot,
			fmt.Sprintf("%d", tile.Z),
			fmt.Sprintf("%d", tile.X),
			fmt.Sprintf("%d.pbf", tile.Y),
		),
		tile: tile,
	}
	if tile == pinnedTile {
		source.sha256 = pinnedTileSHA256
		source.cacheName = pinnedTileCacheName
	}
	return source
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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create tile request: %w", err)
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "vecmap/1")

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch tile: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch tile: unexpected HTTP status %s", response.Status)
	}

	data, err := readBounded(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read tile response: %w", err)
	}
	return data, nil
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

func readBoundedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBounded(file)
}

func readBounded(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxTileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTileBytes {
		return nil, fmt.Errorf("tile exceeds %d-byte limit", maxTileBytes)
	}
	return data, nil
}

func verifyTileChecksum(data []byte, expected string) error {
	actual := tileChecksum(data)
	if actual != expected {
		return fmt.Errorf("tile checksum mismatch: got %s", actual)
	}
	return nil
}

func tileChecksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readCachedTile(path, expected string) ([]byte, string, error) {
	tileCacheMu.Lock()
	defer tileCacheMu.Unlock()

	data, err := readBoundedFile(path)
	if err != nil {
		return nil, "", err
	}
	if expected == "" {
		checksum, err := readBoundedFile(path + ".sha256")
		if err != nil {
			return nil, "", err
		}
		expected = strings.TrimSpace(string(checksum))
		decoded, err := hex.DecodeString(expected)
		if err != nil || len(decoded) != sha256.Size {
			return nil, "", errors.New("cached tile checksum is invalid")
		}
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	if expected != "" {
		_ = os.Chtimes(path+".sha256", now, now)
	}
	return data, expected, nil
}

func writeCachedTilePair(cacheDir, name string, data []byte, checksum string) error {
	tileCacheMu.Lock()
	defer tileCacheMu.Unlock()

	if err := writeTileAtomically(cacheDir, name, data); err != nil {
		return err
	}
	if err := writeTileAtomically(cacheDir, name+".sha256", []byte(checksum+"\n")); err != nil {
		_ = os.Remove(filepath.Join(cacheDir, name))
		_ = os.Remove(filepath.Join(cacheDir, name+".sha256"))
		return err
	}
	_ = enforceTileCacheLimitLocked(cacheDir, defaultTileCacheBytes)
	return nil
}

type tileCacheEntry struct {
	base     string
	paths    []string
	size     int64
	modified time.Time
}

func enforceTileCacheLimit(cacheDir string, maximumBytes int64) error {
	tileCacheMu.Lock()
	defer tileCacheMu.Unlock()
	return enforceTileCacheLimitLocked(cacheDir, maximumBytes)
}

func enforceTileCacheLimitLocked(cacheDir string, maximumBytes int64) error {
	if cacheDir == "" || maximumBytes <= 0 {
		return nil
	}
	entries := make(map[string]*tileCacheEntry)
	var cleanupErrors []error
	err := filepath.WalkDir(cacheDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			cleanupErrors = append(cleanupErrors, walkErr)
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".tile-") {
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, removeErr)
			}
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			cleanupErrors = append(cleanupErrors, infoErr)
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		base := strings.TrimSuffix(path, ".sha256")
		cacheEntry := entries[base]
		if cacheEntry == nil {
			cacheEntry = &tileCacheEntry{base: base}
			entries[base] = cacheEntry
		}
		cacheEntry.paths = append(cacheEntry.paths, path)
		cacheEntry.size += info.Size()
		if info.ModTime().After(cacheEntry.modified) {
			cacheEntry.modified = info.ModTime()
		}
		return nil
	})
	if err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}

	var totalBytes int64
	candidates := make([]*tileCacheEntry, 0, len(entries))
	pinnedPath := filepath.Join(cacheDir, pinnedTileCacheName)
	for _, entry := range entries {
		totalBytes += entry.size
		if entry.base != pinnedPath {
			candidates = append(candidates, entry)
		}
	}
	sort.Slice(candidates, func(first, second int) bool {
		return candidates[first].modified.Before(candidates[second].modified)
	})
	for _, entry := range candidates {
		if totalBytes <= maximumBytes {
			break
		}
		removed := true
		for _, path := range entry.paths {
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, removeErr)
				removed = false
			}
		}
		if removed {
			totalBytes -= entry.size
		}
	}
	return errors.Join(cleanupErrors...)
}

func writeTileAtomically(cacheDir, name string, data []byte) error {
	cleanName := filepath.Clean(name)
	if filepath.IsAbs(name) || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
		return errors.New("tile cache path escapes cache directory")
	}
	target := filepath.Join(cacheDir, cleanName)
	targetDir := filepath.Dir(target)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(targetDir, ".tile-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, target)
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
