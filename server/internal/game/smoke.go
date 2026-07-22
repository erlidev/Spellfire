package game

// smokeCircle is one lobe of the visible and authoritative smoke shape. The
// five fixed lobes keep the cloud cheap to draw and cheap to transmit: the wire
// carries one field collider and both server and shader expand it identically.
type smokeCircle struct {
	Center Vec
	Radius float64
}

func smokeCircles(center Vec, radius float64) []smokeCircle {
	const offsetScale = 0.4
	const outerRadiusScale = 0.6
	return []smokeCircle{
		{Center: center, Radius: radius * 0.62},
		{Center: center.Add(Vec{X: radius * offsetScale}), Radius: radius * outerRadiusScale},
		{Center: center.Add(Vec{X: -radius * offsetScale}), Radius: radius * outerRadiusScale},
		{Center: center.Add(Vec{Y: radius * offsetScale}), Radius: radius * outerRadiusScale},
		{Center: center.Add(Vec{Y: -radius * offsetScale}), Radius: radius * outerRadiusScale},
	}
}

func pointInSmokeCircle(point Vec, circle smokeCircle) bool {
	return point.Sub(circle.Center).LengthSq() <= circle.Radius*circle.Radius
}

// smokeOccluded applies the special rule for an enterable opaque occluder. If
// the viewer is inside one or more lobes, their union is the visible pocket and
// every point outside it is occluded. From outside, crossing any lobe blocks.
func smokeOccluded(from, to Vec, circles []smokeCircle) bool {
	inside := false
	toInsideViewerCircle := false
	for _, circle := range circles {
		if !pointInSmokeCircle(from, circle) {
			continue
		}
		inside = true
		toInsideViewerCircle = toInsideViewerCircle || pointInSmokeCircle(to, circle)
	}
	if inside {
		return !toInsideViewerCircle
	}
	for _, circle := range circles {
		if segmentIntersectsCircle(from, to, circle.Center, circle.Radius) {
			return true
		}
	}
	return false
}
