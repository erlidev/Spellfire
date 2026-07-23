package game

import "math"

// sightOccluders is the resolved set of sight-blockers for one visibility pass:
// vision-blocking terrain and smoke clouds. Collecting it once per viewer-send
// keeps the hot visibility loop off the full world-item slice — most of which is
// non-occluding trees — and off the deployable map.
//
// Smoke is now an occluder in its own right: a cloud casts a shadow, hiding what
// stands behind it exactly as a wall does, rather than only swallowing what it
// covers. The one thing it does not do that terrain does is hide the viewer's
// own rounds, and that exemption is applied by the caller, not here.
type sightOccluders struct {
	terrain []*Entity
	smoke   []*Deployable
}

// collectOccluders gathers the standing sight-blockers once. Destroyed or
// expired terrain (Alive is cleared on the same transition as its collision) and
// fading clouds stop occluding immediately: the graceful-removal tail is visual
// feedback, not cover that can still hide a target.
func (w *World) collectOccluders() sightOccluders {
	occ := sightOccluders{}
	for _, item := range w.worldItems {
		if item != nil && item.Alive && item.OccludesVision {
			occ.terrain = append(occ.terrain, item)
		}
	}
	for _, id := range sortedDeployableIDs(w.deployables) {
		cloud := w.deployables[id]
		if !cloud.Deleting && cloud.Field.Conceals && cloud.Field.Radius > 0 {
			occ.smoke = append(occ.smoke, cloud)
		}
	}
	return occ
}

// rayBlocked reports whether the direct segment is stopped by any occluder.
// Smoke is included only when includeSmoke is set, and clouds in skip — the ones
// the viewer is standing inside — are ignored, because being inside a cloud is
// handled by the reveal circle rather than by shadowing the viewer's every ray.
func (o sightOccluders) rayBlocked(from, to Vec, includeSmoke bool, skip []*Deployable) bool {
	for _, item := range o.terrain {
		if item.overlapsSegment(from, to, 0) {
			return true
		}
	}
	if includeSmoke {
		for _, cloud := range o.smoke {
			if containsCloud(skip, cloud) {
				continue
			}
			if segmentCircle(from, to, cloud.Position, cloud.Field.Radius) {
				return true
			}
		}
	}
	return false
}

// clearLine reports whether any part of a target's silhouette — a circle of
// radius extent at `at` — has an unobstructed line from `from`. Occlusion is a
// property of the whole body, not its centre: a target with one edge in the open
// is visible. The direct centre line is tested first, so an unobstructed target
// costs a single ray; only when the centre is blocked are the silhouette samples
// tested. Those are the two tangent edges — which catch a body peeking past a
// wall corner — and the near cap toward the viewer, which catches one half-out
// of a round cloud's rim. The far cap is never the first part seen, so it is not
// sampled.
func (o sightOccluders) clearLine(from, at Vec, extent float64, includeSmoke bool, skip []*Deployable) bool {
	if !o.rayBlocked(from, at, includeSmoke, skip) {
		return true
	}
	if extent <= 0 {
		return false
	}
	dir := at.Sub(from)
	if dir.LengthSq() < 1e-9 {
		return true
	}
	unit := dir.Normalized()
	for _, offset := range []Vec{{-unit.Y, unit.X}, {unit.Y, -unit.X}, {-unit.X, -unit.Y}} {
		if !o.rayBlocked(from, at.Add(offset.Mul(extent)), includeSmoke, skip) {
			return true
		}
	}
	return false
}

// containing returns the concealing clouds the viewer stands inside and the
// largest reveal radius among them. Inside smoke a body sees only a small circle
// centred on itself: everything past the reveal radius is swallowed, and
// standing at the rim lets it see just past the cloud's edge, because the circle
// is measured from the body rather than from the smoke's boundary.
func (o sightOccluders) containing(from Vec) ([]*Deployable, float64) {
	var inside []*Deployable
	reveal := 0.0
	for _, cloud := range o.smoke {
		if from.Sub(cloud.Position).LengthSq() <= cloud.Field.Radius*cloud.Field.Radius {
			inside = append(inside, cloud)
			if cloud.Field.RevealRadius > reveal {
				reveal = cloud.Field.RevealRadius
			}
		}
	}
	return inside, reveal
}

// visible is the authoritative rule for a target the viewer does not own:
// terrain and smoke both cast shadows, and a viewer inside smoke can see nothing
// whose silhouette does not reach into its reveal circle.
func (o sightOccluders) visible(from, at Vec, extent float64) bool {
	inside, reveal := o.containing(from)
	if len(inside) > 0 {
		// Only what reaches into the reveal circle is seen at all; the ray test
		// below still applies terrain and any other cloud within that circle.
		if math.Sqrt(at.Sub(from).LengthSq())-extent > reveal {
			return false
		}
	}
	return o.clearLine(from, at, extent, true, inside)
}

// visibleTerrain is the rule for the viewer's own entities: terrain hides them,
// smoke never does. A cloud that hid a body's own rounds would read as a wall it
// could not shoot through — the round vanishing at the edge and reappearing past
// it is a vision rule pretending to be collision.
func (o sightOccluders) visibleTerrain(from, at Vec, extent float64) bool {
	return o.clearLine(from, at, extent, false, nil)
}

func containsCloud(clouds []*Deployable, cloud *Deployable) bool {
	for _, candidate := range clouds {
		if candidate == cloud {
			return true
		}
	}
	return false
}

// terrainOccluded reports whether vision-blocking terrain crosses the direct
// point-to-point line. It is the single-shot form used by tooling and tests; the
// hot paths use a collected sightOccluders instead.
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
