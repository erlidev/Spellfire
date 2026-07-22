package tuning

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"spellfire/data"
)

// shipped copies the embedded tables into a mutable filesystem so a test can
// edit one row and reload, exactly as a balance patch would.
func shipped(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	entries, err := fs.ReadDir(data.Tuning, "tuning")
	if err != nil {
		t.Fatalf("read embedded tables: %v", err)
	}
	for _, entry := range entries {
		name := "tuning/" + entry.Name()
		raw, err := fs.ReadFile(data.Tuning, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	return files
}

// edit rewrites one table through a mutation function, keeping the rest of the
// shipped data intact.
func edit(t *testing.T, files fstest.MapFS, name string, mutate func(document map[string]any)) fstest.MapFS {
	t.Helper()
	document := map[string]any{}
	if err := json.Unmarshal(files["tuning/"+name].Data, &document); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	mutate(document)
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	files["tuning/"+name] = &fstest.MapFile{Data: raw}
	return files
}

func TestShippedTablesLoadAndValidate(t *testing.T) {
	tables, err := Load()
	if err != nil {
		t.Fatalf("embedded tables are invalid: %v", err)
	}
	if tables.Manifest.SchemaVersion != SchemaVersion || tables.Manifest.Version < 1 {
		t.Fatalf("manifest = %#v", tables.Manifest)
	}
	if tables.Admins.Emails == nil {
		t.Fatal("admins table was not loaded")
	}
	spawnables := 0
	for _, definition := range tables.Entities {
		if definition.Admin.Spawnable {
			spawnables++
		}
	}
	if spawnables == 0 || tables.Materials.AdminGrant.Attribute == "" {
		t.Fatalf("entity admin metadata or material grant is missing")
	}
	if tree, wall := tables.Entities["tree"], tables.Entities["wall"]; tree.MaxHealth != 500 || wall.Mass != -1 || wall.MaxHealth != -1 || wall.CollisionObjects[0].Width != wall.CollisionObjects[0].Height {
		t.Fatalf("world entity definitions = tree %#v wall %#v", tree, wall)
	}
	for _, class := range []string{"gunslinger", "mage"} {
		weapon, ok := tables.StarterWeapon(class)
		if !ok {
			t.Fatalf("no starter weapon for %s", class)
		}
		if _, ok := tables.WeaponAbility(weapon); !ok {
			t.Fatalf("starter weapon %q does not resolve to an ability", weapon.ID)
		}
	}
}

func TestSharedTelegraphGrammarCoversEveryStandardShape(t *testing.T) {
	shapes := map[string]Telegraph{
		"circle": {Shape: "circle", Radius: 80, ActiveMS: 100, ResolvedMS: 150},
		"cone":   {Shape: "cone", Length: 180, AngleDegrees: 60, ActiveMS: 100, ResolvedMS: 150},
		"line":   {Shape: "line", Length: 300, Width: 40, ActiveMS: 100, ResolvedMS: 150},
		"ring":   {Shape: "ring", Radius: 120, Width: 20, ActiveMS: 100, ResolvedMS: 150},
	}
	for name, telegraph := range shapes {
		t.Run(name, func(t *testing.T) {
			problems := &report{}
			MustLoad().validateTelegraph(problems, "test", name, telegraph)
			if err := problems.err(); err != nil {
				t.Fatalf("valid %s rejected: %v", name, err)
			}
		})
	}
	sentry := MustLoad().Mobs["sentry"]
	if sentry.DodgeVector != "telegraph" || !contains(TelegraphShapes, sentry.TelegraphShape) {
		t.Fatalf("Sentry does not consume the shared grammar: %#v", sentry)
	}
}

func TestVisionAttributesAreIndependentEntityProperties(t *testing.T) {
	tables := MustLoad()
	for _, id := range []string{"wall", "stone-wall"} {
		definition := tables.Entities[id]
		if !definition.OccludesVision || !definition.VisibleInShadow {
			t.Errorf("%s vision attributes = occludes:%v visible:%v, want both", id, definition.OccludesVision, definition.VisibleInShadow)
		}
	}
	if tree := tables.Entities["tree"]; tree.OccludesVision || !tree.VisibleInShadow {
		t.Fatalf("tree vision attributes = occludes:%v visible:%v, want non-occluding landmark", tree.OccludesVision, tree.VisibleInShadow)
	}
	if tables.Entities["player"].OccludesVision || tables.Entities["player"].VisibleInShadow {
		t.Fatal("players must neither cast static terrain shadows nor remain visible through them")
	}
}

// The keystone versioning invariant: balance lives in shared rows, so editing
// one row moves every item that references it. Characters store references
// only, so nothing needs migrating.
func TestEditingOneBandRowMovesEveryDependentItem(t *testing.T) {
	before := MustLoad()
	files := edit(t, shipped(t), "combat.json", func(document map[string]any) {
		bands := document["damage_bands"].(map[string]any)
		sustained := bands["sustained"].(map[string]any)
		sustained["damage_per_hit"] = 8.05
	})
	after, err := Parse(files)
	if err != nil {
		t.Fatalf("edited tables rejected: %v", err)
	}
	for class, weaponID := range map[string]string{"gunslinger": "field-pistol", "mage": "starter-staff"} {
		weapon := before.Weapons[weaponID]
		original, _ := before.WeaponAbility(weapon)
		edited, ok := after.WeaponAbility(weapon)
		if !ok {
			t.Fatalf("%s starter weapon stopped resolving after the edit", class)
		}
		if before.BandDamage(original.DamageBand) != 8 || after.BandDamage(edited.DamageBand) != 8.05 {
			t.Fatalf("%s damage did not follow the band row: before=%g after=%g band=%s", class, before.BandDamage(original.DamageBand), after.BandDamage(edited.DamageBand), edited.DamageBand)
		}
		// The ability row itself was untouched: only the band it points at moved.
		if after.Abilities[edited.ID].DamageBand != before.Abilities[original.ID].DamageBand {
			t.Fatalf("%s ability row changed; the band edit should have been enough", class)
		}
	}
}

// The mage's staff carries no combat numbers of its own, and neither does the
// spell it casts: both delegate to one ability row, so retuning that row retunes
// the staff.
func TestEditingAnAbilityRowMovesTheStaffThatReachesIt(t *testing.T) {
	files := edit(t, shipped(t), "abilities.json", func(document map[string]any) {
		cast := document["fire-bolt-cast"].(map[string]any)
		cast["cost"].(map[string]any)["amount"] = 30.0
	})
	after, err := Parse(files)
	if err != nil {
		t.Fatalf("edited tables rejected: %v", err)
	}
	staff, _ := after.StarterWeapon("mage")
	ability, ok := after.WeaponAbility(staff)
	if !ok {
		t.Fatal("staff stopped resolving")
	}
	if ability.Cost.Amount != 30 || ability.Interval().Milliseconds() != 240 {
		t.Fatalf("staff did not follow its ability: %#v", ability)
	}
}

// The starter kit must land on the design's raw time-to-kill, and it must do so
// as a consequence of the tables rather than a number written next to them.
func TestStarterItemsHitTheDesignTimeToKill(t *testing.T) {
	tables := MustLoad()
	for _, class := range []string{"gunslinger", "mage"} {
		weapon, _ := tables.StarterWeapon(class)
		ability, _ := tables.WeaponAbility(weapon)
		band := tables.Combat.DamageBands[ability.DamageBand]
		hits := math.Ceil(tables.Entities["player"].MaxHealth / tables.BandDamage(ability.DamageBand))
		seconds := (hits - 1) * ability.Interval().Seconds()
		if math.Abs(seconds-band.TargetTTKSeconds) > band.TTKToleranceSeconds {
			t.Fatalf("%s raw TTK is %.2fs, outside %.2f±%.2fs", class, seconds, band.TargetTTKSeconds, band.TTKToleranceSeconds)
		}
	}
}

func TestEveryDamagingWeaponAndSpellHitsItsCommonBand(t *testing.T) {
	tables := MustLoad()
	target := tables.Entities["player"].MaxHealth
	check := func(kind, id, abilityID string) {
		t.Helper()
		ability := tables.Abilities[abilityID]
		if !ability.Damaging() {
			if ability.Deployable == nil || ability.Deployable.DamageBand == "" {
				return
			}
			band, ok := tables.Combat.DamageBands[ability.Deployable.DamageBand]
			if !ok || ability.Deployable.DamageFraction <= 0 || ability.Deployable.DamageFraction > 1 || ability.Deployable.TickMS <= 0 {
				t.Fatalf("%s %s has an invalid fractional field delivery in band %q", kind, id, ability.Deployable.DamageBand)
			}
			if profile := tables.ResolveDamage(Ability{DamageBand: ability.Deployable.DamageBand}, target); math.Abs(profile.RawTTK.Seconds()-band.TargetTTKSeconds) > band.TTKToleranceSeconds {
				t.Fatalf("%s %s field references off-target band %s", kind, id, ability.Deployable.DamageBand)
			}
			return
		}
		profile := tables.ResolveDamage(ability, target)
		band := tables.Combat.DamageBands[ability.DamageBand]
		if math.Abs(profile.RawTTK.Seconds()-band.TargetTTKSeconds) > band.TTKToleranceSeconds {
			t.Fatalf("%s %s resolves to %.2fs in %s, want %.2f±%.2fs", kind, id, profile.RawTTK.Seconds(), ability.DamageBand, band.TargetTTKSeconds, band.TTKToleranceSeconds)
		}
	}
	for id, weapon := range tables.Weapons {
		check("weapon", id, weapon.Ability)
	}
	for id, spell := range tables.Spells {
		check("spell", id, spell.Ability)
	}
}

func TestRarityUsesTheWeakestComponentAndCoversEveryRole(t *testing.T) {
	tables := MustLoad()
	signature := map[string]string{"receiver": "prototype-receiver", "barrel": "prototype-barrel", "action": "prototype-action", "feed": "prototype-feed", "sight": "prototype-sight"}
	if got := tables.RarityMultiplier(signature); got != 1.3 {
		t.Fatalf("complete Signature gun multiplier = %g, want 1.3", got)
	}
	signature["sight"] = "iron-sights"
	if got := tables.RarityMultiplier(signature); got != 1 {
		t.Fatalf("mixed-tier gun multiplier = %g, want weakest-tier 1", got)
	}

	for _, class := range []string{"gunslinger", "mage"} {
		covered := map[string]bool{}
		for _, weapon := range tables.Weapons {
			if weapon.Class == class {
				for _, role := range weapon.Roles {
					covered[role] = true
				}
			}
		}
		for _, spell := range tables.Spells {
			if class == "mage" {
				for _, role := range spell.Roles {
					covered[role] = true
				}
			}
		}
		for _, gadget := range tables.Gadgets {
			if gadget.Class == class {
				for _, role := range gadget.Roles {
					covered[role] = true
				}
			}
		}
		for _, keystone := range tables.Keystones {
			if keystone.Class == class {
				for _, role := range keystone.Roles {
					covered[role] = true
				}
			}
		}
		for _, role := range tables.Combat.Roles {
			if !covered[role] {
				t.Errorf("%s does not cover %s", class, role)
			}
		}
	}
}

func TestValidationRejectsBrokenTables(t *testing.T) {
	cases := []struct {
		name, file, want string
		mutate           func(document map[string]any)
	}{
		{
			name: "unsupported schema version", file: "manifest.json", want: "forward migration",
			mutate: func(document map[string]any) { document["schema_version"] = 99.0 },
		},
		{
			name: "unknown collision type", file: "entities.json", want: "unsupported type",
			mutate: func(document map[string]any) {
				document["tree"].(map[string]any)["collision_objects"].([]any)[0].(map[string]any)["type"] = "capsule"
			},
		},
		{
			name: "vision occluder without geometry", file: "entities.json", want: "occludes vision but has no collision geometry",
			mutate: func(document map[string]any) {
				document["wall"].(map[string]any)["collision_objects"] = []any{}
			},
		},
		{
			name: "invalid health sentinel", file: "entities.json", want: "max_health must be -1 or positive",
			mutate: func(document map[string]any) { document["wall"].(map[string]any)["max_health"] = -2.0 },
		},
		{
			name: "wall is not square", file: "entities.json", want: "must be square",
			mutate: func(document map[string]any) {
				document["wall"].(map[string]any)["collision_objects"].([]any)[0].(map[string]any)["height"] = 64.0
			},
		},
		{
			name: "invalid admin email", file: "admins.json", want: "not a valid account email",
			mutate: func(document map[string]any) { document["emails"] = []any{"not-an-email"} },
		},
		{
			name: "duplicate normalized admin email", file: "admins.json", want: "duplicate account email",
			mutate: func(document map[string]any) { document["emails"] = []any{"ADMIN@example.com", " admin@example.com "} },
		},
		{
			name: "entity admin field with unsupported input", file: "entities.json", want: "unsupported input",
			mutate: func(document map[string]any) {
				document["player"].(map[string]any)["admin"].(map[string]any)["fields"].([]any)[0].(map[string]any)["input"] = "range-knob"
			},
		},
		{
			name: "damaging ability without a dodge vector", file: "abilities.json", want: "counterplay vector",
			mutate: func(document map[string]any) {
				document["fire-bolt-cast"].(map[string]any)["dodge_vector"] = ""
			},
		},
		{
			name: "cast time without a windup", file: "abilities.json", want: "claims cast_time but declares no windup",
			mutate: func(document map[string]any) {
				document["rifle-shot"].(map[string]any)["dodge_vector"] = "cast_time"
			},
		},
		{
			name: "instant ability damage", file: "abilities.json", want: "no travel speed",
			mutate: func(document map[string]any) {
				document["fire-bolt-cast"].(map[string]any)["projectile"].(map[string]any)["speed"] = 0.0
			},
		},
		{
			name: "unknown damage band", file: "abilities.json", want: "unknown damage band",
			mutate: func(document map[string]any) {
				document["rifle-shot"].(map[string]any)["damage_band"] = "nonexistent"
			},
		},
		{
			name: "windup without a telegraph", file: "abilities.json", want: "windup_ms and a telegraph together",
			mutate: func(document map[string]any) {
				delete(document["fire-bolt-cast"].(map[string]any), "telegraph")
			},
		},
		{
			name: "an ability charging a resource nothing holds", file: "abilities.json", want: "unknown cost kind",
			mutate: func(document map[string]any) {
				document["rifle-shot"].(map[string]any)["cost"].(map[string]any)["kind"] = "stamina"
			},
		},
		{
			name: "a magazine weapon whose ability spends something else", file: "abilities.json", want: "holds a magazine but its ability",
			mutate: func(document map[string]any) {
				document["rifle-shot"].(map[string]any)["cost"] = map[string]any{"kind": "mana", "amount": 5.0}
			},
		},
		{
			name: "an ability applying an effect that does not exist", file: "abilities.json", want: "unknown effect",
			mutate: func(document map[string]any) {
				document["rifle-shot"].(map[string]any)["effects"] = []any{"ghost-burn"}
			},
		},
		{
			name: "an effect kind the simulation cannot run", file: "effects.json", want: "cannot run",
			mutate: func(document map[string]any) {
				document["confusion"] = map[string]any{"name": "Confusion", "kind": "confuse", "duration_ms": 1000.0, "stacking": "refresh"}
			},
		},
		{
			name: "a slow that is really a root", file: "effects.json", want: "a full stop is a root",
			mutate: func(document map[string]any) {
				document["glue"] = map[string]any{"name": "Glue", "kind": "slow", "duration_ms": 1000.0, "stacking": "refresh", "speed_multiplier": 0.0}
			},
		},
		{
			name: "a burn authoring damage outside the band", file: "effects.json", want: "unknown damage band",
			mutate: func(document map[string]any) {
				document["scorch"] = map[string]any{
					"name": "Scorch", "kind": "burn", "duration_ms": 3000.0, "stacking": "refresh",
					"tick_ms": 500.0, "damage_fraction": 0.2, "damage_band": "invented",
				}
			},
		},
		{
			name: "an effect carrying a field its kind does not use", file: "effects.json", want: "declares slow's speed_multiplier",
			mutate: func(document map[string]any) {
				document["snare"] = map[string]any{
					"name": "Snare", "kind": "root", "duration_ms": 1000.0, "stacking": "refresh", "speed_multiplier": 0.5,
				}
			},
		},
		{
			name: "a spell pointing at no ability", file: "spells.json", want: "unknown ability",
			mutate: func(document map[string]any) {
				document["fire-bolt"].(map[string]any)["ability"] = "nonexistent"
			},
		},
		{
			name: "a weapon that both fires and casts", file: "weapons.json", want: "exactly one of ability or spell",
			mutate: func(document map[string]any) {
				document["starter-staff"].(map[string]any)["ability"] = "rifle-shot"
			},
		},
		{
			name: "class left without a starter weapon", file: "weapons.json", want: "no starter weapon for mage",
			mutate: func(document map[string]any) {
				document["starter-staff"].(map[string]any)["starter"] = false
			},
		},
		{
			name: "biome pointing at no element", file: "biomes.json", want: "unknown element",
			mutate: func(document map[string]any) {
				document["emberlands"].(map[string]any)["element"] = "plasma"
			},
		},
		{
			name: "danger bands that leave a gap to the rim", file: "world.json", want: "outermost danger band",
			mutate: func(document map[string]any) {
				bands := document["danger_bands"].([]any)
				bands[len(bands)-1].(map[string]any)["outer_radius"] = 2500.0
			},
		},
		{
			name: "safety reappearing outside a hostile band", file: "world.json", want: "contiguous from the hub",
			mutate: func(document map[string]any) {
				document["danger_bands"].([]any)[3].(map[string]any)["pvp"] = "off"
			},
		},
		{
			name: "a logout window that lets a body vanish", file: "session.json", want: "combat logging free",
			mutate: func(document map[string]any) { document["logout_linger_seconds"] = 0.0 },
		},
		{
			name: "a position expiring before the body it belongs to", file: "session.json", want: "must exceed logout_linger_seconds",
			mutate: func(document map[string]any) { document["position_expiry_seconds"] = 5.0 },
		},
		{
			name: "an outpost outside the world", file: "outposts.json", want: "outside the",
			mutate: func(document map[string]any) {
				document["adrift"] = map[string]any{"name": "Adrift", "position": []any{99000.0, 0.0}}
			},
		},
		{
			name: "snapshot rate that does not divide the tick rate", file: "simulation.json", want: "whole multiple",
			mutate: func(document map[string]any) { document["send_rate"] = 7.0 },
		},
		{
			name: "component filling a slot its blueprint lacks", file: "components.json", want: "does not expose",
			mutate: func(document map[string]any) {
				document["components"].(map[string]any)["bad-scope"] = map[string]any{
					"name": "Bad scope", "blueprint": "staff", "slot": "scope", "effect": "none",
				}
			},
		},
		{
			name: "a gun with no recoil pattern", file: "weapons.json", want: "every gun kicks",
			mutate: func(document map[string]any) {
				document["starter-rifle"].(map[string]any)["recoil"] = map[string]any{"pattern": []any{}, "recovery_ms": 400.0}
			},
		},
		{
			name: "a recoil pattern that never recovers", file: "weapons.json", want: "never recovers",
			mutate: func(document map[string]any) {
				document["starter-rifle"].(map[string]any)["recoil"].(map[string]any)["recovery_ms"] = 0.0
			},
		},
		{
			name: "firing on the move costing no accuracy", file: "weapons.json", want: "has to cost accuracy",
			mutate: func(document map[string]any) {
				document["starter-rifle"].(map[string]any)["spread"].(map[string]any)["moving_degrees"] = 0.1
			},
		},
		{
			name: "a weight class that speeds its carrier up", file: "combat.json", want: "never speeds one up",
			mutate: func(document map[string]any) {
				document["weight_classes"].(map[string]any)["heavy"].(map[string]any)["movement_multiplier"] = 1.4
			},
		},
		{
			name: "hitscan without a scope to commit to", file: "abilities.json", want: "no counterplay at all",
			mutate: func(document map[string]any) {
				document["sniper-shot"].(map[string]any)["requires_scope"] = false
			},
		},
		{
			name: "a shield that also deals damage", file: "abilities.json", want: "locks fire while it is up",
			mutate: func(document map[string]any) {
				document["riot-shield-raise"].(map[string]any)["damage_band"] = "standard"
			},
		},
		{
			name: "a withheld category that costs nothing", file: "weapons.json", want: "nothing gates it",
			mutate: func(document map[string]any) { delete(document["long-sniper"].(map[string]any), "cost") },
		},
		{
			name: "a basic-set weapon that has to be built first", file: "weapons.json", want: "unusable weapon",
			mutate: func(document map[string]any) { document["starter-rifle"].(map[string]any)["requires_craft"] = true },
		},
		{
			name: "special ammunition nothing can build", file: "ammunition.json", want: "no ammunition recipe produces it",
			mutate: func(document map[string]any) { delete(document, "rocket") },
		},
		{
			name: "a round that pays for itself", file: "ammunition.json", want: "costs the very material it produces",
			mutate: func(document map[string]any) {
				document["rocket"].(map[string]any)["cost"] = map[string]any{"rocket": 1.0}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Parse(edit(t, shipped(t), testCase.file, testCase.mutate))
			if err == nil {
				t.Fatal("invalid tables were accepted")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %q does not mention %q", err, testCase.want)
			}
		})
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	files := edit(t, shipped(t), "combat.json", func(document map[string]any) {
		document["mystery"] = 1.0
	})
	if _, err := Parse(files); err == nil || !strings.Contains(err.Error(), "mystery") {
		t.Fatalf("unknown field accepted: %v", err)
	}
}

func TestBandAtResolvesEveryRadius(t *testing.T) {
	world := MustLoad().World
	cases := map[float64]string{0: "hub", 430: "hub", 431: "fringe", 1000: "fringe", 2100: "frontier", 2999: "deadlands", 4000: "deadlands"}
	for distance, want := range cases {
		if got := world.BandAt(distance); got.ID != want {
			t.Fatalf("BandAt(%g) = %q, want %q", distance, got.ID, want)
		}
	}
	if world.SafeRadius() != 430 || world.PvPRadius() != 1000 {
		t.Fatalf("derived radii = %g / %g", world.SafeRadius(), world.PvPRadius())
	}
}

// withMaterial adds one live material row so a retirement test has something to
// resolve to. Materials arrive with Phase 4.1; the retirement contract does not
// wait for them.
func withMaterial(t *testing.T, files fstest.MapFS, id string) fstest.MapFS {
	t.Helper()
	return edit(t, files, "materials.json", func(document map[string]any) {
		document["materials"].(map[string]any)[id] = map[string]any{
			"name": "Scrap " + id, "grade": "common", "kind": "structural",
		}
	})
}

func retire(t *testing.T, files fstest.MapFS, rows map[string]any) fstest.MapFS {
	t.Helper()
	return edit(t, files, "retired.json", func(document map[string]any) {
		for id, row := range rows {
			document[id] = row
		}
	})
}

// A retired ID must stay resolvable forever: content is withdrawn by pointing
// it at a replacement or a refund, never by deleting it out from under a save.
func TestRetiredIDsResolveToAReplacementOrARefund(t *testing.T) {
	files := withMaterial(t, shipped(t), "iron")
	files = retire(t, files, map[string]any{
		"old-iron": map[string]any{"kind": "material", "replacement": "older-iron", "note": "renamed twice"},
		// A chain must be followed to the end, not one hop.
		"older-iron": map[string]any{"kind": "material", "replacement": "iron", "note": "renamed"},
		"lost-alloy": map[string]any{"kind": "material", "refund": map[string]any{"iron": 2.0}, "note": "recipe withdrawn"},
		"old-rifle":  map[string]any{"kind": "weapon", "replacement": "starter-rifle", "note": "superseded"},
	})
	tables, err := Parse(files)
	if err != nil {
		t.Fatalf("valid retirements rejected: %v", err)
	}

	if resolved, ok := tables.Resolve("material", "iron"); !ok || resolved.ID != "iron" {
		t.Fatalf("live material = %#v, %v", resolved, ok)
	}
	if resolved, ok := tables.Resolve("material", "old-iron"); !ok || resolved.ID != "iron" {
		t.Fatalf("retirement chain = %#v, %v", resolved, ok)
	}
	if resolved, ok := tables.Resolve("weapon", "old-rifle"); !ok || resolved.ID != "starter-rifle" {
		t.Fatalf("retired weapon = %#v, %v", resolved, ok)
	}
	resolved, ok := tables.Resolve("material", "lost-alloy")
	if !ok || resolved.ID != "" || resolved.Refund["iron"] != 2 {
		t.Fatalf("refund = %#v, %v", resolved, ok)
	}
	// Retirement never crosses tables, and an ID from no build at all is the
	// only reference a caller may drop.
	if _, ok := tables.Resolve("weapon", "old-iron"); ok {
		t.Fatal("a retired material resolved as a weapon")
	}
	if _, ok := tables.Resolve("material", "never-shipped"); ok {
		t.Fatal("an unknown id resolved")
	}
}

func TestValidationRejectsBrokenRetirements(t *testing.T) {
	cases := []struct {
		name, want string
		rows       map[string]any
	}{
		{
			name: "chain that reaches nothing", want: "reaches neither a live",
			rows: map[string]any{"gone": map[string]any{"kind": "material", "replacement": "vapour", "note": "n"}},
		},
		{
			name: "cycle", want: "reaches neither a live",
			rows: map[string]any{
				"a": map[string]any{"kind": "material", "replacement": "b", "note": "n"},
				"b": map[string]any{"kind": "material", "replacement": "a", "note": "n"},
			},
		},
		{
			name: "retiring a live id", want: "either current or retired",
			rows: map[string]any{"starter-rifle": map[string]any{"kind": "weapon", "replacement": "starter-rifle", "note": "n"}},
		},
		{
			name: "neither replacement nor refund", want: "exactly one of replacement or refund",
			rows: map[string]any{"gone": map[string]any{"kind": "material", "note": "n"}},
		},
		{
			name: "both replacement and refund", want: "exactly one of replacement or refund",
			rows: map[string]any{"gone": map[string]any{"kind": "material", "replacement": "iron", "refund": map[string]any{"iron": 1.0}, "note": "n"}},
		},
		{
			name: "refund of unknown material", want: "refunds unknown material",
			rows: map[string]any{"gone": map[string]any{"kind": "material", "refund": map[string]any{"vapour": 1.0}, "note": "n"}},
		},
		{
			name: "unknown kind", want: "unknown kind",
			rows: map[string]any{"gone": map[string]any{"kind": "outpost", "replacement": "iron", "note": "n"}},
		},
		{
			name: "no reason recorded", want: "must record why",
			rows: map[string]any{"gone": map[string]any{"kind": "material", "replacement": "iron"}},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Parse(retire(t, withMaterial(t, shipped(t), "iron"), testCase.rows))
			if err == nil {
				t.Fatal("invalid retirements were accepted")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %q does not mention %q", err, testCase.want)
			}
		})
	}
}

// The action bar is one width for both classes: a Gunslinger's weapon plus its
// gadgets and a Mage's spells must lay out to the same number of bindings.
func TestLoadoutRejectsMismatchedBarWidths(t *testing.T) {
	files := edit(t, shipped(t), "loadout.json", func(document map[string]any) {
		document["gadget_slots"] = 2.0
	})
	if _, err := Parse(files); err == nil || !strings.Contains(err.Error(), "action bars of different widths") {
		t.Fatalf("mismatched bar widths accepted: %v", err)
	}
}

// Affinity's shape is locked, but the multiplier is tunable — and a multiplier
// that makes the grid's own tier-4 build unequippable is a table error.
func TestLoadoutRejectsAnUnsatisfiableAffinityRule(t *testing.T) {
	files := edit(t, shipped(t), "loadout.json", func(document map[string]any) {
		document["affinity"] = map[string]any{"same_element_per_tier": 3.0}
	})
	if _, err := Parse(files); err == nil || !strings.Contains(err.Error(), "same-element spells beside it") {
		t.Fatalf("unsatisfiable affinity accepted: %v", err)
	}
}

// The unlock ledger stores bare IDs, so an ID shared across content tables
// would make an entry ambiguous, and content no level grants would be
// unreachable for anyone whose starter draw missed it.
func TestUnlockIDsMustBeUniqueAndReachable(t *testing.T) {
	files := shipped(t)
	files["tuning/gadgets.json"] = &fstest.MapFile{Data: []byte(`{"fire-bolt": {"name": "Bolt gadget", "class": "gunslinger", "unlock_level": 2, "ability": "rifle-shot"}}`)}
	if _, err := Parse(files); err == nil || !strings.Contains(err.Error(), "unlock IDs are flat") {
		t.Fatalf("an ID claimed by two content tables was accepted: %v", err)
	}
	unreachable := edit(t, shipped(t), "spells.json", func(document map[string]any) {
		document["fire-bolt"].(map[string]any)["unlock_level"] = 0.0
	})
	if _, err := Parse(unreachable); err == nil || !strings.Contains(err.Error(), "can never be earned") {
		t.Fatalf("content no level grants was accepted: %v", err)
	}
	past := edit(t, shipped(t), "spells.json", func(document map[string]any) {
		document["fire-bolt"].(map[string]any)["unlock_level"] = 9999.0
	})
	if _, err := Parse(past); err == nil || !strings.Contains(err.Error(), "past the level cap") {
		t.Fatalf("content unlocking past the cap was accepted: %v", err)
	}
}

// The starter kit exists so a zero-material character is combat-capable
// immediately. A draw too small to fill the bar would leave it with holes.
func TestProgressionRejectsACurveOrKitThatCannotWork(t *testing.T) {
	small := edit(t, shipped(t), "progression.json", func(document map[string]any) {
		document["starter_kit"] = map[string]any{"unlocks": 1.0}
	})
	if _, err := Parse(small); err == nil || !strings.Contains(err.Error(), "cannot fill the") {
		t.Fatalf("a starter kit too small for the bar was accepted: %v", err)
	}
	unpriced := edit(t, shipped(t), "progression.json", func(document map[string]any) {
		delete(document["sources"].(map[string]any), "harvest")
	})
	if _, err := Parse(unpriced); err == nil || !strings.Contains(err.Error(), `source "harvest"`) {
		t.Fatalf("an unpriced XP source was accepted: %v", err)
	}
	unknown := edit(t, shipped(t), "progression.json", func(document map[string]any) {
		document["sources"].(map[string]any)["duelling"] = 5.0
	})
	if _, err := Parse(unknown); err == nil || !strings.Contains(err.Error(), "not one the simulation awards") {
		t.Fatalf("an XP source nothing awards was accepted: %v", err)
	}
	shrinking := edit(t, shipped(t), "progression.json", func(document map[string]any) {
		document["growth"] = 0.5
	})
	if _, err := Parse(shrinking); err == nil || !strings.Contains(err.Error(), "at least 1") {
		t.Fatalf("a curve where a later level costs less was accepted: %v", err)
	}
}

// A gadget is identity over one ability, exactly like a spell, and it is the
// Gunslinger's slot kind alone.
func TestGadgetRowsAreValidatedLikeSpells(t *testing.T) {
	files := shipped(t)
	files["tuning/gadgets.json"] = &fstest.MapFile{Data: []byte(`{"smoke": {"name": "Smoke", "class": "mage", "ability": "rifle-shot"}}`)}
	if _, err := Parse(files); err == nil || !strings.Contains(err.Error(), "gadgets are the Gunslinger's slot kind") {
		t.Fatalf("a Mage gadget was accepted: %v", err)
	}
	files["tuning/gadgets.json"] = &fstest.MapFile{Data: []byte(`{"smoke": {"name": "Smoke", "class": "gunslinger", "ability": "no-such-ability"}}`)}
	if _, err := Parse(files); err == nil || !strings.Contains(err.Error(), "unknown ability") {
		t.Fatalf("a gadget with no ability was accepted: %v", err)
	}
}

// Crafting names only consumed attributes. Even with the mana-crystal output
// exception, fire cadence is an unrestricted DPS multiplier and an unread name
// is a promise the world would silently drop.
func TestComponentModifiersStayOnTheBehaviourAxis(t *testing.T) {
	component := func(modifiers string) string {
		return `{"blueprints": {"gun": {"name": "Gun", "slots": ["muzzle"]}, "staff": {"name": "Staff", "slots": ["core"]}},
		 "components": {"brake": {"name": "Brake", "blueprint": "gun", "slot": "muzzle",
		   "effect": "Rounds leave faster.", "cost": {"salvaged-plate": 2}, "modifiers": ` + modifiers + `}}}`
	}
	cases := []struct{ name, modifiers, want string }{
		{"cadence", `{"interval_ms": 0.8}`, "crafting may never touch"},
		{"unread", `{"recoil": 0.8}`, "which the simulation does not read"},
		{"out of band", `{"projectile_speed": 6}`, "outside the [0.5,2] band"},
		{"no change", `{"projectile_speed": 1}`, "which is no change at all"},
		{"empty", `{}`, "declares no modifiers"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			files := shipped(t)
			files["tuning/components.json"] = &fstest.MapFile{Data: []byte(component(testCase.modifiers))}
			if _, err := Parse(files); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("modifiers %s were accepted: %v", testCase.modifiers, err)
			}
		})
	}
}

// Materials have to be hauled to a safe zone and spent, so a component with no
// cost would put a behaviour change outside the economy entirely — and a cost in
// a material that does not exist could never be paid.
func TestComponentsMustCostLiveMaterialsAndExplainThemselves(t *testing.T) {
	base := `{"blueprints": {"gun": {"name": "Gun", "slots": ["muzzle"]}, "staff": {"name": "Staff", "slots": ["core"]}},
	 "components": {"brake": {"name": "Brake", "blueprint": "gun", "slot": "muzzle", %s
	   "modifiers": {"projectile_speed": 1.2}}}}`
	cases := []struct{ name, row, want string }{
		{"no cost", `"effect": "Faster rounds.",`, "declares no material cost"},
		{"unknown material", `"effect": "Faster rounds.", "cost": {"unobtainium": 1},`, "costs unknown material"},
		{"non-positive", `"effect": "Faster rounds.", "cost": {"salvaged-plate": 0},`, "non-positive count"},
		{"no plain language", `"cost": {"salvaged-plate": 2},`, "plain language"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			files := shipped(t)
			files["tuning/components.json"] = &fstest.MapFile{Data: []byte(fmt.Sprintf(base, testCase.row))}
			if _, err := Parse(files); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("component row %s was accepted: %v", testCase.row, err)
			}
		})
	}
}

// A magazine attribute means nothing on a blueprint whose weapons cast spells,
// so a staff component may not claim one.
func TestStaffComponentsMayNotModifyAMagazine(t *testing.T) {
	files := shipped(t)
	files["tuning/components.json"] = &fstest.MapFile{Data: []byte(
		`{"blueprints": {"gun": {"name": "Gun", "slots": ["muzzle"]}, "staff": {"name": "Staff", "slots": ["core"]}},
		 "components": {"core": {"name": "Core", "blueprint": "staff", "slot": "core",
		   "effect": "Holds more.", "cost": {"salvaged-plate": 2}, "modifiers": {"magazine_size": 1.5}}}}`)}
	if _, err := Parse(files); err == nil || !strings.Contains(err.Error(), "no \"staff\" weapon holds a magazine") {
		t.Fatalf("a staff component claimed a magazine: %v", err)
	}
}

// Affinity requires N−1 same-element spells beside a tier-N spell, so an
// element authored short of tier 4 could not support the 4 + 2 build its own
// rule describes. This is a claim about the shipped content rather than a
// structural rule, which is why it is a test and not a loader check: a fixture
// or a deployment may legitimately ship a narrower table.
func TestShippedSpellGridIsComplete(t *testing.T) {
	tables := MustLoad()
	for element := range tables.Elements {
		for tier := 1; tier <= 4; tier++ {
			found := ""
			for id, spell := range tables.Spells {
				if spell.Element == element && spell.Tier == tier {
					found = id
				}
			}
			if found == "" {
				t.Fatalf("%s has no tier %d spell, so its own affinity rule cannot be satisfied", element, tier)
			}
		}
	}
	// And the signature build the rule describes has to fit the bar it shares:
	// one tier-4 spell plus the company it needs, inside the action bar.
	needed := 1 + tables.Loadout.RequiredSameElement(4)
	if slots := tables.Loadout.BarSlots(); needed > slots {
		t.Fatalf("a tier 4 signature needs %d slots of the %d-slot bar", needed, slots)
	}
}

// Every spell owes cost and counterplay on the ability it names: mana to cast,
// and — for the defining tiers — a cooldown that is the second resource axis.
func TestShippedSpellsPriceThemselves(t *testing.T) {
	tables := MustLoad()
	for id, spell := range tables.Spells {
		ability := tables.Abilities[spell.Ability]
		if ability.Cost.Kind != CostMana || ability.Cost.Amount <= 0 {
			t.Fatalf("%s costs %q %g, want mana", id, ability.Cost.Kind, ability.Cost.Amount)
		}
		if spell.Tier > 1 && ability.CooldownMS <= 0 {
			t.Fatalf("%s is tier %d but holds no cooldown; mana alone cannot gate a defining spell", id, spell.Tier)
		}
	}
	// Tier is a commitment axis: a higher tier costs more and locks out longer.
	for _, element := range sortedKeys(tables.Elements) {
		byTier := map[int]Ability{}
		for _, spell := range tables.Spells {
			if spell.Element == element {
				byTier[spell.Tier] = tables.Abilities[spell.Ability]
			}
		}
		for tier := 2; tier <= 4; tier++ {
			if byTier[tier].Cost.Amount <= byTier[tier-1].Cost.Amount {
				t.Fatalf("%s tier %d costs %g, no more than tier %d's %g", element, tier, byTier[tier].Cost.Amount, tier-1, byTier[tier-1].Cost.Amount)
			}
			if byTier[tier].CooldownMS <= byTier[tier-1].CooldownMS {
				t.Fatalf("%s tier %d locks out %dms, no longer than tier %d's %dms", element, tier, byTier[tier].CooldownMS, tier-1, byTier[tier-1].CooldownMS)
			}
		}
	}
}
