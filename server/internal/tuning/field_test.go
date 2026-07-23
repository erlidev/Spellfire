package tuning

import (
	"math"
	"testing"
)

// TestShippedSeedCoversEveryElementInEveryBand is the shipped half of the
// coverage contract. Validation refuses a bad seed at load; this states what
// the seed actually delivers, so a re-roll that squeaks past the floor is
// visible as a regression rather than as a pass.
func TestShippedSeedCoversEveryElementInEveryBand(t *testing.T) {
	tables := MustLoad()
	rule := tables.World.Field.Coverage
	coverage := tables.Field().Sample(rule.Resolution)
	worst, where := 1.0, ""
	for _, band := range tables.World.DangerBands {
		if band.MaterialGrade == "" {
			continue
		}
		for _, biome := range sortedKeys(tables.Biomes) {
			share := coverage.Share[band.ID][biome]
			if share < worst {
				worst, where = share, biome+" in "+band.ID
			}
		}
	}
	if worst < rule.MinimumShare {
		t.Fatalf("%s covers %.1f%%, below the %.1f%% floor", where, worst*100, rule.MinimumShare*100)
	}
	// A seed that only just clears the floor is one balance edit from failing,
	// and the point of a re-rollable seed is that a comfortable one is free.
	if worst < 2*rule.MinimumShare {
		t.Fatalf("the shipped seed only just clears its own floor: %s covers %.1f%% against a %.1f%% floor; re-roll for margin",
			where, worst*100, rule.MinimumShare*100)
	}
	if balance := coverage.Balance(tables.FieldBands(), sortedKeys(tables.Biomes)); balance > 0.06 {
		t.Fatalf("the world-wide biome mix is %.1f%% off an even split; one element dominates the map", balance*100)
	}
}

// TestGeographyNeverHardLocksARecipe is the claim the coverage check exists to
// support, stated at the level a player feels it: every component in the tables
// is buildable by someone, and the materials it needs are actually produced by
// ground that exists.
func TestGeographyNeverHardLocksARecipe(t *testing.T) {
	tables := MustLoad()
	// What the whole world can yield, taken from the field rather than assumed.
	reachable := map[string]bool{}
	for step := 0; step < 4000; step++ {
		at := Vec2{X: -44000 + float64(step%64)*1400, Y: -44000 + float64(step/64)*1400}
		if math.Hypot(at.X, at.Y) > tables.World.Radius {
			continue
		}
		for _, id := range tables.MaterialsAtPosition(at.X, at.Y) {
			reachable[id] = true
		}
	}
	for _, id := range sortedKeys(tables.Components.Components) {
		for material := range tables.Components.Components[id].Cost {
			kind := tables.Materials.Kinds[tables.Materials.Materials[material].Kind]
			// Crafted and boss-dropped stock has no ground behind it by design.
			if kind.Source == "craft" || kind.ID == "boss_reward" || material == "signature-essence" {
				continue
			}
			if !reachable[material] {
				t.Fatalf("component %q costs %q, which no ground in the world yields", id, material)
			}
		}
	}
}

// TestEveryBiomeIsWorthTravellingTo keeps the biome axis load-bearing rather
// than decorative: each region has to offer stock no other region does, or a
// player would never have a reason to cross the world for one.
func TestEveryBiomeIsWorthTravellingTo(t *testing.T) {
	tables := MustLoad()
	for _, biome := range sortedKeys(tables.Biomes) {
		exclusive := 0
		for _, id := range tables.MaterialsAt(biome, len(tables.Materials.Grades)) {
			if tables.Materials.Materials[id].Biome == biome {
				exclusive++
			}
		}
		if exclusive < 2 {
			t.Fatalf("biome %q offers %d materials nobody else does; there is no reason to go there", biome, exclusive)
		}
	}
	// And the universal half: the hub-grade structural stock every blueprint
	// needs must be available in all of them.
	for _, biome := range sortedKeys(tables.Biomes) {
		found := false
		for _, id := range tables.MaterialsAt(biome, 1) {
			found = found || id == "salvaged-plate"
		}
		if !found {
			t.Fatalf("biome %q does not yield the universal structural stock", biome)
		}
	}
}

// TestGradeIsACeilingNotAnEquality holds the rule harvesting will inherit:
// richer ground yields everything poorer ground did plus its own grade, so
// walking outward adds options rather than trading them.
func TestGradeIsACeilingNotAnEquality(t *testing.T) {
	tables := MustLoad()
	for _, biome := range sortedKeys(tables.Biomes) {
		previous := tables.MaterialsAt(biome, 1)
		for tier := 2; tier <= 3; tier++ {
			current := tables.MaterialsAt(biome, tier)
			if len(current) <= len(previous) {
				t.Fatalf("biome %q yields %d materials at tier %d and %d at tier %d; depth has to be worth something",
					biome, len(previous), tier-1, len(current), tier)
			}
			for _, id := range previous {
				found := false
				for _, candidate := range current {
					found = found || candidate == id
				}
				if !found {
					t.Fatalf("biome %q stops yielding %q at tier %d", biome, id, tier)
				}
			}
			previous = current
		}
	}
}

// Vec2 is a local point type. The tuning package has no geometry of its own and
// deliberately does not import the simulation's.
type Vec2 struct{ X, Y float64 }
