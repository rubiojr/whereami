package retained

import (
	"math"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func planner(t testing.TB, limits ResidencyLimits) *Planner {
	t.Helper()
	p, err := NewPlanner(limits)
	require.NoError(t, err)
	return p
}

func nextBatch(t testing.TB, p *Planner, budget Budget) *Batch {
	t.Helper()
	b, err := p.Next(budget)
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.LessOrEqual(t, len(b.Uploads)+len(b.Releases), budget.Resources)
	assert.LessOrEqual(t, b.Bytes, budget.Bytes)
	return b
}

func settle(t testing.TB, p *Planner, budget Budget) {
	t.Helper()
	for range 100 {
		b, err := p.Next(budget)
		require.NoError(t, err)
		if b == nil {
			return
		}
		require.NoError(t, p.Acknowledge(b.Ticket, true))
	}
	t.Fatal("planner did not settle")
}

func TestAcknowledgedActivationAndRetirement(t *testing.T) {
	s := newStore(t, Limits{})
	require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	first := snapshot(t, s, "a")
	p := planner(t, ResidencyLimits{})
	require.NoError(t, p.SetTarget(first))
	budget := Budget{Bytes: 84, Resources: 1}
	upload := nextBatch(t, p, budget)
	assert.Equal(t, uint64(84), upload.Bytes)
	assert.Equal(t, MeshResource, upload.Uploads[0].Version.Kind)
	assert.Nil(t, p.Current())
	assert.ErrorIs(t, p.SetTarget(nil), ErrBusy)
	_, err := p.Next(budget)
	assert.ErrorIs(t, err, ErrBusy)
	assert.ErrorIs(t, p.Acknowledge(upload.Ticket+1, true), ErrTicket)
	meshKey := upload.Uploads[0].Version
	upload.Uploads[0].Version.ID = 999 // descriptor isolation
	require.NoError(t, p.Acknowledge(upload.Ticket, true))
	assert.Contains(t, p.resident, meshKey)
	assert.Nil(t, p.Current(), "texture has not been acknowledged")
	texture := nextBatch(t, p, budget)
	assert.Equal(t, TextureResource, texture.Uploads[0].Version.Kind)
	require.NoError(t, p.Acknowledge(texture.Ticket, false))
	assert.Nil(t, p.Current())
	retry := nextBatch(t, p, budget)
	assert.Equal(t, texture.Uploads, retry.Uploads)
	assert.NotEqual(t, texture.Ticket, retry.Ticket)
	assert.ErrorIs(t, p.Acknowledge(texture.Ticket, true), ErrTicket)
	require.NoError(t, p.Acknowledge(retry.Ticket, true))
	assert.Same(t, first, p.Current())
	assert.ErrorIs(t, p.Acknowledge(retry.Ticket, true), ErrTicket)

	require.NoError(t, s.Apply([]Change{{"a", variant(2)}}))
	second := snapshot(t, s, "a")
	require.NoError(t, p.SetTarget(second))
	assert.Same(t, first, p.Current())
	replacement := nextBatch(t, p, Budget{Bytes: 88, Resources: 2})
	require.Len(t, replacement.Uploads, 2)
	assert.Empty(t, replacement.Releases)
	assert.Equal(t, first.Meshes[0].ID, replacement.Uploads[0].Version.ID)
	assert.Equal(t, uint64(2), replacement.Uploads[0].Version.Revision)
	require.NoError(t, p.Acknowledge(replacement.Ticket, true))
	assert.Same(t, second, p.Current())
	assert.Len(t, p.resident, 4, "old and new versions coexist until release acknowledgement")
	release := nextBatch(t, p, Budget{Bytes: 88, Resources: 2})
	require.Len(t, release.Releases, 2)
	assert.Equal(t, uint64(0), release.Bytes)
	require.NoError(t, p.Acknowledge(release.Ticket, false))
	assert.Len(t, p.resident, 4)
	release = nextBatch(t, p, Budget{Bytes: 88, Resources: 2})
	release.Releases[0].Revision = 99
	require.NoError(t, p.Acknowledge(release.Ticket, true))
	assert.Len(t, p.resident, 2)
	assert.Same(t, second, p.Current())
	settle(t, p, budget)
	require.NoError(t, p.SetTarget(nil))
	assert.Nil(t, p.Current())
	settle(t, p, budget)
	assert.Empty(t, p.resident)
	assert.Empty(t, p.residentOrder)
}

func TestSupersessionReleasesOnlyUnusedVersions(t *testing.T) {
	s := newStore(t, Limits{})
	require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	first := snapshot(t, s, "a")
	p := planner(t, ResidencyLimits{Bytes: 176, Resources: 4})
	budget := Budget{Bytes: 84, Resources: 1}
	require.NoError(t, p.SetTarget(first))
	settle(t, p, budget)
	require.NoError(t, s.Apply([]Change{{"a", variant(2)}}))
	second := snapshot(t, s, "a")
	require.NoError(t, p.SetTarget(second))
	partial := nextBatch(t, p, budget)
	require.NoError(t, p.Acknowledge(partial.Ticket, true))
	assert.Same(t, first, p.Current())
	require.NoError(t, s.Apply([]Change{{"a", variant(3)}}))
	third := snapshot(t, s, "a")
	require.NoError(t, p.SetTarget(third))
	obsolete := nextBatch(t, p, budget)
	assert.Empty(t, obsolete.Uploads)
	assert.Equal(t, []Version{partial.Uploads[0].Version}, obsolete.Releases)
	require.NoError(t, p.Acknowledge(obsolete.Ticket, true))
	assert.Same(t, first, p.Current())
	settle(t, p, budget)
	assert.Same(t, third, p.Current())
	assert.Len(t, p.resident, 2)
	for key := range p.resident {
		assert.Equal(t, uint64(3), key.Revision)
	}
	// A draw/instance-only snapshot uses existing versions and publishes at once.
	instance, err := s.Snapshot([]Range{{Key: "a", Count: 1, Transform: 7}})
	require.NoError(t, err)
	require.NoError(t, p.SetTarget(instance))
	assert.Same(t, instance, p.Current())
	batch, err := p.Next(budget)
	require.NoError(t, err)
	assert.Nil(t, batch)
}

func TestUploadAdmissionAndInvalidInput(t *testing.T) {
	s := newStore(t, Limits{})
	require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	first := snapshot(t, s, "a")
	for i, limits := range []ResidencyLimits{{Bytes: 175}, {Resources: 3}} {
		p := planner(t, limits)
		require.NoError(t, p.SetTarget(first))
		settle(t, p, Budget{Bytes: 88, Resources: 2})
		require.NoError(t, s.Apply([]Change{{"a", variant(byte(i + 1))}}))
		assert.ErrorIs(t, p.SetTarget(snapshot(t, s, "a")), ErrLimit)
		assert.Same(t, first, p.Current())
		assert.Same(t, first, p.target)
	}
	p := planner(t, ResidencyLimits{Bytes: 88, Resources: 2})
	require.NoError(t, p.SetTarget(first))
	for _, b := range []Budget{{Resources: 1}, {Bytes: 88}, {Bytes: 89, Resources: 1}, {Bytes: 88, Resources: 3}, {Bytes: 83, Resources: 1}} {
		batch, err := p.Next(b)
		assert.ErrorIs(t, err, ErrBudget)
		assert.Nil(t, batch)
		assert.Nil(t, p.pending)
	}
	settle(t, p, Budget{Bytes: 84, Resources: 2}) // byte budget splits mesh and texture
	tooSmall := planner(t, ResidencyLimits{Bytes: 87})
	assert.ErrorIs(t, tooSmall.SetTarget(first), ErrLimit)
	tooFew := planner(t, ResidencyLimits{Resources: 1})
	assert.ErrorIs(t, tooFew.SetTarget(first), ErrLimit)
	many := &scene.Scene{Meshes: make([]scene.Mesh, 2)}
	assert.ErrorIs(t, tooFew.SetTarget(many), ErrLimit)
	invalid := triangle()
	require.Error(t, p.SetTarget(invalid), "zero mesh revision")
	invalid.Meshes[0].Revision = 1
	assert.ErrorIs(t, p.SetTarget(invalid), ErrInput, "zero texture revision")
	invalid.Meshes[0].ID = 0
	require.Error(t, p.SetTarget(invalid), "invalid scene")
	for _, limits := range []ResidencyLimits{{Bytes: 1<<30 + 1}, {Resources: -1}, {Resources: 16385}} {
		_, err := NewPlanner(limits)
		assert.ErrorIs(t, err, ErrLimit)
	}
	var zero Planner
	assert.ErrorIs(t, zero.SetTarget(nil), ErrInput)
	_, err := zero.Next(Budget{})
	assert.ErrorIs(t, err, ErrInput)
	assert.ErrorIs(t, zero.Acknowledge(1, true), ErrInput)
}

func TestUploadTicketExhaustion(t *testing.T) {
	s := newStore(t, Limits{})
	require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	p := planner(t, ResidencyLimits{})
	require.NoError(t, p.SetTarget(snapshot(t, s, "a")))
	p.nextTicket = math.MaxUint64
	batch := nextBatch(t, p, Budget{Bytes: 84, Resources: 1})
	require.NoError(t, p.Acknowledge(batch.Ticket, true))
	_, err := p.Next(Budget{Bytes: 84, Resources: 1})
	assert.ErrorIs(t, err, ErrLimit)
	assert.Nil(t, p.Current())
	assert.Nil(t, p.pending)
}

func TestReadyEmptyScene(t *testing.T) {
	p := planner(t, ResidencyLimits{})
	empty := &scene.Scene{}
	require.NoError(t, p.SetTarget(empty))
	assert.Same(t, empty, p.Current())
	settle(t, p, Budget{Bytes: 88, Resources: 2})
}
