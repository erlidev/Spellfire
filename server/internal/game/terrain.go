package game

import (
	"math"

	"spellfire/server/internal/tuning"
)

// This file owns the macro shape of the world: the concentric impassable ridge
// belts that funnel radial travel through a handful of staggered passes, and the
// biome vocabulary the scatter and the belts are drawn from. It is deliberately
// server-only — nothing here is shared bit-for-bit with the browser the way the
// world field is, because the client renders the entities the generator emits
// rather than recomputing where they are. That frees the belt maths to use
// trigonometry, which the field is forbidden.
//
// A belt is an annulus at one of belts.Radii, its centre wobbling with the angle
// so the ridge reads as a natural formation rather than a drawn circle. Each
// belt is solid except for PassesPerBelt angular gaps, and the gap centres are
// staggered belt-to-belt from the belt seed. Two properties follow, and both are
// held by tests over the live generator:
//
//   - No straight radial line is clear through every belt at once, so crossing
//     the world means threading offset passes and detouring between them. That
//     detour is what turns a three-minute straight walk into a five-minute
//     journey without adding empty distance.
//   - Between any two belts the annulus is fully open and every belt has a pass,
//     so nothing is ever sealed: the walkable space is one connected region.

// terrainSetAt is the scatter/barrier vocabulary of the biome underfoot, or the
// default set where a biome declares none.
func (w *World) terrainSetAt(pos Vec) tuning.BiomeTerrain {
	terrain := w.tuning.Tables.World.Terrain
	biome := w.tuning.Field.BiomeAt(pos.X, pos.Y).ID
	if set, ok := terrain.Biomes[biome]; ok {
		return set
	}
	return terrain.Default
}

// beltCenterRadius is a belt's centre radius at one angle, wobbled so the ridge
// is not a perfect circle.
func (w *World) beltCenterRadius(index int, theta float64) float64 {
	belts := w.tuning.Tables.World.Terrain.Belts
	phase := beltPhase(belts.Seed+0x51, index)
	return belts.Radii[index] + belts.Waviness*math.Sin(belts.WaveCount*theta+phase)
}

// inPass reports whether an angle falls inside one of a belt's gaps, within the
// given half-width. The gap centres are evenly spaced and offset per belt, which
// is the whole staggering mechanism.
func (w *World) inPass(index int, theta, halfAngle float64) bool {
	belts := w.tuning.Tables.World.Terrain.Belts
	phase := beltPhase(belts.Seed, index)
	step := 2 * math.Pi / float64(belts.PassesPerBelt)
	for j := 0; j < belts.PassesPerBelt; j++ {
		if angularDistance(theta, phase+float64(j)*step) <= halfAngle {
			return true
		}
	}
	return false
}

// beltBarrierAt reports the impassable archetype covering a position, if any. A
// position inside a belt annulus is barrier unless it sits in one of that belt's
// passes.
func (w *World) beltBarrierAt(pos Vec) (string, bool) {
	belts := w.tuning.Tables.World.Terrain.Belts
	if len(belts.Radii) == 0 {
		return "", false
	}
	r := math.Sqrt(pos.LengthSq())
	theta := math.Atan2(pos.Y, pos.X)
	half := belts.Thickness / 2
	for index := range belts.Radii {
		if math.Abs(r-w.beltCenterRadius(index, theta)) > half {
			continue
		}
		if w.inPass(index, theta, belts.PassHalfAngle) {
			return "", false
		}
		return w.terrainSetAt(pos).Barrier, true
	}
	return "", false
}

// walkableAt reports whether a body could stand at a position as far as the
// macro structure is concerned — that is, whether it is outside every belt or
// inside a pass. Scatter is avoidable and does not count. It is the predicate the
// traversal and connectivity tests build their walkable grid from.
func (w *World) walkableAt(pos Vec) bool {
	_, barrier := w.beltBarrierAt(pos)
	return !barrier
}

// routeClearsAt reports whether a position sits in the cleared mouth of a pass,
// where the scatter thins so a chokepoint reads as an exposed lane rather than
// as more cover. The channel is wider than the impassable gap so the opening is
// visible from either side of the belt.
func (w *World) routeClearsAt(pos Vec) bool {
	terrain := w.tuning.Tables.World.Terrain
	belts := terrain.Belts
	if len(belts.Radii) == 0 || terrain.Routes.HalfAngleScale <= 0 {
		return false
	}
	r := math.Sqrt(pos.LengthSq())
	theta := math.Atan2(pos.Y, pos.X)
	reach := belts.Thickness/2 + belts.Cell
	for index := range belts.Radii {
		if math.Abs(r-w.beltCenterRadius(index, theta)) > reach {
			continue
		}
		if w.inPass(index, theta, belts.PassHalfAngle*terrain.Routes.HalfAngleScale) {
			return true
		}
	}
	return false
}

// maxScatterExtent is the widest any scatter archetype in any biome can reach.
// The jitter clamp uses it rather than the per-site archetype, so a site can
// hold any archetype without a neighbour ever overlapping it.
func (w *World) maxScatterExtent() float64 {
	terrain := w.tuning.Tables.World.Terrain
	widest := 0.0
	consider := func(set tuning.BiomeTerrain) {
		for _, scatter := range set.Scatter {
			definition, ok := w.tuning.Tables.Entities[scatter.Entity]
			if !ok {
				continue
			}
			shape := newEntity("", "", Vec{}, definition, EntityOverrides{})
			widest = math.Max(widest, shape.boundingRadius()+scatter.RadiusSpread)
		}
	}
	consider(terrain.Default)
	for _, set := range terrain.Biomes {
		consider(set)
	}
	return widest
}

// angularDistance is the smallest absolute angle between two headings.
func angularDistance(a, b float64) float64 {
	return math.Abs(math.Atan2(math.Sin(a-b), math.Cos(a-b)))
}

// beltPhase draws a stable angle in [0, 2π) from the belt seed and an index, so
// two belts stagger their passes without any shared state.
func beltPhase(seed uint64, index int) float64 {
	h := seed ^ (uint64(index)+1)*0x9e3779b97f4a7c15
	h = (h ^ (h >> 30)) * 0xbf58476d1ce4e5b9
	h = (h ^ (h >> 27)) * 0x94d049bb133111eb
	h ^= h >> 31
	return float64(h>>11) / float64(uint64(1)<<53) * 2 * math.Pi
}
