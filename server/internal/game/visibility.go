package game

// terrainOccluded reports whether solid world geometry crosses the direct line
// between two points. The collision component is the source of truth for both
// projectile cover and sight, so authored fixtures and player-raised wall
// segments cannot disagree about what they block.
//
// Only standing geometry occludes. Destruction and expiry remove an item from
// gameplay immediately; its graceful deletion tail is visual feedback, not
// cover that can still hide a target.
func (w *World) terrainOccluded(from, to Vec) bool {
	if from == to {
		return false
	}
	for _, item := range w.worldItems {
		if item != nil && item.intersectsSegment(from, to, 0) {
			return true
		}
	}
	return false
}

// targetVisible is the authoritative visibility rule for automatic targeting.
// Terrain blocks the sightline, while concealing fields hide only a body they
// cover completely and preserve their close-range reveal rule. Manual ground
// placement deliberately does not call this: walls stop sight and aimed
// projectiles, not area effects placed onto the ground.
func (w *World) targetVisible(from, to Vec, extent float64) bool {
	return !w.terrainOccluded(from, to) && !w.concealed(from, to, extent)
}
