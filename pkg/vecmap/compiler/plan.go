package compiler

import (
	"encoding/binary"
	"errors"
	"hash/maphash"
	"unsafe"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

// ErrStableMismatch reports that geometry for StableMesh would differ from the
// plan a compilation or build borrows. Callers prepare and build in full.
var ErrStableMismatch = errors.New("stable geometry differs from its plan")

// StablePlan records the stable batches of one tile: each batch CompileTile
// packs into StableMesh (fills, patterns, extruded and shader-dashed lines,
// backgrounds), in emission order. A run is identified by its layer, kind,
// cap and join and the source features it holds, so equal plans mean equal
// StableMesh bytes for the same decoded tile, layers and options. After a
// build it also holds the draw each run produced. It is immutable once
// published and holds no geometry.
type StablePlan struct {
	runs []stableRun
}

type stableRun struct {
	hash     uint64
	elements int  // draw elements of the batch's geometry
	emitted  bool // the batch became a primitive: nonempty, visible paint, within budget
	// Set by a build.
	drawn        bool
	first, count uint32
	layout       scene.Layout
}

// RetainedBytes is the plan's storage.
func (p *StablePlan) RetainedBytes() uint64 {
	if p == nil {
		return 0
	}
	return uint64(unsafe.Sizeof(*p)) + uint64(cap(p.runs))*uint64(unsafe.Sizeof(stableRun{}))
}

// StablePlanner follows the stable batches of one CompileTilePlanned call. With
// a reference plan it borrows: stable batches are evaluated but not
// tessellated, and emit placeholder primitives that a FragmentBuilder draws
// from the reference's mesh. The first batch that differs aborts compilation
// with ErrStableMismatch. Without one it records a new plan.
type StablePlanner struct {
	reference *StablePlan
	runs      []stableRun
	next      int // reference runs matched so far
	current   int // 1 + the run being emitted, 0 between runs
}

func NewStablePlanner(reference *StablePlan) *StablePlanner {
	return &StablePlanner{reference: reference}
}

// Plan is the recorded plan, or the reference when borrowing.
func (p *StablePlanner) Plan() *StablePlan {
	if p.reference != nil {
		return p.reference
	}
	return &StablePlan{runs: p.runs}
}

func (p *StablePlanner) borrowing() bool { return p != nil && p.reference != nil }

// begin starts emitting a stable batch.
func (p *StablePlanner) begin(hash uint64) error {
	if p.reference == nil {
		p.runs = append(p.runs, stableRun{hash: hash})
		p.current = len(p.runs)
		return nil
	}
	if p.next >= len(p.reference.runs) || p.reference.runs[p.next].hash != hash {
		return ErrStableMismatch
	}
	p.next++
	p.current = p.next
	return nil
}

// beginRun starts emitting a stable batch identified by runHash. A nil planner
// follows nothing.
func (p *StablePlanner) beginRun(kind stableKind, order int, lineCap, lineJoin string, features *featureHash) error {
	if p == nil {
		return nil
	}
	return p.begin(runHash(kind, order, lineCap, lineJoin, features))
}

// end closes the batch begin started.
func (p *StablePlanner) end() {
	if p != nil {
		p.current = 0
	}
}

// add records that a batch holds a source feature.
func (p *StablePlanner) add(features *featureHash, feature int) {
	if p != nil {
		features.add(feature)
	}
}

// finish checks that a borrowing compilation matched every reference run.
func (p *StablePlanner) finish() error {
	if p != nil && p.reference != nil && p.next != len(p.reference.runs) {
		return ErrStableMismatch
	}
	return nil
}

// run is the batch being emitted, from the plan being recorded or borrowed.
func (p *StablePlanner) run() *stableRun {
	if p.reference != nil {
		return &p.reference.runs[p.current-1]
	}
	return &p.runs[p.current-1]
}

// stableKind distinguishes runs whose features could coincide.
type stableKind uint64

const (
	stableBackground stableKind = iota + 1
	stableFill
	stablePattern
	stableExtruded
	stableDashed
)

// planSeed keys run hashes per process, so tile data cannot be crafted to make
// two different batches collide.
var planSeed = maphash.MakeSeed()

// featureHash accumulates the source features of one batch.
type featureHash struct {
	hash    maphash.Hash
	started bool
}

func (h *featureHash) add(feature int) {
	if !h.started {
		h.hash.SetSeed(planSeed)
		h.started = true
	}
	var buffer [8]byte
	binary.LittleEndian.PutUint64(buffer[:], uint64(feature))
	h.hash.Write(buffer[:])
}

// runHash identifies a batch: its kind, layer, cap and join, and features.
func runHash(kind stableKind, order int, lineCap, lineJoin string, features *featureHash) uint64 {
	var h maphash.Hash
	h.SetSeed(planSeed)
	var buffer [8]byte
	for _, value := range []uint64{uint64(kind), uint64(order)} {
		binary.LittleEndian.PutUint64(buffer[:], value)
		h.Write(buffer[:])
	}
	h.WriteString(lineCap)
	h.WriteByte(0)
	h.WriteString(lineJoin)
	h.WriteByte(0)
	if features != nil && features.started {
		binary.LittleEndian.PutUint64(buffer[:], features.hash.Sum64())
		h.Write(buffer[:])
	}
	return h.Sum64()
}
