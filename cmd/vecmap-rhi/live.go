//go:build vecmap_rhi

package main

import (
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/producer"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/tileio"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

type liveOptions struct {
	enabled                   bool
	template, cache, glyphs   string
	latitude, longitude, zoom float64
	drawMargin                float64
	cacheBytes, sceneBytes    uint64
	workers, compilers        int
	cacheOnly                 bool
	residentGeometry          bool
	residentSymbols           bool
	residentDashes            bool
	compactVertices           bool
	packedVertices            bool
	shortIndices              bool
	coarser                   int
	keepPreparation           bool
	reuseDecoded              bool
	deferHiddenRefresh        bool
	reuseStable               bool
	cameraSelectInterval      time.Duration
}

type liveSource struct {
	p        *producer.Producer
	bridge   *producer.Bridge
	initial  *scene.Document
	request  producer.Request // GUI-owned after construction
	revision uint64
}

func newLiveSource(options liveOptions, budget retained.Budget) (*liveSource, error) {
	if options.glyphs == "" {
		return nil, fmt.Errorf("live mode requires -glyph-dir with supplied Noto Sans Regular/Bold/Italic 0-255 PBFs")
	}
	for _, v := range []float64{options.latitude, options.longitude, options.zoom} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, producer.ErrInput
		}
	}
	if options.coarser < 0 || options.coarser > view.MaxCoarser {
		return nil, producer.ErrInput
	}
	var load producer.Loader
	var err error
	if options.cacheOnly {
		load, err = producer.CacheLoader(options.cache, options.template)
	} else {
		load, err = producer.HTTPLoader(nil, options.cache, options.template)
	}
	if err != nil {
		return nil, err
	}
	layers, err := liberty.Layers()
	if err != nil {
		return nil, err
	}
	ranges := make(map[string][]byte)
	for _, name := range []string{"Noto Sans Regular", "Noto Sans Bold", "Noto Sans Italic"} {
		ranges[name], err = tileio.ReadFile(filepath.Join(options.glyphs, url.PathEscape(name)+".pbf"))
		if err != nil {
			return nil, fmt.Errorf("load supplied font %q: %w", name, err)
		}
	}
	fonts, err := compiler.DecodeFontRanges(ranges, 0)
	if err != nil {
		return nil, err
	}
	limits := producer.DefaultLimits()
	limits.CacheBytes = options.cacheBytes
	if options.sceneBytes != 0 {
		limits.SnapshotBytes = options.sceneBytes
	}
	// The viewer's assets never change, so preparation has no later use.
	limits.DiscardPreparation = !options.keepPreparation
	limits.ReuseDecoded = options.reuseDecoded
	limits.DeferHiddenRefresh = options.deferHiddenRefresh
	limits.ReuseStable = options.reuseStable
	limits.CameraSelectInterval = options.cameraSelectInterval
	limits.DrawMargin = options.drawMargin
	if options.workers != 0 {
		limits.Workers = options.workers
	}
	if options.compilers != 0 {
		limits.Compilers = options.compilers
	}
	p, err := producer.New(load, limits)
	if err != nil {
		return nil, err
	}
	camera := view.NewCamera(view.Coordinate{Latitude: options.latitude, Longitude: options.longitude}, options.zoom, 0, 800, 600)
	initial := &scene.Document{Width: 800, Height: 600, Camera: &camera, TileSpaces: []scene.TileSpace{{Tile: view.TileID{}}}}
	b, err := producer.NewBridge(p, initial, retained.ResidencyLimits{}, budget)
	if err != nil {
		p.Close()
		<-p.Done()
		return nil, err
	}
	l := &liveSource{p: p, bridge: b, initial: initial, request: producer.Request{Camera: camera,
		Style:  &producer.Style{Source: options.template, Epoch: 1, Layers: layers, Options: tiles.PrepareOptions{Zoom: view.StyleZoomAt(camera.Zoom, options.coarser), Coarser: options.coarser, Indexed: true, ResidentGeometry: options.residentGeometry, ResidentSymbols: options.residentSymbols, ResidentDashes: options.residentDashes, CompactVertices: options.compactVertices, PackedVertices: options.packedVertices, ShortIndices: options.shortIndices}, Bytes: 4 << 20},
		Assets: &producer.Assets{Epoch: 1, Bytes: 60 << 20, Value: tiles.Assets{Fonts: fonts, Sprite: liberty.Sprite, SpriteEntry: liberty.SpriteEntry, FallbackEligible: glyph.LegacyFallbackEligible}},
	}}
	l.revision, err = p.Submit(l.request)
	if err != nil {
		b.Close()
		return nil, err
	}
	return l, nil
}

func (l *liveSource) update(camera view.Camera) error {
	if camera == l.request.Camera {
		return nil
	}
	l.request.Camera = camera
	zoom := view.StyleZoomAt(camera.Zoom, l.request.Style.Options.Coarser)
	if zoom != l.request.Style.Options.Zoom {
		next := *l.request.Style
		if next.Epoch == math.MaxUint64 {
			return producer.ErrLimit
		}
		next.Epoch++
		next.Options.Zoom = zoom
		l.request.Style = &next
	}
	var err error
	l.revision, err = l.p.Submit(l.request)
	return err
}

func (l *liveSource) ready() bool {
	s := l.p.Status()
	return s.Revision == l.revision && s.Pending == 0 && s.Failed == 0 && l.bridge.Ready(l.revision)
}
