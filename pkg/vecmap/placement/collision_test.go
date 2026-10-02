package placement

import (
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func visiblePart(x float64) CollisionPart {
	return CollisionPart{Box: Box{Left: x, Top: 0, Right: x + 10, Bottom: 10}, Present: true, Visible: true}
}

func TestSelectSymbolsStablePriorityAndOwnership(t *testing.T) {
	refs := []CollisionReference[int]{
		{Key: 1, Order: 1, Text: visiblePart(0)},
		{Key: 2, Order: 2, SortKey: 5, Text: visiblePart(0)},
		{Key: 3, Order: 2, SortKey: 1, Text: visiblePart(0)},
		{Key: 4, Order: 2, SortKey: 1, Text: visiblePart(0)},
		{Key: 5, Order: 2, SortKey: 1, Text: visiblePart(20)},
	}
	accepted, err := SelectSymbols(refs, CollisionOptions{Width: 128, Height: 128})
	require.NoError(t, err)
	assert.Equal(t, map[int]Accepted{3: {Text: true}, 5: {Text: true}}, accepted)
	var order []int
	for _, ref := range refs {
		order = append(order, ref.Key)
	}
	assert.Equal(t, []int{1, 2, 3, 4, 5}, order, "references keep their order")
	clear(refs)
	assert.Equal(t, map[int]Accepted{3: {Text: true}, 5: {Text: true}}, accepted)
}

// Narrow orders take the counting sort, widely spread ones the comparison sort.
func TestCollisionGridMatchesBruteForceTextSelection(t *testing.T) {
	for name, order := range map[string]func(*rand.Rand) int{
		"layers":  func(random *rand.Rand) int { return random.IntN(3) },
		"spread":  func(random *rand.Rand) int { return random.IntN(3) * 1_000_000_000 },
		"extreme": func(random *rand.Rand) int { return []int{math.MinInt, 0, math.MaxInt}[random.IntN(3)] },
	} {
		t.Run(name, func(t *testing.T) { requireBruteForceSelection(t, order) })
	}
	accepted, err := SelectSymbols([]CollisionReference[int]{}, CollisionOptions{Width: 200, Height: 200})
	require.NoError(t, err)
	assert.Empty(t, accepted)
}

func requireBruteForceSelection(t *testing.T, order func(*rand.Rand) int) {
	random := rand.New(rand.NewPCG(1, 2))
	refs := make([]CollisionReference[int], 300)
	for index := range refs {
		left, top := float64(random.IntN(220)-20), float64(random.IntN(220)-20)
		box := Box{Left: left, Top: top, Right: left + float64(random.IntN(40)+1), Bottom: top + float64(random.IntN(40)+1)}
		refs[index] = CollisionReference[int]{Key: index, Order: order(random), SortKey: float64(random.IntN(3)),
			Text: CollisionPart{Box: box, Present: true, Visible: box.Right >= 0 && box.Bottom >= 0 && box.Left <= 200 && box.Top <= 200}}
	}
	ordered := slices.Clone(refs)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Order != ordered[j].Order {
			return ordered[i].Order > ordered[j].Order
		}
		return ordered[i].SortKey < ordered[j].SortKey
	})
	want := make(map[int]Accepted)
	var boxes []Box
	for _, ref := range ordered {
		if !ref.Text.Visible {
			continue
		}
		blocked := false
		for _, occupied := range boxes {
			if BoxesIntersect(ref.Text.Box, occupied) {
				blocked = true
				break
			}
		}
		if !blocked {
			want[ref.Key] = Accepted{Text: true}
			boxes = append(boxes, ref.Text.Box)
		}
	}
	got, err := SelectSymbols(refs, CollisionOptions{Width: 200, Height: 200})
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSelectSymbolsOptionalDependenciesAndOverlap(t *testing.T) {
	for _, tt := range []struct {
		name       string
		text, icon CollisionPart
		want       Accepted
	}{
		{"both collide", visiblePart(0), visiblePart(0), Accepted{}},
		{"required text blocks icon", visiblePart(0), visiblePart(20), Accepted{}},
		{"optional text keeps icon", CollisionPart{Box: visiblePart(0).Box, Present: true, Visible: true, Optional: true}, visiblePart(20), Accepted{Icon: true}},
		{"required icon blocks text", visiblePart(20), visiblePart(0), Accepted{}},
		{"optional icon keeps text", visiblePart(20), CollisionPart{Box: visiblePart(0).Box, Present: true, Visible: true, Optional: true}, Accepted{Text: true}},
		{"unavailable required text", CollisionPart{Present: true}, visiblePart(20), Accepted{}},
		{"unavailable optional text", CollisionPart{Present: true, Optional: true}, visiblePart(20), Accepted{Icon: true}},
		{"unavailable icon does not block text", visiblePart(20), CollisionPart{Present: true}, Accepted{Text: true}},
		{"absent text", CollisionPart{}, visiblePart(20), Accepted{Icon: true}},
		{"same symbol parts do not collide", visiblePart(20), visiblePart(20), Accepted{Text: true, Icon: true}},
		{"overlap skips queries", CollisionPart{Box: visiblePart(0).Box, Present: true, Visible: true, AllowsOverlap: true}, CollisionPart{Box: visiblePart(0).Box, Present: true, Visible: true, AllowsOverlap: true}, Accepted{Text: true, Icon: true}},
		{"required icon can veto overlapping text", CollisionPart{Box: visiblePart(0).Box, Present: true, Visible: true, AllowsOverlap: true}, visiblePart(0), Accepted{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			refs := []CollisionReference[int]{{Key: 1, Text: visiblePart(0)}, {Key: 2, Text: tt.text, Icon: tt.icon}}
			accepted, err := SelectSymbols(refs, CollisionOptions{Width: 128, Height: 128})
			require.NoError(t, err)
			assert.Equal(t, Accepted{Text: true}, accepted[1])
			assert.Equal(t, tt.want, accepted[2])
			if tt.want == (Accepted{}) {
				assert.NotContains(t, accepted, 2)
			}
		})
	}
	// Overlap-enabled parts neither query nor occupy cells for later candidates.
	part := visiblePart(0)
	part.AllowsOverlap = true
	accepted, err := SelectSymbols([]CollisionReference[int]{{Key: 1, Text: part, Icon: part}, {Key: 2, Text: visiblePart(0)}}, CollisionOptions{Width: 128, Height: 128})
	require.NoError(t, err)
	assert.Len(t, accepted, 2)
}

func TestCollisionCellBoundariesClippingAndStrictIntersections(t *testing.T) {
	for _, x := range []float64{10, 64} {
		refs := []CollisionReference[int]{{Key: 1, Text: CollisionPart{Box: Box{Right: x, Bottom: 10}, Visible: true}}, {Key: 2, Text: visiblePart(x)}}
		accepted, err := SelectSymbols(refs, CollisionOptions{Width: 128, Height: 128})
		require.NoError(t, err)
		assert.Len(t, accepted, 2)
	}
	assert.False(t, BoxesIntersect(Box{Right: 10, Bottom: 10}, Box{Left: 10, Top: 10, Right: 20, Bottom: 20}))
	assert.True(t, BoxesIntersect(Box{Right: 10, Bottom: 10}, Box{Left: 9, Top: 9, Right: 20, Bottom: 20}))
	giant := CollisionPart{Visible: true, Box: Box{Left: -math.MaxFloat64, Top: -math.MaxFloat64, Right: math.MaxFloat64, Bottom: math.MaxFloat64}}
	accepted, err := SelectSymbols([]CollisionReference[int]{{Key: 1, Text: giant}, {Key: 2, Icon: visiblePart(0)}}, CollisionOptions{Width: 128, Height: 128, WorkLimit: 20})
	require.NoError(t, err) // Nine cells queried + nine inserted + one query/comparison.
	assert.Equal(t, map[int]Accepted{1: {Text: true}}, accepted)
	for _, box := range []Box{
		{Left: math.MaxFloat64, Right: math.MaxFloat64, Bottom: 1},
		{Left: -math.MaxFloat64, Right: -math.MaxFloat64, Bottom: 1},
		{Top: math.MaxFloat64, Bottom: math.MaxFloat64, Right: 1},
	} {
		_, err := SelectSymbols([]CollisionReference[int]{{Key: 1, Text: CollisionPart{Visible: true, Box: box}}}, CollisionOptions{Width: 128, Height: 128, WorkLimit: 1})
		require.NoError(t, err) // No out-of-range integer conversion or cell visit.
	}
	// Keep the old floored cell traversal even for inverted rectangles in one cell.
	accepted, err = SelectSymbols([]CollisionReference[int]{{Key: 1, Text: visiblePart(0)}, {Key: 2, Text: CollisionPart{Visible: true, Box: Box{Left: 8, Right: 2, Bottom: 10}}}}, CollisionOptions{Width: 128, Height: 128})
	require.NoError(t, err)
	assert.NotContains(t, accepted, 2)
}

func TestCollisionLimitsAndAtomicFailure(t *testing.T) {
	for _, tt := range []struct {
		refs []CollisionReference[int]
		work int
	}{
		{[]CollisionReference[int]{{Key: 1, Text: visiblePart(0)}}, 1},                                  // Text insertion.
		{[]CollisionReference[int]{{Key: 1, Text: visiblePart(0), Icon: visiblePart(20)}}, 3},           // Icon insertion.
		{[]CollisionReference[int]{{Key: 1, Text: visiblePart(0)}, {Key: 2, Text: visiblePart(0)}}, 3},  // Text comparison.
		{[]CollisionReference[int]{{Key: 1, Icon: visiblePart(0)}, {Key: 2, Icon: visiblePart(0)}}, 3},  // Icon comparison.
		{[]CollisionReference[int]{{Key: 1, Text: visiblePart(0)}, {Key: 2, Text: visiblePart(20)}}, 2}, // Next query.
		{[]CollisionReference[int]{{Key: 1, Text: visiblePart(0), Icon: visiblePart(20)}}, 1},           // Icon query.
	} {
		accepted, err := SelectSymbols(tt.refs, CollisionOptions{Width: 128, Height: 128, WorkLimit: tt.work})
		assert.ErrorIs(t, err, ErrCollisionLimit)
		assert.Nil(t, accepted)
	}
	accepted, err := SelectSymbols([]CollisionReference[int]{{Key: 1, Text: visiblePart(0)}}, CollisionOptions{Width: 128, Height: 128, WorkLimit: 2})
	require.NoError(t, err)
	assert.Len(t, accepted, 1)
	_, err = SelectSymbols(make([]CollisionReference[int], MaxCollisionReferences+1), CollisionOptions{})
	assert.ErrorIs(t, err, ErrCollisionLimit)
	accepted, err = SelectSymbols(make([]CollisionReference[int], MaxCollisionReferences), CollisionOptions{Width: MaxCollisionViewport, Height: MaxCollisionViewport})
	require.NoError(t, err)
	assert.Empty(t, accepted)
}

func TestCollisionRejectsInvalidInputBeforeSorting(t *testing.T) {
	base := []CollisionReference[int]{{Key: 1, Order: 1, Text: visiblePart(0)}, {Key: 2, Order: 2}}
	for _, options := range []CollisionOptions{
		{Width: math.NaN()}, {Height: math.Inf(1)}, {Width: -1}, {Height: -1},
		{Width: MaxCollisionViewport + 1}, {Height: MaxCollisionViewport + 1}, {WorkLimit: -1}, {WorkLimit: MaxCollisionWork + 1},
	} {
		refs := slices.Clone(base)
		accepted, err := SelectSymbols(refs, options)
		assert.Error(t, err)
		assert.Nil(t, accepted)
		assert.Equal(t, base, refs)
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for field := range 5 {
			ref := CollisionReference[int]{Text: visiblePart(0)}
			fields := []*float64{&ref.SortKey, &ref.Text.Box.Left, &ref.Text.Box.Top, &ref.Text.Box.Right, &ref.Text.Box.Bottom}
			*fields[field] = invalid
			accepted, err := SelectSymbols([]CollisionReference[int]{ref}, CollisionOptions{})
			assert.ErrorIs(t, err, ErrCollisionInput)
			assert.Nil(t, accepted)
		}
	}
	ref := CollisionReference[int]{Icon: CollisionPart{Visible: true, Box: Box{Left: math.NaN()}}}
	_, err := SelectSymbols([]CollisionReference[int]{ref}, CollisionOptions{})
	assert.ErrorIs(t, err, ErrCollisionInput)
	ref.Icon.Visible = false
	accepted, err := SelectSymbols([]CollisionReference[int]{ref}, CollisionOptions{})
	require.NoError(t, err)
	assert.Empty(t, accepted) // Invisible parts' unused boxes need not be finite.
}

func FuzzCollisionSelection(f *testing.F) {
	f.Add(0.0, 0.0, 100.0, 100.0)
	f.Add(-1e30, -1e30, 1e30, 1e30)
	f.Fuzz(func(t *testing.T, left, top, right, bottom float64) {
		refs := []CollisionReference[int]{{Key: 1, Text: CollisionPart{Present: true, Visible: true, Box: Box{Left: left, Top: top, Right: right, Bottom: bottom}}}, {Key: 2, Text: visiblePart(0)}}
		accepted, err := SelectSymbols(refs, CollisionOptions{Width: 512, Height: 512, WorkLimit: 1000})
		if err != nil {
			require.Nil(t, accepted)
			return
		}
		require.LessOrEqual(t, len(accepted), 2)
		for _, decision := range accepted {
			require.True(t, decision.Text || decision.Icon)
		}
	})
}

// BenchmarkSelectSymbols selects among 12 tiles of 330 references, each tile in
// descending layer order as tiles collect them; a quarter of the layers have
// varying sort keys.
func BenchmarkSelectSymbols(b *testing.B) {
	random := rand.New(rand.NewPCG(3, 4))
	var refs []CollisionReference[int]
	for range 12 {
		orders := make([]int, 330)
		for i := range orders {
			orders[i] = 60 + random.IntN(25)
		}
		slices.Sort(orders)
		slices.Reverse(orders)
		for _, order := range orders {
			key := 0.0
			if order%4 == 0 {
				key = float64(random.IntN(10))
			}
			x, y := random.Float64()*1200, random.Float64()*800
			refs = append(refs, CollisionReference[int]{Key: len(refs), Order: order, SortKey: key,
				Text: CollisionPart{Box: Box{Left: x, Top: y, Right: x + 60, Bottom: y + 14}, Present: true, Visible: true}})
		}
	}
	// Callers collect references in traversal order for every selection.
	scratch := make([]CollisionReference[int], len(refs))
	for b.Loop() {
		copy(scratch, refs)
		if _, err := SelectSymbols(scratch, CollisionOptions{Width: 1280, Height: 860}); err != nil {
			b.Fatal(err)
		}
	}
}
