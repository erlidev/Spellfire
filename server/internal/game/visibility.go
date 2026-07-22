package game

// terrainOccluded reports whether vision-blocking world geometry crosses the
// direct line between two points. The entity attribute opts a collider into
// sight blocking, so a tree can remain physical without becoming visual cover;
// opted-in fixtures and player-raised walls reuse their collision geometry.
//
// Only standing geometry occludes. Destruction and expiry remove an item from
// gameplay immediately; its graceful deletion tail is visual feedback, not
// cover that can still hide a target.
func (w *World) terrainOccluded(from, to Vec) bool {
	if from == to {
		return false
	}
	for _, item := range w.worldItems {
		if item != nil && item.OccludesVision && item.intersectsSegment(from, to, 0) {
			return true
		}
	}
	return false
}

<<<<<<< HEAD
// visionOccluded extends standing terrain with authored field occluders. Smoke
// expands into the same five enterable circles drawn by the client; other field
// occluders use their authored circle directly.
func (w *World) visionOccluded(from, to Vec) bool {
	if w.terrainOccluded(from, to) {
		return true
	}
	for _, field := range w.deployables {
		definition, ok := w.tuning.Tables.Entities[field.Kind]
		if !ok || !definition.OccludesVision || field.Deleting || !field.Alive || field.Field.Radius <= 0 {
			continue
		}
		circles := []smokeCircle{{Center: field.Position, Radius: field.Field.Radius}}
		if field.Field.Conceals {
			circles = smokeCircles(field.Position, field.Field.Radius)
		}
		if smokeOccluded(from, to, circles) {
			return true
		}
	}
	return false
}

func segmentIntersectsCircle(from, to, center Vec, radius float64) bool {
	delta := to.Sub(from)
	lengthSq := delta.LengthSq()
	if lengthSq <= 0 {
		return center.Sub(from).LengthSq() <= radius*radius
	}
	t := center.Sub(from).X*delta.X + center.Sub(from).Y*delta.Y
	// Ignore the exact starting point: a viewer touching a boundary and looking
	// away from it has not crossed the occluder. Looking inward still intersects
	// immediately after this epsilon.
	t = math.Max(0.000001, math.Min(1, t/lengthSq))
	return center.Sub(from.Add(delta.Mul(t))).LengthSq() <= radius*radius
}

// anyPartVisible samples the target silhouette instead of treating its centre
// as the entire body. Sending the body when even one perimeter point has a
// clear ray lets the client shader mask precisely the portion behind cover.
func (w *World) anyPartVisible(from, center Vec, extent float64) bool {
	if !w.visionOccluded(from, center) {
		return true
	}
	if extent <= 0 {
		return false
	}
	const silhouetteSamples = 32
	for sample := 0; sample < silhouetteSamples; sample++ {
		angle := float64(sample) * 2 * math.Pi / silhouetteSamples
		point := center.Add(Vec{X: math.Cos(angle) * extent, Y: math.Sin(angle) * extent})
		if !w.visionOccluded(from, point) {
			return true
		}
	}
	return false
}

// targetVisible is the authoritative visibility rule for automatic targeting.
// Terrain and authored fields block the sightline, while perimeter sampling
// preserves any exposed part of the target. An enterable field handles its
// interior pocket in smokeOccluded. Manual ground placement deliberately does
// not call this: walls stop sight and aimed projectiles, not area effects placed
// onto the ground.
func (w *World) targetVisible(from, to Vec, extent float64) bool {
	return w.anyPartVisible(from, to, extent)
=======
// targetVisible is the authoritative visibility rule for automatic targeting.
// Terrain blocks the sightline, while concealing fields hide only a body they
// cover completely and preserve their close-range reveal rule. Manual ground
// placement deliberately does not call this: walls stop sight and aimed
// projectiles, not area effects placed onto the ground.
func (w *World) targetVisible(from, to Vec, extent float64) bool {
	return !w.terrainOccluded(from, to) && !w.concealed(from, to, extent)
>>>>>>> b44abae (Revert "fix los")
}
