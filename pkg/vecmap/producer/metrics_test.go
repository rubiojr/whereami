package producer

import (
	"bytes"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompactCacheAdmissionAndSourceRollback(t *testing.T) {
	limits := DefaultLimits()
	limits.Workers = 1
	limits.CacheBytes = 4096
	p, c := newControlled(t, limits)
	r := testRequest(t, view.TileID{})
	originalStyle := r.Style
	_, err := p.Submit(r)
	require.NoError(t, err)
	job := nextCall(t, c)
	waitStatus(t, p, func(s Status) bool { return s.ReservedRawBytes == uint64(limits.RawBytes) })
	data := tilePBF()
	job.reply <- answer{data: data}
	first := nextLease(t, p, func(l *Lease) bool { return len(l.Snapshot.Cover) == 1 })
	require.True(t, p.Current(1, 1, first))
	before := waitStatus(t, p, func(s Status) bool { return s.Builds == 1 && s.Jobs == 0 })
	assert.Equal(t, 2*uint64(len(data)), before.Cache.Raw)
	assert.Equal(t, before.Cache.Total(), before.CacheBytes)
	assert.Equal(t, before.PeakCache.Total(), before.PeakCacheBytes)
	assert.Equal(t, uint64(len(data)), before.ResponseBytes)
	assert.Equal(t, uint64(limits.RawBytes), before.RawCapacityBytes)
	assert.Equal(t, before.Loads, before.Loading.Count)
	assert.Equal(t, before.Prepares, before.Preparing.Count)
	assert.Equal(t, before.Builds, before.Building.Count)
	assert.Greater(t, before.Selecting.Count, uint64(0))
	assert.GreaterOrEqual(t, before.Building.Total, before.Building.Maximum)
	// Replacement cannot erase the old raw/prepared data until compact raw
	// admission succeeds. Charge the old visible fragment and its profiles too.
	changed := *r.Style
	changed.Source = "replacement"
	changed.Epoch++
	r.Style = &changed
	_, err = p.Submit(r)
	require.NoError(t, err)
	tooLarge := int((limits.CacheBytes-before.Cache.Fragments-before.Cache.Profiles)/2) + 1
	job = nextCall(t, c)
	job.reply <- answer{data: bytes.Repeat([]byte{0xff}, tooLarge)}
	failed := waitStatus(t, p, func(s Status) bool { return s.Failed == 1 && s.LastErrorStage == "cache/raw" })
	assert.Equal(t, before.Cache, failed.Cache, "failed raw replacement is atomic")
	assert.Equal(t, before.Prepares, failed.Prepares)
	assert.Equal(t, []byte{255, 0, 0, 255}, byteColor(first))
	r.Style = originalStyle
	revision, err := p.Submit(r)
	require.NoError(t, err)
	nextLease(t, p, func(l *Lease) bool { return l.Revision == revision })
	recovered := waitStatus(t, p, func(s Status) bool { return s.Builds == 2 })
	assert.Equal(t, uint64(2), recovered.Loads, "reverting source reuses the original valid cached bytes")
	assert.LessOrEqual(t, recovered.PeakCacheBytes, limits.CacheBytes)
}

func TestResponseCompactionOwnership(t *testing.T) {
	full := []byte{1, 2, 3}
	assert.Same(t, &full[0], &compactResponse(full)[0], "full-sized buffers transfer without a copy")
	spare := make([]byte, 3, 1024)
	copy(spare, full)
	compact := compactResponse(spare)
	assert.Equal(t, full, compact)
	assert.Equal(t, len(compact), cap(compact))
	clear(spare)
	assert.Equal(t, full, compact)
	assert.Zero(t, cap(compactResponse(make([]byte, 0, 1024))))
}
