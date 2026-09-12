// Command vecmap-fixture captures the existing map compiler's output for
// deterministic, network-free backend comparisons.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

func main() {
	tile := flag.String("tile", "", "pinned z9/250/193 PBF file")
	glyphDir := flag.String("glyph-dir", "", "directory of URL-escaped font-stack.pbf files (range 0-255)")
	output := flag.String("out", "", "output JSON scene (stdout when empty)")
	indexed := flag.Bool("indexed", false, "deduplicate vertices into indexed buffers before capturing")
	directIndexed := flag.Bool("direct-indexed", false, "construct indexed geometry directly without vertex deduplication")
	flag.Parse()
	if err := run(*tile, *glyphDir, *output, *indexed, *directIndexed); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path, glyphDir, output string, indexed, directIndexed bool) error {
	if indexed && directIndexed {
		return fmt.Errorf("-indexed and -direct-indexed are mutually exclusive")
	}
	options := vecmap.RenderFixtureOptions{DirectIndexed: directIndexed}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var load vecmap.FixtureGlyphLoader
	if glyphDir != "" {
		load = func(fonts []string) (map[string][]byte, error) { return loadFixtureGlyphs(glyphDir, fonts) }
	}
	fixture, err := vecmap.CompileRenderFixtureWithGlyphLoader(data, load, options)
	if err != nil {
		return err
	}
	document := scene.Document{Scene: *fixture.Scene, Transforms: fixture.Frame(fixture.Camera).Transforms, Width: 512, Height: 512, Labels: fixture.Labels, MissingFonts: fixture.MissingFonts, Source: "OpenFreeMap 20260823 z9/250/193; Liberty at fixed zoom 10"}
	document.Camera = &fixture.Camera
	document.TileSpaces = []scene.TileSpace{{Tile: view.TileID{X: 250, Y: 193, Z: 9}}}
	if indexed {
		if err := indexDocument(&document); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "draws=%d vertices=%d indices=%d textures=%d labels=%d missing_fonts=%q\n", len(document.Scene.Draws), len(document.Scene.Meshes[0].Vertices), len(document.Scene.Meshes[0].Indices), len(document.Scene.Textures), document.Labels, document.MissingFonts)
	out := os.Stdout
	if output != "" {
		out, err = os.Create(output)
		if err != nil {
			return err
		}
		defer out.Close()
	}
	return json.NewEncoder(out).Encode(document)
}

func loadFixtureGlyphs(dir string, fonts []string) (map[string][]byte, error) {
	glyphs := make(map[string][]byte, len(fonts))
	for _, font := range fonts {
		data, err := os.ReadFile(filepath.Join(dir, url.PathEscape(font)+".pbf"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		glyphs[font] = data
	}
	return glyphs, nil
}

func indexDocument(document *scene.Document) error {
	start := time.Now()
	var before, after uint64
	for i, mesh := range document.Scene.Meshes {
		before += mesh.BufferBytes()
		indexed, err := scene.IndexMesh(mesh)
		if err != nil {
			return err
		}
		document.Scene.Meshes[i] = indexed
		after += indexed.BufferBytes()
	}
	fmt.Fprintf(os.Stderr, "indexing=%s geometry_bytes=%d->%d\n", time.Since(start), before, after)
	return document.Validate()
}
