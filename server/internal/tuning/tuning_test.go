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
		if _, ok := tables.Shot(weapon); !ok {
			t.Fatalf("starter weapon %q does not resolve to a shot", weapon.ID)
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
		original, _ := before.Shot(weapon)
		edited, ok := after.Shot(weapon)
		if !ok {
			t.Fatalf("%s starter weapon stopped resolving after the edit", class)
		}
		if original.Damage != 10 || edited.Damage != 25 {
			t.Fatalf("%s damage did not follow the band row: %g -> %g", class, original.Damage, edited.Damage)
		}
		// The item row itself was untouched: only the band it points at moved.
		if after.Weapons[weapon.ID].DamageBand != before.Weapons[weapon.ID].DamageBand {
			t.Fatalf("%s weapon row changed; the band edit should have been enough", class)
		}
	}
}

// The mage's staff carries no combat numbers of its own, so retuning the spell
// it casts retunes the staff.
func TestEditingASpellRowMovesTheStaffThatCastsIt(t *testing.T) {
	files := edit(t, shipped(t), "spells.json", func(document map[string]any) {
		bolt := document["fire-bolt"].(map[string]any)
		bolt["mana_cost"] = 30.0
		bolt["cast_interval_ms"] = 500.0
	})
	after, err := Parse(files)
	if err != nil {
		t.Fatalf("edited tables rejected: %v", err)
	}
	staff, _ := after.StarterWeapon("mage")
	shot, ok := after.Shot(staff)
	if !ok {
		t.Fatal("staff stopped resolving")
	}
	if shot.ManaCost != 30 || shot.Interval.Milliseconds() != 500 {
		t.Fatalf("staff did not follow its spell: %#v", shot)
	}
}

// The starter kit must land on the design's raw time-to-kill, and it must do so
// as a consequence of the tables rather than a number written next to them.
func TestStarterItemsHitTheDesignTimeToKill(t *testing.T) {
	tables := MustLoad()
	band := tables.Combat.DamageBands["standard"]
	for _, class := range []string{"gunslinger", "mage"} {
		weapon, _ := tables.StarterWeapon(class)
		shot, _ := tables.Shot(weapon)
		hits := math.Ceil(tables.Combat.Player.MaxHealth / shot.Damage)
		seconds := (hits - 1) * shot.Interval.Seconds()
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
			name: "damaging spell without a dodge vector", file: "spells.json", want: "counterplay vector",
			mutate: func(document map[string]any) {
				document["fire-bolt"].(map[string]any)["dodge_vector"] = ""
			},
		},
		{
			name: "instant spell damage", file: "spells.json", want: "no travel speed",
			mutate: func(document map[string]any) {
				document["fire-bolt"].(map[string]any)["projectile"].(map[string]any)["speed"] = 0.0
			},
		},
		{
			name: "unknown damage band", file: "weapons.json", want: "unknown damage band",
			mutate: func(document map[string]any) {
				document["starter-rifle"].(map[string]any)["damage_band"] = "nonexistent"
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
