package game

import (
	"encoding/json"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"spellfire/data"
	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

// shipped copies the embedded tables into a mutable filesystem so a test can
// edit rows and reload, exactly as a content patch would.
func shipped(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	entries, err := fs.ReadDir(data.Tuning, "tuning")
	if err != nil {
		t.Fatalf("read embedded tables: %v", err)
	}
	for _, entry := range entries {
		raw, err := fs.ReadFile(data.Tuning, "tuning/"+entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		files["tuning/"+entry.Name()] = &fstest.MapFile{Data: raw}
	}
	return files
}

// edit rewrites one table through a mutation function, keeping the rest intact.
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

// rebalanced reloads the shipped tables with one damage-band row edited, the
// way a balance patch would ship.
func rebalanced(t *testing.T, damagePerHit float64) *tuning.Tables {
	t.Helper()
	files := edit(t, shipped(t), "combat.json", func(document map[string]any) {
		document["damage_bands"].(map[string]any)["standard"].(map[string]any)["damage_per_hit"] = damagePerHit
	})
	tables, err := tuning.Parse(files)
	if err != nil {
		t.Fatalf("rebalanced tables rejected: %v", err)
	}
	return tables
}

// damageDealt fires one starter shot from a stored character record and reports
// the health the target lost.
func damageDealt(t *testing.T, tables *tuning.Tables, shooter, target model.Character) float64 {
	t.Helper()
	balance := FromTables(tables)
	balance.AOIRadius = 500
	world := NewWorld(balance)
	world.colliders = nil
	now := time.Unix(1_700_000_000, 0)
	attacker := world.AddPlayer(shooter, now)
	victim := world.AddPlayer(target, now)
	attacker.Position, victim.Position = Vec{1200, 0}, Vec{1300, 0}
	world.recordHistory(attacker, now)
	world.recordHistory(victim, now)
	world.ApplyInput(attacker.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	world.Step(now)
	for i := 1; i <= 60 && victim.Health == balance.MaxHealth; i++ {
		world.Step(now.Add(time.Duration(i) * time.Second / 60))
	}
	return balance.MaxHealth - victim.Health
}

// The versioning invariant, end to end: the same persisted character records
// are replayed against the shipped tables and against a single edited row. Both
// classes move together, and nothing about the stored characters changes —
// there is no migration step because they hold references, not stats.
func TestOneTableRowRebalancesBothClassesWithoutCharacterMigration(t *testing.T) {
	gunslinger := model.Character{ID: "stored-gunslinger", Name: "Vance", Class: model.Gunslinger, Level: 1}
	mage := model.Character{ID: "stored-mage", Name: "Ilse", Class: model.Mage, Level: 1}
	stored := []model.Character{gunslinger, mage}

	shippedTables := tuning.MustLoad()
	patched := rebalanced(t, 25)

	cases := []struct {
		name             string
		shooter, target  model.Character
		shipped, patched float64
	}{
		{"gunslinger", gunslinger, mage, 0, 0},
		{"mage", mage, gunslinger, 0, 0},
	}
	for index := range cases {
		cases[index].shipped = damageDealt(t, shippedTables, cases[index].shooter, cases[index].target)
		cases[index].patched = damageDealt(t, patched, cases[index].shooter, cases[index].target)
	}
	for _, testCase := range cases {
		if testCase.shipped != 10 {
			t.Fatalf("%s dealt %g on the shipped tables, want the standard band's 10", testCase.name, testCase.shipped)
		}
		if testCase.patched != 25 {
			t.Fatalf("%s dealt %g after the band edit, want 25", testCase.name, testCase.patched)
		}
	}
	if !reflect.DeepEqual([]model.Character{gunslinger, mage}, stored) {
		t.Fatal("the stored character records were mutated; a balance patch must need no migration")
	}
}
