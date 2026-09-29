package producer

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/tileio"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPSourceCacheAndClassification(t *testing.T) {
	var hits atomic.Int32
	var status atomic.Int32
	data := tilePBF()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if code := status.Load(); code != 0 {
			w.WriteHeader(int(code))
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	template := server.URL + "/{z}/{x}/{y}.pbf"
	dir := t.TempDir()
	load, err := HTTPLoader(nil, dir, template)
	require.NoError(t, err)
	key := Key{Source: template, Tile: view.TileID{}}
	var out bytes.Buffer
	offline, err := CacheLoader(dir, template)
	require.NoError(t, err)
	assert.ErrorIs(t, offline(context.Background(), key, &out), ErrMissing)
	assert.Zero(t, hits.Load())
	require.NoError(t, load(context.Background(), key, &out))
	assert.Equal(t, data, out.Bytes())
	out.Reset()
	require.NoError(t, load(context.Background(), key, &out))
	assert.Equal(t, int32(1), hits.Load())
	out.Reset()
	require.NoError(t, offline(context.Background(), key, &out))
	assert.Equal(t, data, out.Bytes())
	assert.Equal(t, int32(1), hits.Load())
	path := filepath.Join(dir, "source-"+tileio.Checksum([]byte(template)), "0", "0", "0.pbf")
	require.NoError(t, os.WriteFile(path, []byte("corrupt"), 0600))
	out.Reset()
	assert.ErrorIs(t, offline(context.Background(), key, &out), ErrMissing)
	assert.Equal(t, int32(1), hits.Load())
	out.Reset()
	require.NoError(t, load(context.Background(), key, &out))
	assert.Equal(t, int32(2), hits.Load())
	// An empty tile answered with 204 is a successful zero-byte response, cached
	// like any other.
	status.Store(204)
	emptyKey := Key{Source: template, Tile: view.TileID{Z: 1}}
	out.Reset()
	require.NoError(t, load(context.Background(), emptyKey, &out))
	assert.Zero(t, out.Len())
	assert.Equal(t, int32(3), hits.Load())
	require.NoError(t, offline(context.Background(), emptyKey, &out))
	assert.Zero(t, out.Len())
	status.Store(0)
	uncached, err := HTTPLoader(nil, "", template)
	require.NoError(t, err)
	for _, tc := range []struct {
		code int32
		want error
	}{{404, ErrMissing}, {403, ErrPermanent}, {429, nil}, {503, nil}} {
		status.Store(tc.code)
		err := uncached(context.Background(), key, &out)
		require.Error(t, err)
		if tc.want != nil {
			assert.ErrorIs(t, err, tc.want)
		} else {
			assert.True(t, retryable(err))
		}
	}
	status.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, load(ctx, key, &out), context.Canceled)
	assert.ErrorIs(t, load(context.Background(), Key{Source: "other"}, &out), ErrPermanent)
	_, err = loadHTTP(context.Background(), server.Client(), "", tileio.Source{URL: server.URL, SHA256: "wrong"})
	assert.ErrorIs(t, err, ErrPermanent)
	for _, bad := range []string{"", "file:///x/{z}/{x}/{y}", server.URL + "/no-placeholders"} {
		_, err := HTTPLoader(nil, "", bad)
		assert.ErrorIs(t, err, ErrInput)
	}
	_, err = CacheLoader("", template)
	assert.ErrorIs(t, err, ErrInput)
}

func TestHTTPLoaderReportsABusySource(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	template := server.URL + "/{z}/{x}/{y}.pbf"
	load, err := HTTPLoader(nil, "", template)
	require.NoError(t, err)
	for _, code := range []int32{429, 503} {
		status.Store(code)
		asked := time.Now()
		err := load(context.Background(), Key{Source: template, Tile: view.TileID{}}, &bytes.Buffer{})
		var busy *BusyError
		require.ErrorAs(t, err, &busy, "status %d", code)
		assert.WithinDuration(t, asked.Add(time.Minute), busy.Until, 5*time.Second)
		assert.Contains(t, err.Error(), "source busy until")
	}
	// Other refusals keep their classification even with the header.
	status.Store(403)
	assert.ErrorIs(t, load(context.Background(), Key{Source: template, Tile: view.TileID{}}, &bytes.Buffer{}), ErrPermanent)
}
