package placement

import "math"

type collisionCell struct{ x, y int }

type collisionGrid struct {
	width, height float64
	remaining     int
	occupied      map[collisionCell][]Box
}

func (g *collisionGrid) spend() error {
	if g.remaining == 0 {
		return ErrCollisionLimit
	}
	g.remaining--
	return nil
}

func (g *collisionGrid) partIntersects(part CollisionPart) (bool, error) {
	if !part.Visible || part.AllowsOverlap {
		return false, nil
	}
	collision := false
	err := g.forEachCell(part.Box, func(cell collisionCell) (bool, error) {
		for _, occupiedBox := range g.occupied[cell] {
			if err := g.spend(); err != nil {
				return false, err
			}
			if BoxesIntersect(part.Box, occupiedBox) {
				collision = true
				return false, nil
			}
		}
		return true, nil
	})
	return collision, err
}

func (g *collisionGrid) add(box Box) error {
	return g.forEachCell(box, func(cell collisionCell) (bool, error) {
		g.occupied[cell] = append(g.occupied[cell], box)
		return true, nil
	})
}

func (g *collisionGrid) forEachCell(box Box, visit func(collisionCell) (bool, error)) error {
	left, top := math.Floor(max(0, box.Left)/CollisionCellSize), math.Floor(max(0, box.Top)/CollisionCellSize)
	right, bottom := math.Floor(min(g.width, box.Right)/CollisionCellSize), math.Floor(min(g.height, box.Bottom)/CollisionCellSize)
	// Check emptiness before conversion: fully offscreen finite coordinates can
	// be much larger than int even though the viewport itself is bounded.
	if left > right || top > bottom {
		return nil
	}
	leftCell, topCell := int(left), int(top)
	rightCell, bottomCell := int(right), int(bottom)
	for y := topCell; y <= bottomCell; y++ {
		for x := leftCell; x <= rightCell; x++ {
			if err := g.spend(); err != nil {
				return err
			}
			more, err := visit(collisionCell{x: x, y: y})
			if err != nil || !more {
				return err
			}
		}
	}
	return nil
}

func BoxesIntersect(first, second Box) bool {
	return first.Left < second.Right && first.Right > second.Left &&
		first.Top < second.Bottom && first.Bottom > second.Top
}
