package retained

import (
	"math"
	"strings"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func triangle() *scene.Scene {
	return &scene.Scene{
		Meshes:   []scene.Mesh{{ID: 1, Vertices: []scene.Vertex{{}, {X: 1}, {Y: 1}}, Indices: []uint32{0, 1, 2}}},
		Textures: []scene.Texture{{ID: 1, Width: 1, Height: 1, RGBA: []byte{1, 2, 3, 255}}},
		Draws:    []scene.Draw{{Mesh: 1, Count: 3, Material: scene.Material{Kind: scene.Image, Texture: 1, Color: [4]float32{1, 1, 1, 1}}, Clip: [4]float32{0, 0, 256, 256}}},
	}
}

func newStore(t testing.TB, limits Limits) *Store {
	t.Helper()
	s, err := New(limits)
	require.NoError(t, err)
	return s
}

func snapshot(t testing.TB, s *Store, keys ...string) *scene.Scene {
	t.Helper()
	order := make([]Range, len(keys))
	for i, key := range keys {
		order[i] = Range{Key: key, Count: 1, Transform: i}
	}
	result, err := s.Snapshot(order)
	require.NoError(t, err)
	require.NoError(t, result.Validate())
	return result
}

func TestIdentityLifecycleAndIsolation(t *testing.T) {
	s := newStore(t, Limits{})
	a, b := triangle(), triangle()
	require.NoError(t, s.Apply([]Change{{"a", a}, {"b", b}}))
	first := snapshot(t, s, "a", "b")
	assert.NotEqual(t, first.Meshes[0].ID, first.Meshes[1].ID)
	assert.NotEqual(t, first.Textures[0].ID, first.Textures[1].ID)
	assert.Equal(t, uint64(1), first.Meshes[0].Revision)
	assert.Same(t, &a.Meshes[0].Vertices[0], &first.Meshes[0].Vertices[0])
	assert.Same(t, &a.Textures[0].RGBA[0], &first.Textures[0].RGBA[0])
	assert.Equal(t, uint64(1), a.Meshes[0].ID, "input metadata must not be remapped in place")
	a.Draws[0].Clip = [4]float32{}
	a.Meshes[0].ID = 99
	assert.Equal(t, [4]float32{0, 0, 256, 256}, snapshot(t, s, "a").Draws[0].Clip)
	replacement := triangle()
	replacement.Meshes[0].Vertices[1].X = 2
	require.NoError(t, s.Apply([]Change{{"a", replacement}}))
	second := snapshot(t, s, "a", "b")
	assert.Equal(t, first.Meshes[0].ID, second.Meshes[0].ID)
	assert.Equal(t, uint64(2), second.Meshes[0].Revision)
	assert.Equal(t, first.Meshes[1], second.Meshes[1])
	assert.Equal(t, first.Textures[1], second.Textures[1])
	assert.Equal(t, float32(1), first.Meshes[0].Vertices[1].X, "old snapshots survive replacement")
	require.NoError(t, s.Apply([]Change{{"a", nil}, {"absent", nil}}))
	_, err := s.Snapshot([]Range{{Key: "a", Count: 1}})
	assert.ErrorIs(t, err, ErrInput)
	require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	third := snapshot(t, s, "a")
	assert.NotEqual(t, first.Meshes[0].ID, third.Meshes[0].ID, "removed IDs must never be reused")
	assert.Equal(t, uint64(1), third.Meshes[0].Revision)
	require.NoError(t, first.Validate())
}

func TestExplicitOrderAndInstances(t *testing.T) {
	s := newStore(t, Limits{})
	a := triangle()
	a.Draws = append(a.Draws, a.Draws[0])
	a.Draws[1].Material.Color[3] = 0.5
	require.NoError(t, s.Apply([]Change{{"a", a}, {"b", triangle()}}))
	order := []Range{{Key: "a", Count: 1, Transform: 2}, {Key: "b", Count: 1, Transform: 3}, {Key: "a", First: 1, Count: 1, Transform: 2}, {Key: "a", Count: 2, Transform: 4}}
	out, err := s.Snapshot(order)
	require.NoError(t, err)
	require.NoError(t, out.Validate())
	require.Len(t, out.Meshes, 2)
	require.Len(t, out.Textures, 2)
	require.Len(t, out.Draws, 5)
	assert.Equal(t, []int{2, 3, 2, 4, 4}, []int{out.Draws[0].Transform, out.Draws[1].Transform, out.Draws[2].Transform, out.Draws[3].Transform, out.Draws[4].Transform})
	assert.Equal(t, float32(0.5), out.Draws[2].Material.Color[3])
	assert.Equal(t, out.Draws[0].Mesh, out.Draws[3].Mesh)
	assert.Equal(t, a.Draws[0].Clip, out.Draws[3].Clip)
	assert.Equal(t, 0, a.Draws[0].Transform)
	// Snapshot metadata is owned; an accidental mutation cannot corrupt the store.
	out.Draws[0].Mesh = 0
	require.NoError(t, snapshot(t, s, "a").Validate())
	empty, err := s.Snapshot(nil)
	require.NoError(t, err)
	assert.Empty(t, empty.Meshes)
}

func TestAtomicFailureAndBounds(t *testing.T) {
	s := newStore(t, Limits{Fragments: 2, Bytes: 176}) // two 88-byte fixtures
	require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	before := snapshot(t, s, "a")
	next := s.nextID
	bad := triangle()
	bad.Meshes[0].Indices[2] = 99
	for _, changes := range [][]Change{
		{{"a", nil}, {"b", bad}},
		{{"a", triangle()}, {"a", nil}},
		{{"", triangle()}},
		{{strings.Repeat("x", 257), nil}},
		{{"a", nil}, {"b", nil}, {"c", nil}, {"d", nil}, {"e", nil}},
	} {
		require.Error(t, s.Apply(changes))
		assert.Equal(t, next, s.nextID)
		assert.Equal(t, before, snapshot(t, s, "a"))
	}
	require.NoError(t, s.Apply([]Change{{"b", triangle()}}))
	assert.ErrorIs(t, s.Apply([]Change{{"c", triangle()}}), ErrLimit)
	// Final-state accounting must allow add-before-remove at capacity.
	require.NoError(t, s.Apply([]Change{{"c", triangle()}, {"a", nil}}))
	require.NoError(t, snapshot(t, s, "b", "c").Validate())
	require.NoError(t, s.Apply([]Change{{"d", triangle()}, {"e", triangle()}, {"b", nil}, {"c", nil}}))
	require.NoError(t, snapshot(t, s, "d", "e").Validate())
	limited := newStore(t, Limits{Bytes: 87})
	assert.ErrorIs(t, limited.Apply([]Change{{"a", triangle()}}), ErrLimit)
	_, err := New(Limits{Fragments: -1})
	assert.ErrorIs(t, err, ErrLimit)
	_, err = New(Limits{Bytes: ceilings.Bytes + 1})
	assert.ErrorIs(t, err, ErrLimit)
	var zero Store
	assert.ErrorIs(t, zero.Apply(nil), ErrInput)
	_, err = zero.Snapshot(nil)
	assert.ErrorIs(t, err, ErrInput)
}

func TestSnapshotRejection(t *testing.T) {
	s := newStore(t, Limits{Draws: 2})
	require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	for _, r := range []Range{{Key: "missing", Count: 1}, {Key: "a", First: -1, Count: 1}, {Key: "a", Count: -1}, {Key: "a"}, {Key: "a", First: math.MaxInt, Count: 1}, {Key: "a", Count: math.MaxInt}, {Key: "a", Count: 1, Transform: -1}, {Key: strings.Repeat("x", 257), Count: 1}} {
		out, err := s.Snapshot([]Range{r})
		require.Error(t, err)
		assert.Nil(t, out)
	}
	r := Range{Key: "a", Count: 1}
	_, err := s.Snapshot([]Range{r, r, r})
	assert.ErrorIs(t, err, ErrLimit)
}

func TestExhaustionAndResourceRemoval(t *testing.T) {
	s := newStore(t, Limits{})
	s.nextID = 0
	assert.ErrorIs(t, s.Apply([]Change{{"a", triangle()}}), ErrLimit)
	s.nextID = math.MaxUint64
	assert.ErrorIs(t, s.Apply([]Change{{"a", triangle()}}), ErrLimit)
	assert.Empty(t, s.parts)
	assert.Equal(t, uint64(math.MaxUint64), s.nextID)
	s.nextID = 1
	require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	first := snapshot(t, s, "a")
	noTexture := triangle()
	noTexture.Textures = nil
	noTexture.Draws[0].Material.Texture = 0
	require.NoError(t, s.Apply([]Change{{"a", noTexture}}))
	assert.Empty(t, snapshot(t, s, "a").Textures)
	require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	assert.NotEqual(t, first.Textures[0].ID, snapshot(t, s, "a").Textures[0].ID)
	s.parts["a"].revision = math.MaxUint64
	assert.ErrorIs(t, s.Apply([]Change{{"a", triangle()}}), ErrLimit)
}

func TestIndependentResourceAndBatchLimits(t *testing.T) {
	for _, test := range []struct {
		limits Limits
		change func(*scene.Scene)
	}{
		{Limits{Meshes: 1}, func(s *scene.Scene) { s.Meshes = append(s.Meshes, s.Meshes[0]) }},
		{Limits{Textures: 1}, func(s *scene.Scene) { s.Textures = append(s.Textures, s.Textures[0]) }},
		{Limits{Draws: 1}, func(s *scene.Scene) { s.Draws = append(s.Draws, s.Draws[0]) }},
		{Limits{Bytes: 71}, func(s *scene.Scene) {}}, // vertex length before scan
		{Limits{Bytes: 72}, func(s *scene.Scene) {}}, // combined vertex/index bytes
		{Limits{Bytes: 88}, func(s *scene.Scene) { s.Meshes[0].Indices = make([]uint32, 23) }},
	} {
		s := newStore(t, test.limits)
		input := triangle()
		test.change(input)
		assert.ErrorIs(t, s.Apply([]Change{{"a", input}}), ErrLimit)
		assert.Empty(t, s.parts)
	}
	s := newStore(t, Limits{Bytes: 88})
	assert.ErrorIs(t, s.Apply([]Change{{"a", triangle()}, {"b", triangle()}}), ErrLimit)
	s = newStore(t, Limits{Draws: 2})
	input := triangle()
	input.Draws = append(input.Draws, input.Draws[0])
	require.NoError(t, s.Apply([]Change{{"a", input}}))
	_, err := s.Snapshot([]Range{{Key: "a", Count: 2}, {Key: "a", Count: 1}})
	assert.ErrorIs(t, err, ErrLimit)
}

func TestIndependentSnapshotReaders(t *testing.T) {
	s := newStore(t, Limits{})
	require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	old := snapshot(t, s, "a")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			assert.NoError(t, old.Validate())
			assert.Equal(t, uint64(1), old.Meshes[0].Revision)
		}
	}()
	for range 100 {
		require.NoError(t, s.Apply([]Change{{"a", triangle()}}))
	}
	<-done
}

func BenchmarkSnapshot(b *testing.B) {
	s := newStore(b, Limits{})
	input := triangle()
	// Large payload, small draw plan: snapshot cost should be metadata-only.
	input.Meshes[0].Vertices = make([]scene.Vertex, 350000)
	require.NoError(b, s.Apply([]Change{{"a", input}, {"b", input}}))
	order := []Range{{Key: "a", Count: 1}, {Key: "b", Count: 1, Transform: 1}, {Key: "a", Count: 1, Transform: 2}}
	b.ReportAllocs()
	for b.Loop() {
		_, err := s.Snapshot(order)
		if err != nil {
			b.Fatal(err)
		}
	}
}
