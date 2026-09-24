package producer

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/fixture"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinnedLibertyProducer(t *testing.T) {
	path, dir := os.Getenv("WHEREAMI_VECTOR_TILE_FIXTURE"), os.Getenv("WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	if path == "" || dir == "" {
		t.Skip("set pinned tile and glyph fixture environment variables")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	layers, err := liberty.Layers()
	require.NoError(t, err)
	ranges := make(map[string][]byte)
	for _, name := range []string{"Noto Sans Regular", "Noto Sans Bold", "Noto Sans Italic"} {
		ranges[name], err = os.ReadFile(filepath.Join(dir, url.PathEscape(name)+".pbf"))
		require.NoError(t, err)
	}
	fonts, err := compiler.DecodeFontRanges(ranges, 0)
	require.NoError(t, err)
	reference, err := fixture.Compile(data, ranges, fixture.Options{DirectIndexed: true})
	require.NoError(t, err)
	p, err := New(func(_ context.Context, key Key, writer io.Writer) error {
		if key.Tile != fixture.Tile() {
			return ErrMissing
		}
		_, err := writer.Write(data)
		return err
	}, DefaultLimits())
	require.NoError(t, err)
	t.Cleanup(func() { p.Close(); waitDone(t, p.Done()) })
	_, err = p.Submit(Request{Camera: reference.Camera, Targets: []view.TileID{fixture.Tile()},
		Style:  &Style{Source: "pinned-liberty", Epoch: 1, Layers: layers, Options: tiles.PrepareOptions{Zoom: 10, Indexed: true}, Bytes: 4 << 20},
		Assets: &Assets{Epoch: 1, Bytes: 60 << 20, Value: tiles.Assets{Fonts: fonts, Sprite: liberty.Sprite, SpriteEntry: liberty.SpriteEntry, FallbackEligible: glyph.LegacyFallbackEligible}},
	})
	require.NoError(t, err)
	l := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 && l.Snapshot.Cover[0] == fixture.Tile() })
	require.NoError(t, l.Snapshot.Scene.Validate())
	labels, elements := 0, uint64(0)
	for _, draw := range l.Snapshot.Scene.Draws {
		if draw.Material.Kind == scene.SDFFill {
			labels++
		}
		elements += uint64(draw.Count)
	}
	assert.Equal(t, reference.Labels, labels)
	assert.Equal(t, 62, labels)
	assert.Equal(t, uint64(782409), elements)
	t.Logf("draws=%d labels=%d selected=%d snapshot_charge=%d cache_charge=%d", len(l.Snapshot.Scene.Draws), labels, elements, l.Snapshot.RetainedBytes(), p.Status().CacheBytes)
}
