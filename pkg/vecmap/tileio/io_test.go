package tileio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundedHTTPAndCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "vecmap/1", r.UserAgent())
		assert.Equal(t, "application/x-protobuf", r.Header.Get("Accept"))
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(404)
		case "/empty":
			w.WriteHeader(204)
		case "/busy":
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
		case "/large":
			_, _ = w.Write(make([]byte, MaxBytes+1))
		case "/wait":
			close(started)
			<-r.Context().Done()
		default:
			_, _ = w.Write([]byte("bounded"))
		}
	}))
	defer server.Close()
	client := NewClient()
	assert.Equal(t, 15*time.Second, client.Timeout)
	data, err := Fetch(context.Background(), client, server.URL, "application/x-protobuf")
	require.NoError(t, err)
	assert.Equal(t, "bounded", string(data))
	_, err = Fetch(context.Background(), client, server.URL+"/missing", "application/x-protobuf")
	var status *StatusError
	require.ErrorAs(t, err, &status)
	assert.Equal(t, 404, status.Code)
	assert.Zero(t, status.RetryAfter)
	_, err = Fetch(context.Background(), client, server.URL+"/busy", "application/x-protobuf")
	require.ErrorAs(t, err, &status)
	assert.Equal(t, 429, status.Code)
	assert.Equal(t, time.Minute, status.RetryAfter)
	data, err = Fetch(context.Background(), client, server.URL+"/empty", "application/x-protobuf")
	require.NoError(t, err)
	assert.Empty(t, data)
	_, err = Fetch(context.Background(), client, server.URL+"/large", "application/x-protobuf")
	assert.ErrorIs(t, err, ErrLimit)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := Fetch(ctx, client, server.URL+"/wait", "application/x-protobuf"); done <- err }()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	_, err = Fetch(ctx, client, "://invalid", "application/x-protobuf")
	require.Error(t, err)
	_, err = Read(bytes.NewReader(make([]byte, MaxBytes+1)))
	assert.ErrorIs(t, err, ErrLimit)
	_, err = Read(failingReader{})
	require.ErrorContains(t, err, "reader failed")
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 0}, {"soon", 0}, {"-5", 0}, {"1.5", 0}, {"0", 0},
		{" 30 ", 30 * time.Second},
		{"86400", MaxRetryAfter}, {"99999999999999999999", 0}, {"18446744073709551615", MaxRetryAfter},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{now.Add(48 * time.Hour).Format(http.TimeFormat), MaxRetryAfter},
	} {
		assert.Equal(t, tc.want, RetryAfter(tc.value, now), "Retry-After %q", tc.value)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("reader failed") }

func TestRedirectPolicy(t *testing.T) {
	original, _ := http.NewRequest(http.MethodGet, "https://tiles.example/start", nil)
	for _, url := range []string{"https://other.example/tile", "http://tiles.example/tile"} {
		next, _ := http.NewRequest(http.MethodGet, url, nil)
		assert.Error(t, CheckRedirect(next, []*http.Request{original}))
	}
	next, _ := http.NewRequest(http.MethodGet, "https://tiles.example/end", nil)
	assert.NoError(t, CheckRedirect(next, []*http.Request{original}))
	assert.Error(t, CheckRedirect(next, nil))
	assert.Error(t, CheckRedirect(next, make([]*http.Request, 10)))
}

func TestCachePairsEvictionAndBounds(t *testing.T) {
	dir := t.TempDir()
	data := []byte("data")
	require.NoError(t, WritePair(dir, "pin.pbf", data, Checksum(data), "pin.pbf"))
	require.NoError(t, WritePair(dir, "old/tile.pbf", data, Checksum(data), "pin.pbf"))
	got, expected, err := ReadCached(filepath.Join(dir, "old/tile.pbf"), "")
	require.NoError(t, err)
	require.NoError(t, Verify(got, expected))
	assert.Error(t, Verify([]byte("bad"), expected))
	require.NoError(t, Enforce(dir, "pin.pbf", int64(len(data)+65)))
	_, err = os.Stat(filepath.Join(dir, "old/tile.pbf"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, _, err = ReadCached(filepath.Join(dir, "pin.pbf"), Checksum(data))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pin.pbf.sha256"), []byte("not a digest"), 0600))
	_, _, err = ReadCached(filepath.Join(dir, "pin.pbf"), "")
	require.Error(t, err)
	_, _, err = ReadCached(filepath.Join(dir, "absent"), "")
	require.Error(t, err)
	assert.NoError(t, Enforce("", "", 1))
	assert.NoError(t, Enforce(dir, "", 0))
	assert.Error(t, WriteAtomic(dir, "../escape", data))
	assert.Error(t, WriteAtomic(dir, filepath.Join(dir, "absolute"), data))
	assert.ErrorIs(t, WritePair(dir, "large", make([]byte, MaxBytes+1), "", ""), ErrLimit)
	assert.Error(t, WritePair(dir, "bad", data, "", ""))
	assert.Error(t, WritePair(dir, "bad", data, strings.Repeat("z", 64), ""))
	// Real cardinality limit: tiny responses cannot grow an unbounded scan map.
	large := t.TempDir()
	for i := 0; i < MaxCacheFiles; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(large, fmt.Sprintf("%05d", i)), nil, 0600))
	}
	assert.ErrorIs(t, Enforce(large, "", DefaultCacheBytes), ErrLimit)
	assert.ErrorIs(t, WritePair(large, "new", data, Checksum(data), ""), ErrLimit)
	_, err = os.Stat(filepath.Join(large, "new"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestPinnedSource(t *testing.T) {
	pin := OpenFreeMap(view.TileID{Z: 9, X: 250, Y: 193})
	assert.Equal(t, PinnedSHA256, pin.SHA256)
	assert.Equal(t, PinnedCacheName, pin.CacheName)
	assert.True(t, strings.HasSuffix(pin.URL, "/9/250/193.pbf"))
	other := OpenFreeMap(view.TileID{Z: 3, X: 2, Y: 1})
	assert.Empty(t, other.SHA256)
	assert.Equal(t, filepath.Join("openfreemap-"+OpenFreeMapSnapshot, "3", "2", "1.pbf"), other.CacheName)
}
