package retained

import "github.com/rubiojr/whereami/pkg/vecmap/scene"

// Range selects contiguous draw records (not vertices) from a retained fragment.
// Transform replaces their local transform slot for one instance. Supply ranges
// in exact compositing order: no layer sorting, coverage or wrap inference occurs.
type Range struct {
	Key          string
	First, Count int
	Transform    int
}

// Snapshot assembles owned immutable scene metadata in caller order, borrowing
// validated geometry/pixels. Repeated instances share resource IDs. All resources
// of each selected fragment are included once in first-selection order, even if
// the selected ranges use only some of them. No deep geometry scan/copy occurs.
// Callers own frame transform storage/validation and selection of fallback tiles,
// label visibility, pattern wrap phases and tile clips in their prepared draws.
// Prior snapshots survive later Apply calls. Empty selection yields an empty scene.
func (s *Store) Snapshot(order []Range) (*scene.Scene, error) {
	if s.parts == nil {
		return nil, ErrInput
	}
	if len(order) > s.limits.Draws {
		return nil, ErrLimit
	}
	count := 0
	for _, r := range order {
		if len(r.Key) == 0 || len(r.Key) > 256 {
			return nil, ErrInput
		}
		p := s.parts[r.Key]
		if p == nil || r.First < 0 || r.Count <= 0 || r.First > len(p.scene.Draws) || r.Count > len(p.scene.Draws)-r.First || r.Transform < 0 {
			return nil, ErrInput
		}
		if r.Count > s.limits.Draws-count {
			return nil, ErrLimit
		}
		count += r.Count
	}
	out := &scene.Scene{Draws: make([]scene.Draw, 0, count)}
	seen := make(map[string]bool)
	for _, r := range order {
		p := s.parts[r.Key]
		if !seen[r.Key] {
			out.Meshes = append(out.Meshes, p.scene.Meshes...)
			out.Textures = append(out.Textures, p.scene.Textures...)
			seen[r.Key] = true
		}
		for _, original := range p.scene.Draws[r.First : r.First+r.Count] {
			draw := original
			draw.Transform = r.Transform
			out.Draws = append(out.Draws, draw)
		}
	}
	return out, nil
}
