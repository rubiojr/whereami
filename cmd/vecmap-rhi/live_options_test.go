//go:build vecmap_rhi

package main

import (
	"context"
	"io"
	"math"
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/producer"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveOptionsRejectBeforeWindowCreation(t *testing.T) {
	_, err := newLiveSource(liveOptions{}, retained.Budget{})
	assert.ErrorContains(t, err, "glyph-dir")
	_, err = newLiveSource(liveOptions{glyphs: "supplied", latitude: math.NaN()}, retained.Budget{})
	assert.ErrorIs(t, err, producer.ErrInput)
	_, err = newLiveSource(liveOptions{glyphs: "supplied", template: "bad"}, retained.Budget{})
	assert.ErrorIs(t, err, producer.ErrInput)
	assert.Error(t, run("scene.json", benchmarkOptions{liveOptions: liveOptions{enabled: true}}))
}

func TestFrozenLiveTraceAndSettlementDeadline(t *testing.T) {
	p, err := producer.New(func(context.Context, producer.Key, io.Writer) error { return producer.ErrMissing }, producer.DefaultLimits())
	require.NoError(t, err)
	camera := view.NewCamera(view.Coordinate{}, 2, 0, 256, 256)
	initial := &scene.Document{Width: 256, Height: 256, Camera: &camera, TileSpaces: []scene.TileSpace{{}}}
	b, err := producer.NewBridge(p, initial, retained.ResidencyLimits{}, retained.Budget{Bytes: 1024, Resources: 1})
	require.NoError(t, err)
	defer b.Close()
	l := &liveSource{p: p, bridge: b, initial: initial, request: producer.Request{Camera: camera, Targets: []view.TileID{},
		Style: &producer.Style{Source: "test", Epoch: 1, Bytes: 1, Options: tiles.PrepareOptions{Zoom: 2}}, Assets: &producer.Assets{Epoch: 1, Bytes: 1}}}
	opts := benchmarkOptions{live: l, duration: time.Second, animate: true}
	update, err := opts.sampleUpdate(*initial, initial, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, traceCamera(*initial, 1, true), update.camera, "settlement tail freezes the trace")
	before := l.revision
	_, err = opts.sampleUpdate(*initial, initial, 3*time.Second)
	require.NoError(t, err)
	assert.Equal(t, before, l.revision)
	done, err := opts.finish(time.Second, streamStatus{}, initial)
	assert.False(t, done)
	assert.NoError(t, err)
	done, err = opts.finish(11*time.Second, streamStatus{}, initial)
	assert.True(t, done)
	assert.ErrorContains(t, err, "did not settle")
	_, err = opts.sampleUpdate(scene.Document{}, initial, time.Second)
	assert.ErrorIs(t, err, producer.ErrInput)
	static := benchmarkOptions{duration: time.Second}
	done, err = static.finish(0, streamStatus{}, initial)
	assert.False(t, done)
	assert.NoError(t, err)
	done, err = static.finish(time.Second, streamStatus{}, initial)
	assert.True(t, done)
	assert.ErrorContains(t, err, "did not become ready")
	done, err = static.finish(time.Second, streamStatus{Ready: true, Current: initial}, initial)
	assert.True(t, done)
	assert.NoError(t, err)
}
