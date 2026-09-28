package retained

import (
	"runtime"
	"testing"
	"time"
	"weak"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type frameMapping struct {
	transforms []scene.Affine
}

func TestWorkerSceneData(t *testing.T) {
	store := newStore(t, Limits{})
	var generation byte
	newScene := func() *scene.Scene {
		generation++
		require.NoError(t, store.Apply([]Change{{"a", variant(generation)}}))
		return snapshot(t, store, "a")
	}
	first, second, third := newScene(), newScene(), newScene()
	mappingA := &frameMapping{transforms: []scene.Affine{{DX: 10}}}
	mappingB := &frameMapping{transforms: []scene.Affine{{DX: 20}, {DX: 30}}}
	mappingC := &frameMapping{transforms: []scene.Affine{{DX: 40}}}
	w, err := NewWorkerWithData[*frameMapping](ResidencyLimits{}, Budget{Bytes: 84, Resources: 1})
	require.NoError(t, err)
	t.Cleanup(func() { w.Close(); <-w.Done() })
	gen, err := w.RestartWithData(first, mappingA)
	require.NoError(t, err)
	next := func() PacketWithData[*frameMapping] {
		t.Helper()
		var p PacketWithData[*frameMapping]
		require.Eventually(t, func() bool {
			var ok bool
			p, ok = w.Next()
			return ok && p.Generation == gen
		}, 5*time.Second, time.Millisecond)
		return p
	}
	ack := func(p PacketWithData[*frameMapping], success bool) {
		t.Helper()
		require.True(t, w.Acknowledge(p.Generation, p.Sequence, success))
	}
	for range 2 {
		p := next()
		assert.Nil(t, p.Current)
		assert.Nil(t, p.CurrentData, "unready target mapping must not escape")
		assert.Same(t, mappingA, p.TargetData)
		ack(p, true)
	}
	p := next()
	assert.Same(t, first, p.Current)
	assert.Same(t, mappingA, p.CurrentData)
	ack(p, true)
	require.True(t, w.SetTargetWithData(gen, second, mappingB))
	p = next()
	assert.Same(t, first, p.Current)
	assert.Same(t, mappingA, p.CurrentData)
	ack(p, false)
	p = next()
	assert.Same(t, mappingA, p.CurrentData, "retry retains old mapping")
	ack(p, true)
	p = next() // final upload of second
	assert.Same(t, mappingA, p.CurrentData)
	// The final ack promotes second before a queued replacement is applied. Its
	// mapping must survive even if no standalone publication packet was issued.
	require.True(t, w.SetTargetWithData(gen, third, mappingC))
	ack(p, true)
	for {
		p = next()
		if p.Current == third {
			break
		}
		assert.Same(t, second, p.Current)
		assert.Same(t, mappingB, p.CurrentData)
		assert.Same(t, mappingC, p.TargetData)
		require.NotNil(t, p.Batch)
		ack(p, true)
	}
	assert.Same(t, mappingC, p.CurrentData)
	stale := make(map[Version]bool)
	for _, version := range append(sceneVersions(first), sceneVersions(second)...) {
		stale[version] = true
	}
	for p.Batch != nil { // complete retirement before the data-only target
		ack(p, true)
		for _, version := range p.Batch.Releases {
			require.True(t, stale[version])
			delete(stale, version)
		}
		if len(stale) == 0 {
			break
		}
		p = next()
	}
	// A same-scene mapping change must publish, despite Current pointer equality.
	require.True(t, w.SetTargetWithData(gen, third, mappingB))
	p = next()
	assert.Same(t, third, p.Current)
	assert.Same(t, mappingB, p.CurrentData)
	assert.Nil(t, p.Batch)
	ack(p, true)
	require.True(t, w.SetTargetWithData(gen, &scene.Scene{Meshes: []scene.Mesh{{ID: 0}}}, mappingA))
	p = next()
	require.Error(t, p.Err)
	assert.Same(t, third, p.Current)
	assert.Same(t, mappingB, p.CurrentData, "rejection must not replace current mapping")
	assert.Same(t, mappingB, p.TargetData, "rejection retains the accepted target")
	ack(p, false)
	require.True(t, w.SetTargetWithData(gen, nil, mappingA))
	p = next()
	assert.Nil(t, p.Current)
	assert.Nil(t, p.CurrentData, "clear drops data even if caller supplied it")
	assert.Nil(t, p.TargetData)
	// Reset need not wait for the outstanding retirement acknowledgement.
	gen, err = w.RestartWithData(first, mappingA)
	require.NoError(t, err)
	ack(p, true) // stale-generation result
	p = next()
	assert.Nil(t, p.CurrentData)
	assert.False(t, w.SetTargetWithData(gen-1, third, mappingC))
}

func TestWorkerDataSupersession(t *testing.T) {
	// Drive the same state machine deterministically to prove that abandoning a
	// partially uploaded target releases its metadata rather than growing a map
	// keyed by every historical scene pointer.
	w := &WorkerWithData[*frameMapping]{limits: ResidencyLimits{}, packets: make(chan PacketWithData[*frameMapping], 1), acks: make(chan workerAck, 1)}
	s := workerState[*frameMapping]{}
	store := newStore(t, Limits{})
	require.NoError(t, store.Apply([]Change{{"a", triangle()}}))
	first := snapshot(t, store, "a")
	probe := func() weak.Pointer[frameMapping] {
		mapping := &frameMapping{transforms: make([]scene.Affine, 2)}
		s.target(w, newWorkerTarget(1, first, mapping))
		require.True(t, s.advance(Budget{Bytes: 84, Resources: 1}))
		s.waiting, s.out = s.out, nil
		s.acknowledge(workerAck{generation: 1, sequence: 1, success: true})
		return weak.Make(mapping)
	}()
	require.NoError(t, store.Apply([]Change{{"a", triangle()}}))
	s.target(w, newWorkerTarget(1, snapshot(t, store, "a"), &frameMapping{}))
	require.True(t, s.advance(Budget{Bytes: 84, Resources: 1}))
	assert.Nil(t, s.out.CurrentData)
	require.Eventually(t, func() bool { runtime.GC(); return probe.Value() == nil }, 5*time.Second, time.Millisecond)
	runtime.KeepAlive(s)
}

func TestWorkerSupersessionDropsUploadedPayloadBeforeRetirement(t *testing.T) {
	w := &WorkerWithData[*frameMapping]{limits: ResidencyLimits{}, packets: make(chan PacketWithData[*frameMapping], 1), acks: make(chan workerAck, 1)}
	s := workerState[*frameMapping]{}
	budget := Budget{Bytes: 84, Resources: 1}
	probe := func() weak.Pointer[scene.Vertex] {
		first := triangle()
		first.Meshes[0].Revision, first.Textures[0].Revision = 1, 1
		probe := weak.Make(&first.Meshes[0].Vertices[0])
		s.target(w, newWorkerTarget(1, first, &frameMapping{}))
		require.True(t, s.advance(budget))
		s.waiting, s.out = s.out, nil
		s.acknowledge(workerAck{generation: 1, sequence: 1, success: true})
		return probe
	}()
	s.target(w, newWorkerTarget[*frameMapping](1, nil, nil))
	require.True(t, s.advance(budget))
	require.NotEmpty(t, s.out.Batch.Releases)
	assert.Len(t, s.planner.resident, 1, "native capacity remains charged until release ack")
	require.Eventually(t, func() bool { runtime.GC(); return probe.Value() == nil }, 5*time.Second, time.Millisecond)
	// Failed retirement still retains the native charge, but no abandoned payload.
	s.waiting, s.out = s.out, nil
	s.acknowledge(workerAck{generation: 1, sequence: 2, success: false})
	require.True(t, s.advance(budget))
	assert.Len(t, s.planner.resident, 1)
	require.NotEmpty(t, s.out.Batch.Releases)
	runtime.KeepAlive(s)
}
