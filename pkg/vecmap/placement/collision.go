package placement

import (
	"cmp"
	"errors"
	"slices"
)

const (
	CollisionCellSize      = 64.0
	MaxCollisionReferences = 100_000
	MaxCollisionWork       = 1_000_000
	// MaxCollisionViewport bounds grid indexes on every supported architecture.
	MaxCollisionViewport = 1 << 20
)

var (
	ErrCollisionLimit = errors.New("symbol collision resource limit exceeded")
	ErrCollisionInput = errors.New("invalid symbol collision input")
)

// Box is a projected logical-pixel rectangle. Intersection is strict: touching
// edges do not collide. Rectangle ordering is not normalized.
type Box struct{ Left, Top, Right, Bottom float64 }

// CollisionPart separates availability/visibility from paint policy. In
// particular, present but unavailable required text can suppress a visible icon.
type CollisionPart struct {
	Box                                       Box
	Present, Visible, AllowsOverlap, Optional bool
}

// CollisionReference carries only the data needed for acceptance, with an opaque
// caller-owned logical key. Keys should be unique and stable within a job.
type CollisionReference[K comparable] struct {
	Key        K
	Order      int
	SortKey    float64
	Text, Icon CollisionPart
}

// Accepted records independent text/icon decisions after their dependency rules.
type Accepted struct{ Text, Icon bool }

type CollisionOptions struct {
	Width, Height float64
	// WorkLimit counts visited cells and occupied-box comparisons. Zero selects
	// MaxCollisionWork; a positive value may only lower that ceiling.
	WorkLimit int
}

// SelectSymbols accepts references by descending layer order then ascending sort
// key. Ties keep input order, which lets callers preserve tile/wrap/reverse-candidate
// traversal policy. It doesn't reorder references.
//
// Output owns its map and retains only keys. Failure returns nil, never partial
// acceptance. Projection, text/sprite readiness, viewport visibility and reference
// collection are external.
func SelectSymbols[K comparable](references []CollisionReference[K], options CollisionOptions) (map[K]Accepted, error) {
	if len(references) > MaxCollisionReferences {
		return nil, ErrCollisionLimit
	}
	if err := validateCollisionOptions(options); err != nil {
		return nil, err
	}
	for _, reference := range references {
		if !finite(reference.SortKey) || !validCollisionPart(reference.Text) || !validCollisionPart(reference.Icon) {
			return nil, ErrCollisionInput
		}
	}
	ranks := orderRanks(references)
	remaining := options.WorkLimit
	if remaining == 0 {
		remaining = MaxCollisionWork
	}
	grid := collisionGrid{width: options.Width, height: options.Height, remaining: remaining, occupied: make(map[collisionCell][]Box)}
	accepted := make(map[K]Accepted, len(references))
	for _, rank := range ranks {
		reference := &references[rank.index]
		decision, err := grid.accept(reference.Text, reference.Icon)
		if err != nil {
			return nil, err
		}
		if decision.Text || decision.Icon {
			accepted[reference.Key] = decision
		}
	}
	return accepted, nil
}

// symbolRank is a reference's priority and its input index. Ranks are small, so
// sorting them moves far less than sorting the references themselves.
type symbolRank struct {
	order int
	key   float64
	index int32
}

// countingSpan bounds the extra counters a counting sort by order may use.
const countingSpan = 256

// orderRanks returns the references' ranks in acceptance order. Orders are style
// layer indices, so a counting sort by order does most of the work in linear time
// and keeps input order within a layer; only a layer whose sort keys vary needs
// comparisons. Tiles collect references in descending order already, which a
// comparison sort gains little from. Widely spread orders fall back to one.
func orderRanks[K comparable](references []CollisionReference[K]) []symbolRank {
	ranks := make([]symbolRank, len(references))
	if len(references) == 0 {
		return ranks
	}
	low, high := references[0].Order, references[0].Order
	for i := range references {
		low, high = min(low, references[i].Order), max(high, references[i].Order)
	}
	if span := uint64(high) - uint64(low); span > uint64(len(references))+countingSpan {
		for i := range references {
			ranks[i] = symbolRank{order: references[i].Order, key: references[i].SortKey, index: int32(i)}
		}
		slices.SortFunc(ranks, compareRanks)
		return ranks
	}
	// starts[high-order] is where that order's ranks begin, highest order first.
	starts := make([]int32, high-low+2)
	for i := range references {
		starts[high-references[i].Order+1]++
	}
	for i := 1; i < len(starts); i++ {
		starts[i] += starts[i-1]
	}
	for i := range references {
		slot := &starts[high-references[i].Order]
		ranks[*slot] = symbolRank{order: references[i].Order, key: references[i].SortKey, index: int32(i)}
		*slot++
	}
	for start := 0; start < len(ranks); {
		end := start + 1
		for end < len(ranks) && ranks[end].order == ranks[start].order {
			end++
		}
		if layer := ranks[start:end]; !slices.IsSortedFunc(layer, compareKeys) {
			slices.SortFunc(layer, compareRanks)
		}
		start = end
	}
	return ranks
}

func compareKeys(a, b symbolRank) int { return cmp.Compare(a.key, b.key) }

// compareRanks orders by descending order, then ascending sort key, then input
// index, which makes an unstable sort stable.
func compareRanks(a, b symbolRank) int {
	if a.order != b.order {
		return cmp.Compare(b.order, a.order)
	}
	if c := compareKeys(a, b); c != 0 {
		return c
	}
	return cmp.Compare(a.index, b.index)
}

func validateCollisionOptions(options CollisionOptions) error {
	if !finite(options.Width) || !finite(options.Height) || options.Width < 0 || options.Height < 0 {
		return ErrCollisionInput
	}
	if options.Width > MaxCollisionViewport || options.Height > MaxCollisionViewport || options.WorkLimit < 0 || options.WorkLimit > MaxCollisionWork {
		return ErrCollisionLimit
	}
	return nil
}

func validCollisionPart(part CollisionPart) bool {
	return !part.Visible || (finite(part.Box.Left) && finite(part.Box.Top) && finite(part.Box.Right) && finite(part.Box.Bottom))
}

func (g *collisionGrid) accept(text, icon CollisionPart) (Accepted, error) {
	textCollision, err := g.partIntersects(text)
	if err != nil {
		return Accepted{}, err
	}
	iconCollision, err := g.partIntersects(icon)
	if err != nil {
		return Accepted{}, err
	}
	textAccepted := text.Visible && !textCollision
	iconAccepted := icon.Visible && !iconCollision
	if text.Present && !text.Visible && !text.Optional {
		iconAccepted = false
	}
	if text.Present && textCollision && !text.Optional {
		iconAccepted = false
	}
	if icon.Present && iconCollision && !icon.Optional {
		textAccepted = false
	}
	if textAccepted && !text.AllowsOverlap {
		if err := g.add(text.Box); err != nil {
			return Accepted{}, err
		}
	}
	if iconAccepted && !icon.AllowsOverlap {
		if err := g.add(icon.Box); err != nil {
			return Accepted{}, err
		}
	}
	return Accepted{Text: textAccepted, Icon: iconAccepted}, nil
}
