// Package worldfield answers what the world *is* at a position: which danger
// band it sits in, which biome covers it, and what material grade the ground
// there yields. It holds no world state and reads no table — every input
// arrives as a plain Params value — so the simulation, the loader's coverage
// check, and the browser all reach one answer from one implementation.
//
// The browser's copy lives in web/src/game/worldfield.ts and must stay
// bit-identical to this one. Two rules keep that promise cheap:
//
//   - Every hash is 32-bit integer arithmetic, which JavaScript reproduces
//     exactly through Math.imul and the unsigned shift operators. A 64-bit
//     hash would need BigInt on the other side.
//   - Every float operation is +, -, *, /, or sqrt, all of which IEEE-754
//     specifies to the bit. math.Pow and JavaScript's Math.pow are *not*
//     required to agree, so the radial compression below is written as two
//     square roots rather than as an exponent, and the reward curve is
//     piecewise linear rather than a power.
package worldfield

import (
	"fmt"
	"math"
	"sort"
)

// Band is one radial danger ring, ordered outward from the hub.
type Band struct {
	ID, Name, PvP, MaterialGrade, Shape, Summary string
	Tier                                         int
	OuterRadius                                  float64
}

// Protected reports whether PvP damage is refused inside the band.
func (b Band) Protected() bool { return b.PvP == "off" || b.PvP == "restricted" }

// Safe reports whether the band offers the full service set — crafting,
// loadout mutation, respawn.
func (b Band) Safe() bool { return b.PvP == "off" }

// GradePoint is one vertex of the reward curve: at `At` of the world radius the
// ground is worth `Richness`. Between vertices the curve is linear, and the
// vertices are required to be convex, which is what makes the middle bands a
// route rather than the best farm.
type GradePoint struct{ At, Richness float64 }

// GradeThreshold is the richness a material grade starts at.
type GradeThreshold struct {
	ID   string
	Tier int
	At   float64
}

type GradeCurve struct {
	Points     []GradePoint
	Thresholds []GradeThreshold
}

// Params is everything the field is derived from. It is deliberately primitive:
// tuning builds one of these, and so does the client, so neither the field nor
// its coverage check depends on the table types.
type Params struct {
	Radius float64
	Seed   uint32
	// RegionCell is the biome lattice edge, measured in the compressed space
	// below rather than in world units.
	RegionCell float64
	// RegionJitter is how far a region site may wander inside its own cell, as
	// a fraction of the cell. Bounded below 1 so the five-cell search around a
	// query point always contains the nearest site.
	RegionJitter float64
	// RadialReference is the radius the compression leaves untouched. Positions
	// are pulled toward it by a fixed 3/4 power before the lattice is sampled,
	// so a biome is a broad radial swathe rather than a blob: every element
	// reaches every danger band, which is the two-axis rule biome-type and
	// radius-grade are built on.
	RadialReference float64
	// WarpCell and WarpAmplitude bend the lattice with value noise, so region
	// borders are ragged rather than straight Voronoi seams.
	WarpCell, WarpAmplitude float64
	// BlendWidth is how wide a border blend is, as a fraction of RegionCell.
	BlendWidth float64
	Biomes     []string
	Bands      []Band
	Grades     GradeCurve
}

// Biome is what covers a position: the region's biome, the nearest different
// one, and how far from their shared border the position sits. Blend is 0 on
// the border and 1 once the region is unambiguous, which is what a renderer
// cross-fades an ambient palette over.
type Biome struct {
	ID, Neighbour string
	Blend         float64
}

// Grade is what the ground is worth: the material grade it yields and the
// convex richness the yield itself scales with inside that grade.
type Grade struct {
	ID       string
	Tier     int
	Richness float64
}

// Region is the whole answer at a position, including a stable identity for
// the biome region itself so per-region parameters can be keyed on it.
type Region struct {
	ID     string
	Danger Band
	Biome  Biome
	Grade  Grade
}

type Field struct{ p Params }

// New copies the parameters and sorts the biome list, because a site's biome is
// drawn by index and the draw has to be stable against table ordering.
func New(p Params) *Field {
	field := &Field{p: p}
	field.p.Biomes = append([]string(nil), p.Biomes...)
	sort.Strings(field.p.Biomes)
	field.p.Bands = append([]Band(nil), p.Bands...)
	field.p.Grades.Points = append([]GradePoint(nil), p.Grades.Points...)
	field.p.Grades.Thresholds = append([]GradeThreshold(nil), p.Grades.Thresholds...)
	return field
}

func (f *Field) Params() Params { return f.p }

// DangerAt resolves the band containing a position. Anything past the rim
// resolves to the outermost band rather than to nothing, so a body pushed
// outside the world is never in an undefined zone.
func (f *Field) DangerAt(x, y float64) Band {
	distance := math.Sqrt(x*x + y*y)
	for _, band := range f.p.Bands {
		if distance <= band.OuterRadius {
			return band
		}
	}
	return f.p.Bands[len(f.p.Bands)-1]
}

// Protected reports whether PvP damage is refused at a position, and Safe
// whether the full service set is available there. Both exist so callers ask
// the field a question rather than comparing a radius of their own.
func (f *Field) Protected(x, y float64) bool { return f.DangerAt(x, y).Protected() }
func (f *Field) Safe(x, y float64) bool      { return f.DangerAt(x, y).Safe() }

// GradeAt walks the convex reward curve and reports the grade the richness has
// reached. The curve is what makes the rim disproportionately worth the walk:
// richness rises slowly across the Fringe and steeply toward the Deadlands, so
// the middle bands are a route rather than a destination.
func (f *Field) GradeAt(x, y float64) Grade {
	richness := f.RichnessAt(x, y)
	grade := Grade{Richness: richness}
	for _, threshold := range f.p.Grades.Thresholds {
		if richness >= threshold.At {
			grade.ID, grade.Tier = threshold.ID, threshold.Tier
		}
	}
	return grade
}

// RichnessAt is the reward curve itself, in [0,1].
func (f *Field) RichnessAt(x, y float64) float64 {
	points := f.p.Grades.Points
	if len(points) == 0 || f.p.Radius <= 0 {
		return 0
	}
	at := math.Sqrt(x*x+y*y) / f.p.Radius
	if at <= points[0].At {
		return points[0].Richness
	}
	for index := 1; index < len(points); index++ {
		previous, current := points[index-1], points[index]
		if at > current.At {
			continue
		}
		span := current.At - previous.At
		if span <= 0 {
			return current.Richness
		}
		return previous.Richness + (current.Richness-previous.Richness)*(at-previous.At)/span
	}
	return points[len(points)-1].Richness
}

// BiomeAt samples the noise-warped, radially compressed Voronoi lattice.
func (f *Field) BiomeAt(x, y float64) Biome {
	_, biome := f.regionAt(x, y)
	return biome
}

// RegionAt is the whole field at one position.
func (f *Field) RegionAt(x, y float64) Region {
	cell, biome := f.regionAt(x, y)
	return Region{
		ID:     fmt.Sprintf("region-%d:%d", cell.x, cell.y),
		Danger: f.DangerAt(x, y),
		Biome:  biome,
		Grade:  f.GradeAt(x, y),
	}
}

type cell struct{ x, y int64 }

// regionAt resolves the owning lattice site and the border blend around it.
//
// The search is five cells wide rather than three. A site may sit anywhere
// inside its own cell, so with the jitter bounded below one cell the nearest
// site to a point at a cell corner can still be two cells away on an axis; a
// three-cell search would occasionally pick the wrong region, and a region
// border that flickers is worse than one that costs twenty-five hashes.
func (f *Field) regionAt(x, y float64) (cell, Biome) {
	if len(f.p.Biomes) == 0 || f.p.RegionCell <= 0 {
		return cell{}, Biome{}
	}
	wx, wy := f.warp(f.compress(x, y))
	baseX, baseY := math.Floor(wx/f.p.RegionCell), math.Floor(wy/f.p.RegionCell)
	nearest, other := math.Inf(1), math.Inf(1)
	owner, neighbour := cell{}, ""
	ownerBiome := ""
	for dy := -2.0; dy <= 2; dy++ {
		for dx := -2.0; dx <= 2; dx++ {
			at := cell{int64(baseX + dx), int64(baseY + dy)}
			site, biome := f.site(at)
			distance := math.Sqrt((site.x-wx)*(site.x-wx) + (site.y-wy)*(site.y-wy))
			if distance < nearest {
				nearest, owner, ownerBiome = distance, at, biome
			}
		}
	}
	for dy := -2.0; dy <= 2; dy++ {
		for dx := -2.0; dx <= 2; dx++ {
			at := cell{int64(baseX + dx), int64(baseY + dy)}
			site, biome := f.site(at)
			if biome == ownerBiome {
				continue
			}
			distance := math.Sqrt((site.x-wx)*(site.x-wx) + (site.y-wy)*(site.y-wy))
			if distance < other {
				other, neighbour = distance, biome
			}
		}
	}
	blend := 1.0
	if width := f.p.BlendWidth * f.p.RegionCell; width > 0 && !math.IsInf(other, 1) {
		blend = clamp((other-nearest)/width, 0, 1)
	}
	return owner, Biome{ID: ownerBiome, Neighbour: neighbour, Blend: blend}
}

type point struct{ x, y float64 }

// site is one lattice site: its jittered position and the biome it owns. Both
// are drawn from the seed and the site's own coordinates alone, so the field is
// the same however it is sampled and in whatever order.
func (f *Field) site(at cell) (point, string) {
	ix, iy := uint32(int32(at.x)), uint32(int32(at.y))
	base := hash32(f.p.Seed^saltSite, ix, iy)
	jitter := clamp(f.p.RegionJitter, 0, 1)
	site := point{
		x: (float64(at.x) + 0.5 + (unit(hash32(base, ix, saltJitterX))-0.5)*jitter) * f.p.RegionCell,
		y: (float64(at.y) + 0.5 + (unit(hash32(base, iy, saltJitterY))-0.5)*jitter) * f.p.RegionCell,
	}
	index := int(unit(hash32(base, saltBiome, 0)) * float64(len(f.p.Biomes)))
	if index >= len(f.p.Biomes) {
		index = len(f.p.Biomes) - 1
	}
	return site, f.p.Biomes[index]
}

// compress pulls a position toward the reference radius by a fixed 3/4 power,
// which is written as two square roots because that is the only way Go and
// JavaScript are guaranteed to agree on it to the bit.
//
// The effect is the whole reason biome and grade are independent axes: a region
// becomes a swathe that narrows toward the hub and widens toward the rim rather
// than a blob at one radius, so every element is reachable from every band.
func (f *Field) compress(x, y float64) (float64, float64) {
	distance := math.Sqrt(x*x + y*y)
	reference := f.p.RadialReference
	if distance < 1 || reference <= 0 {
		return x, y
	}
	ratio := reference / distance
	scale := math.Sqrt(ratio * math.Sqrt(ratio)) // ratio ^ (3/4)
	return x * scale, y * scale
}

// warp bends the lattice with two octaves of value noise so region borders read
// as coastlines rather than as straight Voronoi seams.
func (f *Field) warp(x, y float64) (float64, float64) {
	if f.p.WarpCell <= 0 || f.p.WarpAmplitude == 0 {
		return x, y
	}
	u, v := x/f.p.WarpCell, y/f.p.WarpCell
	dx := f.noise(saltWarpX, u, v) + 0.5*f.noise(saltWarpX2, u*2, v*2)
	dy := f.noise(saltWarpY, u, v) + 0.5*f.noise(saltWarpY2, u*2, v*2)
	return x + f.p.WarpAmplitude*dx, y + f.p.WarpAmplitude*dy
}

// noise is ordinary bilinear value noise in [-1,1], smoothstepped so the warp
// has no lattice-aligned creases in it.
func (f *Field) noise(salt uint32, x, y float64) float64 {
	gx, gy := math.Floor(x), math.Floor(y)
	sx, sy := smooth(x-gx), smooth(y-gy)
	ix, iy := uint32(int32(gx)), uint32(int32(gy))
	seed := f.p.Seed ^ salt
	n00, n10 := unit(hash32(seed, ix, iy)), unit(hash32(seed, ix+1, iy))
	n01, n11 := unit(hash32(seed, ix, iy+1)), unit(hash32(seed, ix+1, iy+1))
	top := n00 + (n10-n00)*sx
	bottom := n01 + (n11-n01)*sx
	return (top+(bottom-top)*sy)*2 - 1
}

const (
	saltSite    uint32 = 0x2545f491
	saltJitterX uint32 = 0x51ed270b
	saltJitterY uint32 = 0x2f9277b5
	saltBiome   uint32 = 0x9e3779b1
	saltWarpX   uint32 = 0x1b56c4e9
	saltWarpY   uint32 = 0x7f4a7c15
	saltWarpX2  uint32 = 0x27d4eb2f
	saltWarpY2  uint32 = 0x165667b1
)

// hash32 is a three-input integer hash. Every step is 32-bit multiply, xor, and
// logical shift, which is exactly the set JavaScript reproduces bit for bit.
func hash32(a, b, c uint32) uint32 {
	h := a * 0x9e3779b1
	h ^= b + 0x85ebca6b + (h << 6) + (h >> 2)
	h ^= c + 0xc2b2ae35 + (h << 6) + (h >> 2)
	h ^= h >> 16
	h *= 0x7feb352d
	h ^= h >> 15
	h *= 0x846ca68b
	h ^= h >> 16
	return h
}

// unit maps a hash onto [0,1). The divisor is a power of two, so the division
// is exact and needs no rounding agreement between the two languages.
func unit(h uint32) float64 { return float64(h) / 4294967296 }

func smooth(t float64) float64 { return t * t * (3 - 2*t) }

func clamp(value, low, high float64) float64 {
	return math.Max(low, math.Min(high, value))
}
