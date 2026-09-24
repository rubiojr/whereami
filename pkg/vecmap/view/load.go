package view

// LoadOrder prioritizes immediate coarse parents before detailed targets, retaining
// first-seen order and removing duplicates. Inputs are bounded canonical covers;
// loading, readiness and cancellation remain caller policy.
func LoadOrder(targets []TileID) []TileID {
	order := make([]TileID, 0, len(targets)*2)
	seen := make(map[TileID]bool, len(targets)*2)
	for _, tile := range targets {
		if parent, ok := tile.Parent(); ok && !seen[parent] {
			seen[parent] = true
			order = append(order, parent)
		}
	}
	for _, tile := range targets {
		if !seen[tile] {
			seen[tile] = true
			order = append(order, tile)
		}
	}
	return order
}
