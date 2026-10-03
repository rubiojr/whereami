// Package retained composes immutable scene fragments with stable resource IDs.
// It owns no native resources, tile loading, cover selection or upload scheduling.
package retained

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

var (
	ErrInput = errors.New("invalid retained update input")
	ErrLimit = errors.New("retained update limit exceeded")
)

// Limits bounds retained storage and each input batch before deep validation.
// Zero fields select defaults; positive fields may lower, not raise, the ceilings.
type Limits struct {
	Fragments, Meshes, Textures, Draws int
	Bytes                              uint64
}

var ceilings = Limits{Fragments: 128, Meshes: 4096, Textures: 4096, Draws: 65536, Bytes: 512 << 20}

// Store is single-owner, not copyable or safe for concurrent method calls.
// Published snapshots can be read independently while the owner applies updates.
// Construct with New; a zero Store rejects operations.
type Store struct {
	limits Limits
	parts  map[string]*fragment
	nextID uint64
	reused uint64
}

type fragment struct {
	scene            scene.Scene
	meshes, textures map[uint64]uint64 // local ID -> store-wide ID
	usage            Limits
	revision         uint64
}

// New starts an independent resource namespace. Do not combine snapshots from
// different stores or reuse a backend's residency cache across stores without reset.
func New(limits Limits) (*Store, error) {
	fields := []*int{&limits.Fragments, &limits.Meshes, &limits.Textures, &limits.Draws}
	maximum := []int{ceilings.Fragments, ceilings.Meshes, ceilings.Textures, ceilings.Draws}
	for i, field := range fields {
		if *field == 0 {
			*field = maximum[i]
		}
		if *field < 0 || *field > maximum[i] {
			return nil, ErrLimit
		}
	}
	if limits.Bytes == 0 {
		limits.Bytes = ceilings.Bytes
	}
	if limits.Bytes > ceilings.Bytes {
		return nil, ErrLimit
	}
	return &Store{limits: limits, parts: make(map[string]*fragment), nextID: 1}, nil
}

// Change replaces one logical fragment, or removes it when Scene is nil.
// Keys are caller-defined stable identities (for example tile/style/wrap slots),
// nonempty and at most 256 bytes. A batch may mention a key only once.
type Change struct {
	Key   string
	Scene *scene.Scene
}

// Apply publishes at most twice the fragment limit in changes atomically (so a
// full remove-and-replace fits one batch). It validates bounded input before allocating
// remapped metadata, and consumes no IDs on failure. Removing an absent key is a
// no-op. A replacement bumps the revision of every resource whose payload changed
// or is new; a resource with byte-identical payload under the same store ID keeps
// its revision. Input revisions are ignored. Untouched fragments keep IDs/revisions.
// Input metadata is copied. Geometry/index/pixel buffers are immutable borrows
// through the lifetime of the store AND every snapshot that references them.
func (s *Store) Apply(changes []Change) error {
	if s.parts == nil {
		return ErrInput
	}
	if len(changes) > 2*s.limits.Fragments {
		return ErrLimit
	}
	usage, err := s.preflight(changes)
	if err != nil {
		return err
	}
	for _, change := range changes {
		if change.Scene != nil {
			if err := change.Scene.ValidateExcept(s.validated(change.Key)); err != nil {
				return fmt.Errorf("fragment %q: %w", change.Key, err)
			}
		}
	}
	next := maps.Clone(s.parts)
	nextID := s.nextID
	var reused uint64
	for i, change := range changes {
		if change.Scene == nil {
			delete(next, change.Key)
			continue
		}
		part, kept, err := remap(change.Scene, s.parts[change.Key], &nextID)
		if err != nil {
			return err
		}
		reused += uint64(kept)
		part.usage = usage[i]
		next[strings.Clone(change.Key)] = part
	}
	s.parts, s.nextID = next, nextID
	s.reused += reused
	return nil
}

// validated reports which meshes of a replacement for key are the buffers of
// the mesh with the same local ID it replaces, whose content passed
// validation when applied and is immutable.
func (s *Store) validated(key string) func(*scene.Mesh) bool {
	old := s.parts[key]
	if old == nil {
		return nil
	}
	return func(mesh *scene.Mesh) bool {
		id := old.meshes[mesh.ID]
		for i := range old.scene.Meshes {
			if previous := &old.scene.Meshes[i]; previous.ID == id && id != 0 {
				return sharedMesh(previous, mesh)
			}
		}
		return false
	}
}

// ReusedVersions counts replaced resources that kept their revision because their
// payload was byte-identical. It describes avoided upload planning, not GPU work.
func (s *Store) ReusedVersions() uint64 { return s.reused }

func (s *Store) preflight(changes []Change) ([]Limits, error) {
	seen := make(map[string]bool, len(changes))
	usage := make([]Limits, len(changes))
	var input, total Limits
	for _, p := range s.parts {
		total.add(p.usage)
	}
	for i, change := range changes {
		if change.Key == "" || len(change.Key) > 256 || seen[change.Key] {
			return nil, ErrInput
		}
		seen[change.Key] = true
		if change.Scene != nil {
			var err error
			usage[i], err = measure(change.Scene, s.limits)
			if err != nil {
				return nil, err
			}
		}
		input.add(usage[i])
		if !input.fits(s.limits) {
			return nil, ErrLimit
		}
		if old := s.parts[change.Key]; old != nil {
			total.subtract(old.usage)
		}
	}
	// Evaluate final size only after all removals, so batch order cannot reject a
	// replacement cover that fits (e.g. add children before removing their parent).
	total.add(input)
	if !total.fits(s.limits) {
		return nil, ErrLimit
	}
	return usage, nil
}

func measure(value *scene.Scene, limit Limits) (Limits, error) {
	u := Limits{Fragments: 1, Meshes: len(value.Meshes), Textures: len(value.Textures), Draws: len(value.Draws)}
	if !u.fits(limit) {
		return Limits{}, ErrLimit
	}
	for _, mesh := range value.Meshes {
		// Check lengths before byte multiplication, including on 32-bit systems.
		for layout := range scene.LayoutCount {
			if uint64(mesh.Len(layout)) > limit.Bytes/uint64(layout.Bytes()) {
				return Limits{}, ErrLimit
			}
		}
		if uint64(len(mesh.Indices)) > limit.Bytes/4 {
			return Limits{}, ErrLimit
		}
		u.Bytes += mesh.BufferBytes()
		if u.Bytes > limit.Bytes {
			return Limits{}, ErrLimit
		}
	}
	for _, texture := range value.Textures {
		if uint64(len(texture.RGBA)) > limit.Bytes-u.Bytes {
			return Limits{}, ErrLimit
		}
		u.Bytes += uint64(len(texture.RGBA))
	}
	return u, nil
}

func (u *Limits) add(v Limits) {
	u.Fragments += v.Fragments
	u.Meshes += v.Meshes
	u.Textures += v.Textures
	u.Draws += v.Draws
	u.Bytes += v.Bytes
}

func (u *Limits) subtract(v Limits) {
	u.Fragments -= v.Fragments
	u.Meshes -= v.Meshes
	u.Textures -= v.Textures
	u.Draws -= v.Draws
	u.Bytes -= v.Bytes
}

func (u Limits) fits(v Limits) bool {
	return u.Fragments <= v.Fragments && u.Meshes <= v.Meshes && u.Textures <= v.Textures && u.Draws <= v.Draws && u.Bytes <= v.Bytes
}
