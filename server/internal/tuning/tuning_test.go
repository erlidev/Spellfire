package tuning

import (
	"encoding/json"
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

// The keystone versioning invariant: balance lives in shared rows, so editing
// one row moves every item that references it. Characters store references
// only, so nothing needs migrating.
func TestEditingOneBandRowMovesEveryDependentItem(t *testing.T) {
	before := MustLoad()
	files := edit(t, shipped(t), "combat.json", func(document map[string]any) {
		bands := document["damage_bands"].(map[string]any)
		standard := bands["standard"].(map[string]any)
		standard["damage_per_hit"] = 25.0
	})
	after, err := Parse(files)
	if err != nil {
		t.Fatalf("edited tables rejected: %v", err)
	}
	for _, class := range []string{"gunslinger", "mage"} {
		weapon, _ := before.StarterWeapon(class)
		original, _ := before.WeaponAbility(weapon)
		edited, ok := after.WeaponAbility(weapon)
		if !ok {
			t.Fatalf("%s starter weapon stopped resolving after the edit", class)
		}
		if before.BandDamage(original.DamageBand) != 10 || after.BandDamage(edited.DamageBand) != 25 {
			t.Fatalf("%s damage did not follow the band row", class)
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
		cast["interval_ms"] = 500.0
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
	if ability.Cost.Amount != 30 || ability.Interval().Milliseconds() != 500 {
		t.Fatalf("staff did not follow its ability: %#v", ability)
	}
}

// The starter kit must land on the design's raw time-to-kill, and it must do so
// as a consequence of the tables rather than a number written next to them.
func TestStarterItemsHitTheDesignTimeToKill(t *testing.T) {
	tables := MustLoad()
	band := tables.Combat.DamageBands["standard"]
	for _, class := range []string{"gunslinger", "mage"} {
		weapon, _ := tables.StarterWeapon(class)
		ability, _ := tables.WeaponAbility(weapon)
		hits := math.Ceil(tables.Combat.Player.MaxHealth / tables.BandDamage(ability.DamageBand))
		seconds := (hits - 1) * ability.Interval().Seconds()
		if math.Abs(seconds-band.TargetTTKSeconds) > band.TTKToleranceSeconds {
			t.Fatalf("%s raw TTK is %.2fs, outside %.2f±%.2fs", class, seconds, band.TargetTTKSeconds, band.TTKToleranceSeconds)
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
			name: "damaging ability without a dodge vector", file: "abilities.json", want: "counterplay vector",
			mutate: func(document map[string]any) {
				document["fire-bolt-cast"].(map[string]any)["dodge_vector"] = ""
			},
		},
		{
			name: "dodge vector the simulation does not deliver", file: "abilities.json", want: "does not yet deliver",
			mutate: func(document map[string]any) {
				document["fire-bolt-cast"].(map[string]any)["dodge_vector"] = "cast_time"
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
			name: "a windup the simulation does not run", file: "abilities.json", want: "Phase 1.6",
			mutate: func(document map[string]any) {
				document["fire-bolt-cast"].(map[string]any)["windup_ms"] = 400.0
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
