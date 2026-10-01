//go:build vecmap_rhi

package main

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rubiojr/whereami/internal/vecmaprhi"
	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamCompletionFailureStopsSameContextRecreation(t *testing.T) {
	for _, err := range []error{vecmaprhi.ErrNativeCompletion, vecmaprhi.ErrUnsupportedCompletion, errors.New("ordinary reset")} {
		t.Run(err.Error(), func(t *testing.T) {
			epoch := &nativeEpoch{generation: 1, pending: &retained.PacketWithData[*scene.Document]{}}
			stream := &viewerStream{epoch: epoch, report: func(streamStatus) {}, status: streamStatus{Ready: true}}
			stream.complete(epoch, vecmaprhi.BatchResult{Reset: true, Err: fmt.Errorf("test: %w", err)})
			assert.True(t, epoch.dead)
			assert.Nil(t, epoch.pending)
			assert.False(t, stream.status.Ready)
			if errors.Is(err, vecmaprhi.ErrNativeCompletion) || errors.Is(err, vecmaprhi.ErrUnsupportedCompletion) {
				assert.ErrorIs(t, stream.status.Err, err)
			} else {
				assert.NoError(t, stream.status.Err)
			}
		})
	}
}

func TestViewerBudgetPreflight(t *testing.T) {
	// Reject before constructing QApplication or starting the worker.
	assert.ErrorIs(t, display(scene.Document{}, benchmarkOptions{}), retained.ErrBudget)
}

func TestStreamResultGuards(t *testing.T) {
	worker, err := retained.NewWorkerWithData[*scene.Document](retained.ResidencyLimits{}, retained.Budget{Bytes: 100, Resources: 1})
	require.NoError(t, err)
	worker.Close()
	<-worker.Done()
	current := &nativeEpoch{generation: 2}
	stream := &viewerStream{worker: worker, epoch: current, report: func(streamStatus) {}}
	stream.complete(&nativeEpoch{generation: 1}, vecmaprhi.BatchResult{Reset: true})
	assert.False(t, current.dead)
	assert.NoError(t, stream.status.Err)
	stream.complete(current, vecmaprhi.BatchResult{Ticket: 123, Success: true})
	assert.True(t, current.dead)
	assert.ErrorContains(t, stream.status.Err, "does not match")
	current.dead = false
	current.pending = &retained.PacketWithData[*scene.Document]{Generation: 2, Sequence: 1, Batch: &retained.Batch{Ticket: 4}}
	stream.complete(current, vecmaprhi.BatchResult{Ticket: 4, Success: false})
	assert.True(t, current.dead)
	assert.Equal(t, uint64(1), stream.status.BatchFailures)
	assert.ErrorContains(t, stream.status.Err, "mailbox unavailable")
	assert.Nil(t, stream.sync(nil, nil, streamUpdate{}))
	assert.ErrorIs(t, stream.status.Err, retained.ErrClosed)
}

func TestFrameSamplesNativeReset(t *testing.T) {
	samples := frameSamples{latest: vecmaprhi.Stats{Frames: 42}, lastFrame: time.Now()}
	samples.add(vecmaprhi.Stats{})
	assert.True(t, samples.lastFrame.IsZero())
	samples.add(vecmaprhi.Stats{Frames: 1})
	assert.Empty(t, samples.cadence)
}
