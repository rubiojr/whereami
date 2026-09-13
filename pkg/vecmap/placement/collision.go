package placement

import (
	"errors"
	"sort"
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

// SelectSymbols consumes a caller-owned scratch slice, sorting it stably in place
// by descending layer order then ascending sort key. Ties keep input order, which
// lets callers preserve tile/wrap/reverse-candidate traversal policy.
//
// Output owns its map and retains only keys. Failure returns nil, never partial
// acceptance; the scratch slice can remain sorted after work exhaustion. Projection,
// text/sprite readiness, viewport visibility and reference collection are external.
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
	sort.SliceStable(references, func(first, second int) bool {
		if references[first].Order != references[second].Order {
			return references[first].Order > references[second].Order
		}
		return references[first].SortKey < references[second].SortKey
	})
	remaining := options.WorkLimit
	if remaining == 0 {
		remaining = MaxCollisionWork
	}
	grid := collisionGrid{width: options.Width, height: options.Height, remaining: remaining, occupied: make(map[collisionCell][]Box)}
	accepted := make(map[K]Accepted, len(references))
	for _, reference := range references {
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
