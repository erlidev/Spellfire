package game

import "math"

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

// visionOccluded extends standing terrain with authored field occluders. A
// deployable contributes its field circle, which lets smoke participate in the
// same authoritative LOS rule without pretending it is solid collision.
func (w *World) visionOccluded(from, to Vec) bool {
	if w.terrainOccluded(from, to) {
		return true
	}
	for _, field := range w.deployables {
		definition, ok := w.tuning.Tables.Entities[field.Kind]
		if !ok || !definition.OccludesVision || field.Deleting || !field.Alive || field.Field.Radius <= 0 {
			continue
		}
		// The authored reveal gap still permits contact fighting from inside a
		// cloud. Beyond it, the cloud's exit boundary blocks sight like any
		// other crossed portion of the field.
		if from.Sub(field.Position).LengthSq() < field.Field.Radius*field.Field.Radius &&
			to.Sub(from).LengthSq() <= field.Field.RevealRadius*field.Field.RevealRadius {
			continue
		}
		if segmentIntersectsCircle(from, to, field.Position, field.Field.Radius) {
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
// preserves any exposed part of the target and concealment preserves smoke's
// close-range reveal rule. Manual ground placement deliberately does not call
// this: walls stop sight and aimed projectiles, not area effects placed onto
// the ground.
func (w *World) targetVisible(from, to Vec, extent float64) bool {
	return w.anyPartVisible(from, to, extent) && !w.concealed(from, to, extent)
}
