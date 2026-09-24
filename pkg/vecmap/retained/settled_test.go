package retained

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettlementFollowsAcknowledgedRetirement(t *testing.T) {
	w, err := NewSettlingWorkerWithData[struct{}](ResidencyLimits{}, Budget{Bytes: 84, Resources: 1})
	require.NoError(t, err)
	t.Cleanup(func() { w.Close(); <-w.Done() })
	store := newStore(t, Limits{})
	require.NoError(t, store.Apply([]Change{{"a", triangle()}}))
	first := snapshot(t, store, "a")
	gen, err := w.Restart(first)
	require.NoError(t, err)
	settle := func() {
		for {
			p := workerPacket(t, w, gen)
			require.NoError(t, p.Err)
			workerAckPacket(t, w, p, true)
			if p.Settled {
				assert.Nil(t, p.Batch)
				return
			}
		}
	}
	settle()
	require.NoError(t, store.Apply([]Change{{"a", triangle()}}))
	second := snapshot(t, store, "a")
	require.True(t, w.SetTarget(gen, second))
	for range 2 {
		p := workerPacket(t, w, gen)
		require.NotNil(t, p.Batch)
		assert.False(t, p.Settled)
		workerAckPacket(t, w, p, true)
	}
	release := workerPacket(t, w, gen)
	assert.Same(t, second, release.Current)
	require.Len(t, release.Batch.Releases, 1)
	assert.False(t, release.Settled)
	workerAckPacket(t, w, release, false)
	retry := workerPacket(t, w, gen)
	assert.False(t, retry.Settled)
	assert.Equal(t, release.Batch.Releases, retry.Batch.Releases)
	workerAckPacket(t, w, retry, true)
	release = workerPacket(t, w, gen)
	assert.False(t, release.Settled)
	workerAckPacket(t, w, release, true)
	p := workerPacket(t, w, gen)
	assert.True(t, p.Settled)
	assert.Nil(t, p.Batch)
	assert.Same(t, second, p.Current)
	// Even settlement requires acknowledgement; a reset can cancel that wait.
	newGen, err := w.Restart(nil)
	require.NoError(t, err)
	assert.Greater(t, newGen, gen)
	p = workerPacket(t, w, newGen)
	assert.True(t, p.Settled)
	assert.Nil(t, p.Current)
}
