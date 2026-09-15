package retained

import (
	"errors"
	"slices"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

var (
	ErrBusy   = errors.New("upload batch awaits acknowledgement")
	ErrTicket = errors.New("invalid upload acknowledgement ticket")
	ErrBudget = errors.New("upload batch budget cannot admit resource")
)

type ResourceKind uint8

const (
	MeshResource ResourceKind = iota
	TextureResource
)

// Version identifies one resident allocation, including its revision. Backends
// must be able to stage different revisions of the same ID simultaneously.
type Version struct {
	Kind         ResourceKind
	ID, Revision uint64
}

// Resource carries exactly one mesh or texture according to Version.Kind.
// Metadata is copied; payload slices are immutable borrows from the target scene.
type Resource struct {
	Version Version
	Mesh    scene.Mesh
	Texture scene.Texture
}

func (r Resource) bytes() uint64 {
	if r.Version.Kind == MeshResource {
		return r.Mesh.BufferBytes()
	}
	return uint64(len(r.Texture.RGBA))
}

// ResidencyLimits covers logical payload of active plus staged resources. Zero selects
// defaults; positive values may lower the 1 GiB / 16,384-resource ceilings.
type ResidencyLimits struct {
	Bytes     uint64
	Resources int
}

// Budget bounds one batch. Both values must be positive and within residency
// limits. Meshes (vertices plus indices) and textures are indivisible upload units.
type Budget struct {
	Bytes     uint64
	Resources int
}

// Batch is either uploads or releases, never both. Bytes counts upload payload.
// Execute the entire batch before Acknowledge. On failed uploads discard all
// allocations from that batch; failed releases must have released nothing.
type Batch struct {
	Ticket   uint64
	Uploads  []Resource
	Releases []Version
	Bytes    uint64
}

// Planner is a single-owner CPU state machine, not safe to copy or call
// concurrently. It owns no native handles and assumes an initially empty backend
// residency namespace. Native staging, submission/fences and destruction belong
// to the adapter. Use one retained Store namespace throughout its lifetime.
type Planner struct {
	limits               ResidencyLimits
	active, target       *scene.Scene
	activeSet, targetSet map[Version]Resource
	targetOrder          []Resource
	resident             map[Version]Resource
	residentOrder        []Version
	pending              *Batch
	nextTicket           uint64
}

func NewPlanner(limits ResidencyLimits) (*Planner, error) {
	if limits.Bytes == 0 {
		limits.Bytes = 1 << 30
	}
	if limits.Resources == 0 {
		limits.Resources = 16384
	}
	if limits.Bytes > 1<<30 || limits.Resources < 0 || limits.Resources > 16384 {
		return nil, ErrLimit
	}
	return &Planner{limits: limits, resident: make(map[Version]Resource), nextTicket: 1}, nil
}

// Current is the last fully acknowledged scene, or nil before initial readiness
// and after an explicit clear. Camera-only frames should retain this scene.
func (p *Planner) Current() *scene.Scene { return p.active }

// SetTarget borrows an immutable retained snapshot and validates it on the calling
// preparation worker. A Version must always refer to the same immutable payload;
// this is guaranteed by snapshots from a single Store, not by hashing pixels here.
// Active plus desired resources must fit residency limits before target mutation.
// Supersession is allowed only between batches. Nil clears the active scene;
// obsolete allocations are then released through acknowledged batches.
func (p *Planner) SetTarget(target *scene.Scene) error {
	if p.resident == nil {
		return ErrInput
	}
	if p.pending != nil {
		return ErrBusy
	}
	resources, err := targetResources(target, p.limits)
	if err != nil {
		return err
	}
	set := make(map[Version]Resource, len(resources))
	for _, r := range resources {
		set[r.Version] = r
	}
	if !p.fitsTarget(set) {
		return ErrLimit
	}
	p.target, p.targetOrder, p.targetSet = target, resources, set
	p.publishReady()
	return nil
}

func (p *Planner) fitsTarget(target map[Version]Resource) bool {
	var bytes uint64
	count := len(p.activeSet)
	for _, r := range p.activeSet {
		bytes += r.bytes()
	}
	for key, r := range target {
		if _, exists := p.activeSet[key]; !exists {
			bytes += r.bytes()
			count++
		}
	}
	return bytes <= p.limits.Bytes && count <= p.limits.Resources
}

func targetResources(target *scene.Scene, limits ResidencyLimits) ([]Resource, error) {
	if target == nil {
		return nil, nil
	}
	// Independent length checks avoid overflow before adding hostile slice lengths.
	if len(target.Meshes) > limits.Resources || len(target.Textures) > limits.Resources-len(target.Meshes) {
		return nil, ErrLimit
	}
	bounded := ceilings
	bounded.Bytes = min(bounded.Bytes, limits.Bytes)
	if _, err := measure(target, bounded); err != nil {
		return nil, err
	}
	if err := target.Validate(); err != nil {
		return nil, err
	}
	result := make([]Resource, 0, len(target.Meshes)+len(target.Textures))
	for _, m := range target.Meshes {
		if m.Revision == 0 {
			return nil, ErrInput
		}
		result = append(result, Resource{Version: Version{Kind: MeshResource, ID: m.ID, Revision: m.Revision}, Mesh: m})
	}
	for _, t := range target.Textures {
		if t.Revision == 0 {
			return nil, ErrInput
		}
		result = append(result, Resource{Version: Version{Kind: TextureResource, ID: t.ID, Revision: t.Revision}, Texture: t})
	}
	return result, nil
}

func (p *Planner) publishReady() {
	for key := range p.targetSet {
		if _, ready := p.resident[key]; !ready {
			return
		}
	}
	p.active, p.activeSet = p.target, p.targetSet
}

// Acknowledge commits only the matching outstanding batch. Success means uploads
// are usable by the next frame, or releases have finished their native lifetime
// obligations. Failure changes no residency and allows retry with a fresh ticket.
// A duplicate/stale ticket never acknowledges another batch. Publication occurs
// only when all target resources are ready; old revisions retire afterward.
func (p *Planner) Acknowledge(ticket uint64, success bool) error {
	if p.resident == nil {
		return ErrInput
	}
	if p.pending == nil || p.pending.Ticket != ticket {
		return ErrTicket
	}
	batch := p.pending
	if success {
		for _, key := range batch.Releases {
			delete(p.resident, key)
		}
		p.residentOrder = slices.DeleteFunc(p.residentOrder, func(key Version) bool { _, ok := p.resident[key]; return !ok })
		for _, r := range batch.Uploads {
			p.resident[r.Version] = r
			p.residentOrder = append(p.residentOrder, r.Version)
		}
	}
	p.pending = nil
	p.publishReady()
	return nil
}
