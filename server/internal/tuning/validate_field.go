package tuning

import (
	"math"
)

// validateField holds the world field to its two design contracts.
//
// The first is the reward curve. It has to be *convex* — each segment steeper
// than the one before — because that is what makes the rim disproportionately
// worth reaching and the middle bands a route rather than the best farm. A
// concave or straight curve would make the Frontier the efficient place to
// live, which is the failure world.md names.
//
// The second is coverage. The biome field is generated from a seed rather than
// authored, so nothing guarantees by construction that every element is
// reachable from every band. Rather than trust a seed, the loader samples the
// field and refuses one that leaves a gap: "geography never hard-locks a build"
// is checked here or it is not true.
func (t *Tables) validateField(r *report) {
	field := t.World.Field
	r.require(field.RegionCell > 0, "world: field.region_cell must be positive; it is the biome lattice")
	r.require(field.RegionJitter >= 0 && field.RegionJitter < 1,
		"world: field.region_jitter %g must lie in [0,1); a site that leaves its own cell would be missed by the lattice search", field.RegionJitter)
	r.require(field.RadialReference > 0 && field.RadialReference <= t.World.Radius,
		"world: field.radial_reference %g must lie inside the world radius %g", field.RadialReference, t.World.Radius)
	r.require(field.WarpCell > 0, "world: field.warp_cell must be positive")
	r.require(field.WarpAmplitude >= 0, "world: field.warp_amplitude must not be negative")
	r.require(field.BlendWidth > 0 && field.BlendWidth <= 1,
		"world: field.blend_width %g must lie in (0,1] of a region cell; borders blend rather than seam", field.BlendWidth)
	// The lattice is sampled in the compressed space, so a cell wider than the
	// compressed world would collapse the whole map into one or two regions.
	r.require(field.RegionCell < t.World.Radius,
		"world: field.region_cell %g is not smaller than the world radius %g; the world would hold barely one region", field.RegionCell, t.World.Radius)
	r.require(len(t.Biomes) > 0, "world: the field has no biomes to place")
	t.validateGradeCurve(r)
	t.validateCoverage(r)
}

func (t *Tables) validateGradeCurve(r *report) {
	curve := t.World.Field.GradeCurve
	if !r.require(len(curve.Points) >= 2, "world: field.grade_curve.points needs at least two vertices") {
		return
	}
	first, last := curve.Points[0], curve.Points[len(curve.Points)-1]
	r.require(first[0] == 0 && first[1] == 0, "world: field.grade_curve must start at [0,0]; the hub is worth nothing")
	r.require(last[0] == 1 && last[1] == 1, "world: field.grade_curve must end at [1,1]; the rim is the full reward")
	previousSlope := math.Inf(-1)
	for index := 1; index < len(curve.Points); index++ {
		before, point := curve.Points[index-1], curve.Points[index]
		if !r.require(point[0] > before[0], "world: field.grade_curve vertex %d is not further out than the one before it", index) {
			continue
		}
		r.require(point[1] >= before[1], "world: field.grade_curve vertex %d rewards less than the one before it; reward never falls with risk", index)
		slope := (point[1] - before[1]) / (point[0] - before[0])
		r.require(slope >= previousSlope-1e-9,
			"world: field.grade_curve segment %d rises at %.3f, flatter than the %.3f before it; the curve must stay convex or the middle bands become the best farm",
			index, slope, previousSlope)
		previousSlope = slope
	}
	if !r.require(len(curve.Thresholds) > 0, "world: field.grade_curve declares no grade thresholds") {
		return
	}
	previousAt, previousTier := math.Inf(-1), 0
	for index, threshold := range curve.Thresholds {
		grade, known := t.Materials.Grades[threshold.Grade]
		if !r.require(known, "world: field.grade_curve threshold %d references unknown material grade %q", index, threshold.Grade) {
			continue
		}
		r.require(threshold.At >= 0 && threshold.At <= 1, "world: field.grade_curve threshold %q starts at %g, outside the [0,1] curve", threshold.Grade, threshold.At)
		r.require(threshold.At > previousAt || index == 0, "world: field.grade_curve threshold %q does not start further out than the one before it", threshold.Grade)
		r.require(grade.Tier == previousTier+1,
			"world: field.grade_curve threshold %q has tier %d, want %d; geographic grades must climb one tier at a time from Common", threshold.Grade, grade.Tier, previousTier+1)
		if index == 0 {
			r.require(threshold.At == 0, "world: field.grade_curve's first threshold must start at 0, or the ground closest to the hub would yield nothing")
		}
		previousAt, previousTier = threshold.At, grade.Tier
	}
	// The band table and the curve are two statements of the same fact, and a
	// disagreement would show a player one grade in the HUD and hand them
	// another. Sampling just inside each band's outer edge is what the band's
	// declared grade actually claims.
	field := t.Field()
	for _, band := range t.World.DangerBands {
		if band.MaterialGrade == "" {
			continue
		}
		at := band.OuterRadius * (1 - 1e-9)
		resolved := field.GradeAt(at, 0)
		r.require(resolved.ID == band.MaterialGrade,
			"world: danger band %q declares material grade %q but the reward curve yields %q at its outer edge", band.ID, band.MaterialGrade, resolved.ID)
	}
}

func (t *Tables) validateCoverage(r *report) {
	rule := t.World.Field.Coverage
	if !r.require(rule.Resolution >= 2, "world: field.coverage.resolution must sample at least a two-by-two lattice") {
		return
	}
	r.require(rule.MinimumShare > 0 && rule.MinimumShare < 1/float64(max(len(t.Biomes), 1)),
		"world: field.coverage.minimum_share %g must be a positive fraction below an even split of %d biomes, or no seed could satisfy it", rule.MinimumShare, len(t.Biomes))
	r.require(rule.MinimumSamples > 0, "world: field.coverage.minimum_samples must be positive")
	if len(t.Biomes) == 0 || t.World.Radius <= 0 || len(t.World.DangerBands) == 0 {
		return
	}
	coverage := t.Field().Sample(rule.Resolution)
	for _, problem := range coverage.Problems(t.FieldBands(), sortedKeys(t.Biomes), rule.MinimumShare, rule.MinimumSamples) {
		r.addf("%s", problem)
	}
}
