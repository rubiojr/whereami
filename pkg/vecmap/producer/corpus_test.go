package producer

import (
	"math"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/compiler"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/tileio"
	"github.com/rubiojr/whereami/pkg/vecmap/tiles"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Use the captured immutable workload with a checked fake backend. This isolates
// CPU admission/lease progress from GPU timing without weakening upload budgets.
func TestCapturedMadridAdmission(t *testing.T) {
	dir, fontsDir := os.Getenv("WHEREAMI_VECTOR_WORKLOAD_CACHE"), os.Getenv("WHEREAMI_VECTOR_GLYPH_FIXTURE_DIR")
	if dir == "" || fontsDir == "" {
		t.Skip("set captured workload cache and supplied glyph directory")
	}
	load, err := CacheLoader(dir, tileio.OpenFreeMapTemplate)
	require.NoError(t, err)
	layers, err := liberty.Layers()
	require.NoError(t, err)
	ranges := make(map[string][]byte)
	for _, name := range []string{"Noto Sans Regular", "Noto Sans Bold", "Noto Sans Italic"} {
		ranges[name], err = tileio.ReadFile(filepath.Join(fontsDir, url.PathEscape(name)+".pbf"))
		require.NoError(t, err)
	}
	fonts, err := compiler.DecodeFontRanges(ranges, 0)
	require.NoError(t, err)
	limits := DefaultLimits()
	p, err := New(load, limits)
	require.NoError(t, err)
	camera := view.NewCamera(view.Coordinate{Latitude: 40.4168, Longitude: -3.7038}, 10, 0, 800, 600)
	initial := &scene.Document{Width: 800, Height: 600, Camera: &camera, TileSpaces: []scene.TileSpace{{}}}
	b, err := NewBridge(p, initial, retained.ResidencyLimits{}, retained.Budget{Bytes: 32 << 20, Resources: 2})
	require.NoError(t, err)
	defer b.Close()
	w := b.Worker()
	generation, err := w.RestartWithData(&initial.Scene, initial)
	require.NoError(t, err)
	require.True(t, b.Restarted(generation))
	r := Request{Camera: camera, Style: &Style{Source: tileio.OpenFreeMapTemplate, Epoch: 1, Layers: layers, Options: tiles.PrepareOptions{Zoom: 10, Indexed: true}, Bytes: 4 << 20},
		Assets: &Assets{Epoch: 1, Bytes: 60 << 20, Value: tiles.Assets{Fonts: fonts, Sprite: liberty.Sprite, SpriteEntry: liberty.SpriteEntry, FallbackEligible: glyph.LegacyFallbackEligible}}}
	resident := make(map[retained.Version]bool)
	var current, target *scene.Document
	for _, seconds := range []float64{0, 5} {
		c := camera
		c.Zoom += 0.2 * math.Sin(seconds)
		c.Bearing += 0.15 * math.Sin(seconds*0.7) * 180 / math.Pi
		c = c.Normalized().Panned(-math.Sin(seconds*1.3)*20, 0)
		r.Camera = c
		next := *r.Style
		next.Epoch++
		next.Options.Zoom = view.StyleZoom(c.Zoom)
		r.Style = &next
		revision, err := p.Submit(r)
		require.NoError(t, err)
		deadline := time.Now().Add(20 * time.Second)
		settled := false
		for time.Now().Before(deadline) {
			if desired := b.Target(); desired != target {
				target = desired
				require.True(t, w.SetTargetWithData(generation, &target.Scene, target))
			}
			if packet, ok := w.Next(); ok {
				require.NoError(t, packet.Err)
				require.True(t, b.Consumed(packet))
				current = packet.CurrentData
				active := make(map[retained.Version]bool)
				if packet.Current != nil {
					for _, m := range packet.Current.Meshes {
						active[retained.Version{Kind: retained.MeshResource, ID: m.ID, Revision: m.Revision}] = true
					}
					for _, tex := range packet.Current.Textures {
						active[retained.Version{Kind: retained.TextureResource, ID: tex.ID, Revision: tex.Revision}] = true
					}
				}
				for version := range active {
					require.True(t, resident[version], "Current must be resident")
				}
				if packet.Batch != nil {
					assert.LessOrEqual(t, packet.Batch.Bytes, uint64(32<<20))
					for _, resource := range packet.Batch.Uploads {
						resident[resource.Version] = true
					}
					for _, version := range packet.Batch.Releases {
						require.False(t, active[version])
						require.True(t, resident[version])
						delete(resident, version)
					}
				}
				require.True(t, w.Acknowledge(packet.Generation, packet.Sequence, true))
				require.True(t, b.Completed(packet, true))
			}
			status := p.Status()
			if status.Revision == revision && status.Pending == 0 && status.Failed == 0 && b.Ready(revision) && current == b.Target() {
				settled = true
				break
			}
			time.Sleep(time.Millisecond)
		}
		require.True(t, settled, "t=%g status=%+v", seconds, p.Status())
		s := p.Status()
		assert.Equal(t, len(view.VisibleTileCover(c)), s.SelectedTiles)
		assert.Zero(t, s.Fallbacks)
		if seconds == 0 {
			assert.Equal(t, 130, current.Labels)
		} else {
			assert.Equal(t, 95, current.Labels)
		}
		assert.LessOrEqual(t, s.PeakCacheBytes, limits.CacheBytes)
		assert.LessOrEqual(t, s.PeakLeases, limits.Leases)
		t.Logf("t=%g selected=%d cache=%d peak=%d prepares=%d evictions=%d labels=%d", seconds, s.SelectedTiles, s.CacheBytes, s.PeakCacheBytes, s.Prepares, s.PreparationEvictions, current.Labels)
	}
}
