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

	"github.com/rubiojr/whereami/pkg/vecmap"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

func main() {
	tile := flag.String("tile", "", "pinned z9/250/193 PBF file")
	glyphDir := flag.String("glyph-dir", "", "directory of URL-escaped font-stack.pbf files (range 0-255)")
	output := flag.String("out", "", "output JSON scene (stdout when empty)")
	flag.Parse()
	if err := run(*tile, *glyphDir, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path, glyphDir, output string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fixture, err := vecmap.CompileRenderFixture(data, nil)
	if err != nil {
		return err
	}
	if glyphDir != "" {
		glyphs := make(map[string][]byte)
		for _, font := range fixture.MissingFonts {
			data, err := os.ReadFile(filepath.Join(glyphDir, url.PathEscape(font)+".pbf"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			glyphs[font] = data
		}
		fixture, err = vecmap.CompileRenderFixture(data, glyphs)
		if err != nil {
			return err
		}
	}
	document := scene.Document{Scene: *fixture.Scene, Transforms: fixture.Frame(fixture.Camera).Transforms, Width: 512, Height: 512, Labels: fixture.Labels, MissingFonts: fixture.MissingFonts, Source: "OpenFreeMap 20260823 z9/250/193; Liberty at fixed zoom 10"}
	fmt.Fprintf(os.Stderr, "draws=%d vertices=%d textures=%d labels=%d missing_fonts=%q\n", len(document.Scene.Draws), len(document.Scene.Meshes[0].Vertices), len(document.Scene.Textures), document.Labels, document.MissingFonts)
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
