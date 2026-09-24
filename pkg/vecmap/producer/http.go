package producer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/rubiojr/whereami/pkg/vecmap/tileio"
)

// HTTPLoader reuses vecmap's bounded transport, redirect policy, checksum pairs,
// pinned OpenFreeMap source and disk eviction. template is also the Style.Source
// identity. An empty cache directory disables disk caching. A supplied client must
// enforce its own timeout/redirect policy; nil selects tileio.NewClient().
func HTTPLoader(client *http.Client, directory, template string) (Loader, error) {
	return sourceLoader(client, directory, template, false)
}

// CacheLoader replays only checksum-verified cached responses. Missing or corrupt
// entries fail explicitly; it never falls back to network I/O. Source paths and
// pin policy are identical to HTTPLoader. Use it for reproducible workload inputs.
func CacheLoader(directory, template string) (Loader, error) {
	if directory == "" {
		return nil, ErrInput
	}
	return sourceLoader(nil, directory, template, true)
}

func sourceLoader(client *http.Client, directory, template string, cacheOnly bool) (Loader, error) {
	u, err := url.Parse(template)
	if err != nil || len(template) > 256 || u == nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		!strings.Contains(template, "{z}") || !strings.Contains(template, "{x}") || !strings.Contains(template, "{y}") {
		return nil, ErrInput
	}
	if client == nil {
		client = tileio.NewClient()
	}
	return func(ctx context.Context, key Key, writer io.Writer) error {
		if key.Source != template || key.Tile.Z > 14 || !key.Tile.Valid() {
			return ErrPermanent
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		source := tileio.OpenFreeMap(key.Tile)
		if template != tileio.OpenFreeMapTemplate {
			source.URL = strings.NewReplacer("{z}", fmt.Sprint(key.Tile.Z), "{x}", fmt.Sprint(key.Tile.X), "{y}", fmt.Sprint(key.Tile.Y)).Replace(template)
			source.CacheName = filepath.Join("source-"+tileio.Checksum([]byte(template)), fmt.Sprint(key.Tile.Z), fmt.Sprint(key.Tile.X), fmt.Sprintf("%d.pbf", key.Tile.Y))
			source.SHA256 = ""
		}
		var data []byte
		var err error
		if cacheOnly {
			data, err = readSourceCache(directory, source)
			if err != nil {
				return fmt.Errorf("%w: cached tile %d/%d/%d: %v", ErrMissing, key.Tile.Z, key.Tile.X, key.Tile.Y, err)
			}
		} else {
			data, err = loadHTTP(ctx, client, directory, source)
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err = writer.Write(data)
		return err
	}, nil
}

func readSourceCache(directory string, source tileio.Source) ([]byte, error) {
	data, expected, err := tileio.ReadCached(filepath.Join(directory, source.CacheName), source.SHA256)
	if err == nil {
		err = tileio.Verify(data, expected)
	}
	return data, err
}

func loadHTTP(ctx context.Context, client *http.Client, directory string, source tileio.Source) ([]byte, error) {
	if directory != "" {
		data, err := readSourceCache(directory, source)
		if err == nil {
			return data, nil
		}
	}
	data, err := tileio.Fetch(ctx, client, source.URL, "application/vnd.mapbox-vector-tile, application/x-protobuf")
	if err != nil {
		var status *tileio.StatusError
		switch {
		case errors.Is(err, tileio.ErrLimit):
			return nil, fmt.Errorf("%w: %v", ErrLimit, err)
		case errors.As(err, &status):
			if status.Code == 404 {
				return nil, fmt.Errorf("%w: %v", ErrMissing, err)
			}
			if status.Code >= 400 && status.Code < 500 && status.Code != 408 && status.Code != 429 {
				return nil, fmt.Errorf("%w: %v", ErrPermanent, err)
			}
		}
		return nil, err
	}
	if source.SHA256 != "" {
		if err := tileio.Verify(data, source.SHA256); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPermanent, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if directory != "" {
		_ = tileio.WritePair(directory, source.CacheName, data, tileio.Checksum(data), tileio.PinnedCacheName)
	}
	return data, nil
}
