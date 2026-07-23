package game

import (
	"testing"
	"time"

	"spellfire/server/internal/tuning"
)

// fullScaleWorld is the shipped geography rather than the compact test arena:
// these tests are about the world field, so shrinking the bands would be
// shrinking the subject.
func fullScaleWorld() (*World, time.Time) {
	balance := DefaultTuning()
	balance.AOIRadius = 500
	world := NewWorld(balance)
	world.setWorldItems()
	return world, time.Unix(1_700_000_000, 0)
}

// TestZoneRulesResolveThroughTheField holds the substitution Phase 3.1 made:
// safety and PvP protection are answers the field gives, not radius
// comparisons scattered through the simulation. Every band in the table is
// checked, so a band whose PvP state moved takes the rules with it.
func TestZoneRulesResolveThroughTheField(t *testing.T) {
	world, _ := fullScaleWorld()
	previous := 0.0
	for _, band := range world.tuning.Tables.World.DangerBands {
		at := Vec{X: (previous + band.OuterRadius) / 2}
		got := world.DangerAt(at)
		if got.ID != band.ID {
			t.Fatalf("the middle of %q resolved to %q", band.ID, got.ID)
		}
		wantSafe := band.PvP == "off"
		wantProtected := wantSafe || band.PvP == "restricted"
		if world.Safe(at) != wantSafe || world.Protected(at) != wantProtected {
			t.Fatalf("band %q: safe=%v protected=%v, want %v/%v", band.ID, world.Safe(at), world.Protected(at), wantSafe, wantProtected)
		}
		previous = band.OuterRadius
	}
}

// TestPvPProtectionCoversBothEnds is the invariant the field replaced a pair of
// squared-radius comparisons with: a shooter inside protection cannot reach out
// of it, and a shooter outside it cannot reach in.
func TestPvPProtectionCoversBothEnds(t *testing.T) {
	world, _ := fullScaleWorld()
	inside, outside := Vec{X: 400}, Vec{X: 20000}
	shooter := &Player{}
	shooter.Position = inside
	if world.hostileReach(shooter, outside) {
		t.Fatal("a shooter standing in safety reached a target outside it")
	}
	shooter.Position = outside
	if world.hostileReach(shooter, inside) {
		t.Fatal("a shooter outside safety reached a target standing in it")
	}
	if !world.hostileReach(shooter, Vec{X: 21000}) {
		t.Fatal("two bodies in hostile territory could not reach each other")
	}
	// A field with no caster behind it obeys the same protection.
	if world.hazardReach(nil, "", inside) {
		t.Fatal("an ownerless hazard reached into safety")
	}
	if !world.hazardReach(nil, "", outside) {
		t.Fatal("an ownerless hazard could not reach hostile ground")
	}
}

// TestGroundYieldsUniversalStockEverywhereAndAlignedStockOnlyAtHome is the
// economy half of the field. It is what "geography never hard-locks a build"
// means concretely: wherever a player stands outside the hub, the structural
// stock every recipe needs is available, and the element-typed stock is not.
func TestGroundYieldsUniversalStockEverywhereAndAlignedStockOnlyAtHome(t *testing.T) {
	world, _ := fullScaleWorld()
	tables := world.tuning.Tables
	sampled := map[string]bool{}
	// A square lattice across the world rather than a radial line: regions are
	// radial swathes, so one ray stays inside one biome by design and would
	// prove nothing about the others.
	for step := 0; step < 400; step++ {
		at := Vec{X: -38000 + float64(step%20)*4000, Y: -38000 + float64(step/20)*4000}
		if at.LengthSq() > world.tuning.WorldRadius*world.tuning.WorldRadius {
			continue
		}
		region := world.RegionAt(at)
		if region.Grade.Tier == 0 {
			continue
		}
		sampled[region.Biome.ID] = true
		yielded := world.MaterialsAt(at)
		universal := false
		for _, id := range yielded {
			material := tables.Materials.Materials[id]
			kind := tables.Materials.Kinds[material.Kind]
			if kind.Source == "craft" {
				t.Fatalf("crafted material %q was offered as ground yield at %v", id, at)
			}
			if tables.Materials.Grades[material.Grade].Tier > region.Grade.Tier {
				t.Fatalf("%q is above the %s grade this ground yields", id, region.Grade.ID)
			}
			if !kind.Universal && material.Biome != region.Biome.ID {
				t.Fatalf("%q is gated to %q but was offered in %q", id, material.Biome, region.Biome.ID)
			}
			universal = universal || kind.Universal
		}
		if !universal {
			t.Fatalf("no universal stock at %v in %q; geography would hard-lock a build", at, region.Biome.ID)
		}
	}
	if len(sampled) != len(tables.Biomes) {
		t.Fatalf("the lattice crossed %d biomes, want all %d; every element has to have geography", len(sampled), len(tables.Biomes))
	}
}

// TestMaterialGradeRisesWithRadius is the vertical axis: depth is what buys a
// better grade, so a walk outward must never take an option away.
func TestMaterialGradeRisesWithRadius(t *testing.T) {
	world, _ := fullScaleWorld()
	previousTier, previous := 0, []string(nil)
	for step := 1; step <= 440; step++ {
		at := Vec{X: float64(step) * 100}
		region := world.RegionAt(at)
		if region.Grade.Tier < previousTier {
			t.Fatalf("grade fell from tier %d to %d at %g", previousTier, region.Grade.Tier, at.X)
		}
		yielded := world.MaterialsAt(at)
		if region.Grade.Tier > previousTier {
			previousTier, previous = region.Grade.Tier, yielded
			continue
		}
		if len(yielded) < len(previous) {
			t.Fatalf("walking outward to %g removed an option: %d yields, was %d", at.X, len(yielded), len(previous))
		}
		previous = yielded
	}
	if previousTier < 3 {
		t.Fatalf("the rim tops out at grade tier %d; the deep world has to be worth reaching", previousTier)
	}
}

// TestShippedTablesBuildTheGoldenField ties the cross-language fixture to the
// shipped rows. The fixture states its parameters literally so the worldfield
// package can stay free of a tuning import; without this the two could drift
// and both suites would still pass.
func TestShippedTablesBuildTheGoldenField(t *testing.T) {
	params := tuning.MustLoad().FieldParams()
	if params.Radius != 45000 || params.Seed != 2676017207 {
		t.Fatalf("the shipped field is radius %g seed %d; update testdata/worldfield.json", params.Radius, params.Seed)
	}
	if params.RegionCell != 9000 || params.RadialReference != 22500 || params.RegionJitter != 0.72 {
		t.Fatalf("the shipped lattice moved: %+v", params)
	}
	if params.WarpCell != 14000 || params.WarpAmplitude != 2600 || params.BlendWidth != 0.16 {
		t.Fatalf("the shipped warp moved: %+v", params)
	}
}
