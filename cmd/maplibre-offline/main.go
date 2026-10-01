// Command maplibre-offline prepares the pinned Liberty style, sprites and glyphs
// for MapLibre Native over the tile cache that vecmap's viewer replays with
// -cache-only, so both renderers draw identical bytes without network access.
//
// The written style.json reads tiles, sprites and glyphs through file:// URLs.
// With -record, it also serves style-record.json, whose tiles come from an HTTP
// server on that address. The server answers from the cache and fetches misses
// into it with vecmap's own loader, so the cache grows to cover what MapLibre
// requests during a trace.
//
// Usage:
//
//	maplibre-offline -cache-dir DIR -glyph-dir DIR -out DIR [-record 127.0.0.1:8765]
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/producer"
	"github.com/rubiojr/whereami/pkg/vecmap/tileio"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// Liberty's text-font stacks, each a supplied 0-255 range as vecmap loads them.
var fontStacks = []string{"Noto Sans Regular", "Noto Sans Bold", "Noto Sans Italic"}

func main() {
	cacheDir := flag.String("cache-dir", "", "vecmap tile cache (the viewer's -cache-dir)")
	glyphDir := flag.String("glyph-dir", "", "supplied Noto Sans Regular/Bold/Italic 0-255 PBF directory")
	out := flag.String("out", "", "directory for style.json, sprites and fonts")
	record := flag.String("record", "", "serve style-record.json tiles on this address and fetch misses into -cache-dir")
	flag.Parse()
	if *cacheDir == "" || *glyphDir == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*cacheDir, *glyphDir, *out, *record); err != nil {
		log.Fatal(err)
	}
}

func run(cacheDir, glyphDir, out, record string) error {
	cacheDir, err := filepath.Abs(cacheDir)
	if err != nil {
		return err
	}
	if out, err = filepath.Abs(out); err != nil {
		return err
	}
	if err := writeAssets(cacheDir, glyphDir, out); err != nil {
		return err
	}
	fmt.Printf("style %s\n", filepath.Join(out, "style.json"))
	if record == "" {
		return nil
	}
	styleData, _, _ := liberty.Files()
	recorded, err := offlineStyle(styleData, "http://"+record+"/tiles/{z}/{x}/{y}.pbf", out)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "style-record.json"), recorded, 0o644); err != nil {
		return err
	}
	load, err := producer.HTTPLoader(nil, cacheDir, tileio.OpenFreeMapTemplate)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	server := &http.Server{Addr: record, Handler: tileHandler(load)}
	go func() { <-ctx.Done(); server.Close() }()
	fmt.Printf("recording %s\n", filepath.Join(out, "style-record.json"))
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// writeAssets writes the offline style, the pinned sprite atlas and the supplied
// glyph ranges, and exposes the pinned z9 tile at its XYZ path for MapLibre.
func writeAssets(cacheDir, glyphDir, out string) error {
	styleData, index, atlas := liberty.Files()
	tiles := "file://" + filepath.ToSlash(filepath.Join(cacheDir, "openfreemap-"+tileio.OpenFreeMapSnapshot)) + "/{z}/{x}/{y}.pbf"
	offline, err := offlineStyle(styleData, tiles, out)
	if err != nil {
		return err
	}
	files := map[string][]byte{"style.json": offline, "sprites/ofm.json": index, "sprites/ofm.png": atlas}
	for _, stack := range fontStacks {
		data, err := tileio.ReadFile(filepath.Join(glyphDir, url.PathEscape(stack)+".pbf"))
		if err != nil {
			return fmt.Errorf("glyphs %s: %w", stack, err)
		}
		files[filepath.Join("fonts", stack, "0-255.pbf")] = data
		// vecmap has no glyphs beyond 0-255. Empty ranges give MapLibre the same
		// coverage: a failed range would leave its tiles loading forever.
		for start := 256; start < 1<<16; start += 256 {
			files[filepath.Join("fonts", stack, fmt.Sprintf("%d-%d.pbf", start, start+255))] = nil
		}
	}
	for name, data := range files {
		path := filepath.Join(out, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	// vecmap reads z9/250/193 from its pinned name; MapLibre only knows XYZ paths.
	pinned := tileio.OpenFreeMap(view.TileID{Z: 9, X: 250, Y: 193})
	data, _, err := tileio.ReadCached(filepath.Join(cacheDir, pinned.CacheName), pinned.SHA256)
	if err != nil {
		return fmt.Errorf("pinned tile: %w", err)
	}
	if err := tileio.Verify(data, pinned.SHA256); err != nil {
		return fmt.Errorf("pinned tile: %w", err)
	}
	return tileio.WriteAtomic(filepath.Join(cacheDir, "openfreemap-"+tileio.OpenFreeMapSnapshot, "9", "250"), "193.pbf", data)
}

// offlineStyle points the pinned style at local tiles, sprites and glyphs. The
// natural-earth raster (zoom 0-7) points at a missing directory: neither
// renderer draws it in the Madrid traces, and MapLibre must not fetch it.
func offlineStyle(data []byte, tiles, out string) ([]byte, error) {
	var style map[string]any
	if err := json.Unmarshal(data, &style); err != nil {
		return nil, err
	}
	sources, ok := style["sources"].(map[string]any)
	if !ok {
		return nil, errors.New("style has no sources")
	}
	raster, ok := sources["ne2_shaded"].(map[string]any)
	if !ok || sources["openmaptiles"] == nil || len(sources) != 2 {
		return nil, fmt.Errorf("unexpected Liberty sources %v", sources)
	}
	base := "file://" + filepath.ToSlash(out)
	raster["tiles"] = []string{base + "/missing/{z}/{x}/{y}.png"}
	sources["openmaptiles"] = map[string]any{"type": "vector", "minzoom": 0, "maxzoom": 14, "tiles": []string{tiles}}
	style["sprite"] = base + "/sprites/ofm"
	style["glyphs"] = base + "/fonts/{fontstack}/{range}.pbf"
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(style); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// tileHandler serves /tiles/{z}/{x}/{y}.pbf through vecmap's loader and logs
// whether each tile came from the cache.
func tileHandler(load producer.Loader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tile, ok := parseTile(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		var body bytes.Buffer
		key := producer.Key{Source: tileio.OpenFreeMapTemplate, Tile: tile}
		if err := load(r.Context(), key, &body); err != nil {
			log.Printf("tile %d/%d/%d: %v", tile.Z, tile.X, tile.Y, err)
			status := http.StatusBadGateway
			if errors.Is(err, producer.ErrMissing) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		log.Printf("tile %d/%d/%d %d bytes", tile.Z, tile.X, tile.Y, body.Len())
		w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
		w.Write(body.Bytes())
	})
}

func parseTile(path string) (view.TileID, bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/tiles/"), "/")
	if len(parts) != 3 || !strings.HasSuffix(parts[2], ".pbf") {
		return view.TileID{}, false
	}
	parts[2] = strings.TrimSuffix(parts[2], ".pbf")
	var values [3]uint32
	for i, part := range parts {
		value, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return view.TileID{}, false
		}
		values[i] = uint32(value)
	}
	tile := view.TileID{Z: values[0], X: values[1], Y: values[2]}
	return tile, tile.Valid() && tile.Z <= 14
}
