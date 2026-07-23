package worldfield

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// goldenPath is shared with the browser. The client's field is a hand-written
// mirror of this package rather than a generated one, so the only thing that
// can hold the two together is a fixture both of them check themselves against.
const goldenPath = "../../../testdata/worldfield.json"

// Golden is the cross-language fixture: the parameters the field was built
// from, and what it answered at a fixed set of positions.
type Golden struct {
	Params  Params         `json:"params"`
	Samples []GoldenSample `json:"samples"`
}

type GoldenSample struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Region    string  `json:"region"`
	Danger    string  `json:"danger"`
	Biome     string  `json:"biome"`
	Neighbour string  `json:"neighbour"`
	Blend     float64 `json:"blend"`
	Grade     string  `json:"grade"`
	Tier      int     `json:"tier"`
	Richness  float64 `json:"richness"`
}

// testParams mirrors the shipped world.json. This package must not import
// tuning — tuning imports it, for the coverage check — so the fixture's inputs
// are stated here and TestShippedTablesBuildTheGoldenField in the tuning suite
// holds them to the table.
func testParams() Params {
	return Params{
		Radius: 45000, Seed: 2676017207,
		RegionCell: 9000, RegionJitter: 0.72, RadialReference: 22500,
		WarpCell: 14000, WarpAmplitude: 2600, BlendWidth: 0.16,
		Biomes: []string{"arcane-hollow", "emberlands", "rimeshelf", "stonereach", "stormflats"},
		Bands: []Band{
			{ID: "hub", Name: "Central hub", PvP: "off", Tier: 0, OuterRadius: 900},
			{ID: "fringe", Name: "Fringe", PvP: "restricted", MaterialGrade: "common", Tier: 1, OuterRadius: 9000},
			{ID: "frontier", Name: "Frontier", PvP: "on", MaterialGrade: "uncommon", Tier: 2, OuterRadius: 31500},
			{ID: "deadlands", Name: "Deadlands", PvP: "full", MaterialGrade: "rare", Tier: 3, OuterRadius: 45000},
		},
		Grades: GradeCurve{
			Points: []GradePoint{{0, 0}, {0.2, 0.055}, {0.45, 0.19}, {0.7, 0.526}, {0.9, 0.83}, {1, 1}},
			Thresholds: []GradeThreshold{
				{ID: "common", Tier: 1, At: 0}, {ID: "uncommon", Tier: 2, At: 0.055}, {ID: "rare", Tier: 3, At: 0.526},
			},
		},
	}
}

// goldenSamples walks a fixed lattice across the world plus a radial line out
// through the bands, so the fixture exercises border blends, every danger band,
// and the whole reward curve rather than one convenient corner.
func goldenSamples(field *Field) []GoldenSample {
	positions := make([][2]float64, 0, 200)
	for row := 0; row < 11; row++ {
		for column := 0; column < 11; column++ {
			positions = append(positions, [2]float64{
				-32000 + float64(column)*6400,
				-32000 + float64(row)*6400,
			})
		}
	}
	for step := 0; step <= 30; step++ {
		distance := float64(step) * 1500
		positions = append(positions, [2]float64{distance * 0.6, distance * 0.8})
	}
	positions = append(positions, [2]float64{0, 0}, [2]float64{0.5, -0.25}, [2]float64{-899, 0}, [2]float64{44999, 0})
	samples := make([]GoldenSample, 0, len(positions))
	for _, at := range positions {
		region := field.RegionAt(at[0], at[1])
		samples = append(samples, GoldenSample{
			X: at[0], Y: at[1], Region: region.ID, Danger: region.Danger.ID,
			Biome: region.Biome.ID, Neighbour: region.Biome.Neighbour, Blend: region.Biome.Blend,
			Grade: region.Grade.ID, Tier: region.Grade.Tier, Richness: region.Grade.Richness,
		})
	}
	return samples
}

// TestGoldenFieldIsStable is both the regression guard and the fixture writer.
// Set SPELLFIRE_UPDATE_GOLDEN=1 to rewrite the fixture after a deliberate field
// change; the browser's mirror is then checked against the same file.
func TestGoldenFieldIsStable(t *testing.T) {
	field := New(testParams())
	samples := goldenSamples(field)
	if os.Getenv("SPELLFIRE_UPDATE_GOLDEN") == "1" {
		encoded, err := json.MarshalIndent(Golden{Params: field.Params(), Samples: samples}, "", "  ")
		if err != nil {
			t.Fatalf("encode golden: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, append(encoded, '\n'), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Log("golden fixture rewritten")
		return
	}
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	golden := Golden{}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(golden.Samples) != len(samples) {
		t.Fatalf("golden holds %d samples, the field produced %d", len(golden.Samples), len(samples))
	}
	for index, want := range golden.Samples {
		if got := samples[index]; got != want {
			t.Fatalf("sample %d at (%g,%g): got %+v, want %+v", index, want.X, want.Y, got, want)
		}
	}
}

// TestFieldIsDeterministic holds the property the whole chunked world rests on:
// the field is a pure function of position, so sampling it twice — or building
// two fields from the same parameters — can never disagree.
func TestFieldIsDeterministic(t *testing.T) {
	first, second := New(testParams()), New(testParams())
	for step := 0; step < 500; step++ {
		x, y := float64(step)*97-24000, float64(step)*-61+18000
		if first.RegionAt(x, y) != second.RegionAt(x, y) {
			t.Fatalf("two fields from one seed disagree at (%g,%g)", x, y)
		}
		if first.BiomeAt(x, y) != first.BiomeAt(x, y) {
			t.Fatalf("one field disagrees with itself at (%g,%g)", x, y)
		}
	}
}

// TestBiomesAreRadialSwathes is the two-axis rule from world.md made
// executable: biome decides type and radius decides grade, so a biome must not
// be a blob at one radius. Every biome has to appear at the inner Fringe and
// again at the rim, or a Fringe player could not reach some element's material
// without crossing the whole world.
func TestBiomesAreRadialSwathes(t *testing.T) {
	field := New(testParams())
	for _, radius := range []float64{1200, 8000, 20000, 44000} {
		seen := map[string]bool{}
		// A square lattice clipped to the ring: sampling by angle would need
		// trigonometry, which the field deliberately avoids.
		for step := -400; step <= 400; step++ {
			for _, at := range [][2]float64{
				{float64(step) * radius / 400, math.Sqrt(math.Max(0, radius*radius-math.Pow(float64(step)*radius/400, 2)))},
				{float64(step) * radius / 400, -math.Sqrt(math.Max(0, radius*radius-math.Pow(float64(step)*radius/400, 2)))},
			} {
				seen[field.BiomeAt(at[0], at[1]).ID] = true
			}
		}
		if len(seen) != len(testParams().Biomes) {
			t.Fatalf("the ring at radius %g holds %d biomes, want all %d", radius, len(seen), len(testParams().Biomes))
		}
	}
}

// TestRewardCurveIsConvex holds the incentive shape the bands are separated by.
// Richness must never fall as risk rises, and each step outward must be worth
// at least as much as the one before it — a concave curve would make the
// Frontier the efficient place to live and the rim a formality.
func TestRewardCurveIsConvex(t *testing.T) {
	field := New(testParams())
	previous, previousSlope := 0.0, 0.0
	for step := 1; step <= 450; step++ {
		at := float64(step) * 100
		richness := field.RichnessAt(at, 0)
		if richness < previous-1e-12 {
			t.Fatalf("richness fell from %g to %g at radius %g", previous, richness, at)
		}
		slope := richness - previous
		if slope < previousSlope-1e-9 {
			t.Fatalf("the curve flattened from %g to %g at radius %g; it must stay convex", previousSlope, slope, at)
		}
		previous, previousSlope = richness, slope
	}
	if got := field.RichnessAt(45000, 0); math.Abs(got-1) > 1e-12 {
		t.Fatalf("the rim is worth %g, want the full 1", got)
	}
}

// TestGradeFollowsRadiusNotBiome is the other half of the two-axis rule: two
// positions the same distance from the hub yield the same grade whatever biome
// covers them.
func TestGradeFollowsRadiusNotBiome(t *testing.T) {
	field := New(testParams())
	for _, radius := range []float64{4000, 15000, 35000} {
		want := field.GradeAt(radius, 0)
		for step := -300; step <= 300; step++ {
			x := float64(step) * radius / 300
			y := math.Sqrt(math.Max(0, radius*radius-x*x))
			if got := field.GradeAt(x, y); got.ID != want.ID || math.Abs(got.Richness-want.Richness) > 1e-9 {
				t.Fatalf("grade at radius %g varies by position: %+v vs %+v", radius, got, want)
			}
		}
	}
}

// TestCoverageRefusesADegenerateSeed is the load-time contract itself. A field
// with one biome cannot satisfy coverage, and the refusal has to name what is
// missing rather than fail generically — a designer re-rolls a seed on the
// strength of that message.
func TestCoverageRefusesADegenerateSeed(t *testing.T) {
	params := testParams()
	shipped := New(params).Sample(160)
	if problems := shipped.Problems(params.Bands, params.Biomes, 0.06, 400); len(problems) > 0 {
		t.Fatalf("the shipped seed fails its own coverage floor: %v", problems)
	}
	// A lattice one region wide cannot spread five biomes over four bands.
	params.RegionCell = 200000
	params.WarpAmplitude = 0
	degenerate := New(params).Sample(160)
	problems := degenerate.Problems(params.Bands, params.Biomes, 0.06, 400)
	if len(problems) == 0 {
		t.Fatal("a degenerate field passed coverage")
	}
	named := false
	for _, problem := range problems {
		named = named || (contains(problem, "absent from danger band") && contains(problem, "re-roll field.seed"))
	}
	if !named {
		t.Fatalf("coverage refused without naming the missing biome or the fix: %v", problems)
	}
	// The hub has no material grade, so its mix decides nothing and must never
	// be the reason a seed is refused.
	for _, problem := range problems {
		if contains(problem, `"hub"`) {
			t.Fatalf("coverage judged the hub, which yields nothing: %q", problem)
		}
	}
}

func contains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
