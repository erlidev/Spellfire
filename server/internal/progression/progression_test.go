package progression_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"spellfire/data"
	"spellfire/server/internal/loadout"
	"spellfire/server/internal/model"
	"spellfire/server/internal/progression"
	"spellfire/server/internal/tuning"
)

func shipped(t *testing.T) *tuning.Tables {
	t.Helper()
	tables, err := tuning.Load()
	if err != nil {
		t.Fatalf("load tables: %v", err)
	}
	return tables
}

// edited parses the shipped tables with some files replaced, which is how these
// tests exercise a wider basic set than the game currently ships without
// authoring balance nobody has settled.
func edited(t *testing.T, files map[string]string) *tuning.Tables {
	t.Helper()
	overlay := fstest.MapFS{}
	entries, err := data.Tuning.ReadDir("tuning")
	if err != nil {
		t.Fatalf("read tuning: %v", err)
	}
	for _, entry := range entries {
		raw, err := data.Tuning.ReadFile("tuning/" + entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		overlay["tuning/"+entry.Name()] = &fstest.MapFile{Data: raw}
	}
	for name, contents := range files {
		overlay["tuning/"+name] = &fstest.MapFile{Data: []byte(contents)}
	}
	tables, err := tuning.Parse(overlay)
	if err != nil {
		t.Fatalf("parse edited tables: %v", err)
	}
	return tables
}

// The starter kit's whole purpose: a character with zero materials is
// combat-capable the moment it is created, never a spectator waiting on drops.
func TestAZeroMaterialCharacterCanFillACoherentLoadout(t *testing.T) {
	tables := edited(t, map[string]string{"spells.json": basicSpells, "gadgets.json": basicGadgets})
	for _, class := range []model.Class{model.Gunslinger, model.Mage} {
		ledger := progression.New(progression.StarterKit(tables, class, "fresh-character"))
		set := loadout.Default(tables, class, ledger)
		if err := loadout.Validate(tables, class, ledger, set); err != nil {
			t.Fatalf("%s starter kit does not produce a legal loadout: %v", class, err)
		}
		slots := loadout.Bar(tables, class, set)
		if slots[0].AbilityID == "" {
			t.Fatalf("%s cannot act from its first slot on creation", class)
		}
		// Every slot the class's own content fills must be filled: the kit is
		// sized to the bar, so a new character is not fighting with holes in it.
		filled := 0
		for _, slot := range slots {
			if slot.Filled() {
				filled++
			}
		}
		if filled != len(slots) {
			t.Fatalf("%s filled %d of %d slots from its kit: %+v", class, filled, len(slots), set)
		}
	}
}

// Randomisation is the point: two characters get different opening tools, and
// one character always gets the same kit, so a re-roll is never a re-draw.
func TestTheStarterKitIsRandomPerCharacterAndStablePerCharacter(t *testing.T) {
	tables := edited(t, map[string]string{"spells.json": basicSpells})
	first := progression.StarterKit(tables, model.Mage, "character-one")
	if again := progression.StarterKit(tables, model.Mage, "character-one"); strings.Join(again, ",") != strings.Join(first, ",") {
		t.Fatalf("the same character drew %v and then %v", first, again)
	}
	differs := false
	for _, id := range []string{"character-two", "character-three", "character-four", "character-five"} {
		if strings.Join(progression.StarterKit(tables, model.Mage, id), ",") != strings.Join(first, ",") {
			differs = true
			break
		}
	}
	if !differs {
		t.Fatal("every character drew an identical kit; the draw is not randomised")
	}
	// Nothing drawn is exclusive: the pool is the basic set, every row of which
	// also carries an unlock level.
	for _, id := range first {
		if kind, ok := tables.UnlockKind(id); !ok {
			t.Fatalf("kit entry %q (%s) is not live content", id, kind)
		}
	}
}

// XP is the only thing that moves a level, and a level is the only thing a
// grant hangs off. Surplus XP carries rather than being discarded.
func TestAdvanceLevelsAndGrantsWhatTheLevelUnlocks(t *testing.T) {
	tables := shipped(t)
	cost := tables.Progression.XPToNext(1)
	level, xp, granted := progression.Advance(tables, 1, 0, cost-1)
	if level != 1 || xp != cost-1 || len(granted) > 0 {
		t.Fatalf("one XP short of the level = level %d, xp %d, granted %v", level, xp, granted)
	}
	level, xp, granted = progression.Advance(tables, 1, cost-1, 5)
	if level != 2 {
		t.Fatalf("level = %d, want 2", level)
	}
	if xp != 4 {
		t.Fatalf("surplus xp = %d, want the 4 that overflowed the level", xp)
	}
	if len(granted) == 0 {
		t.Fatal("reaching level 2 granted nothing; the level drives no unlock")
	}
	for _, id := range granted {
		if _, ok := tables.UnlockKind(id); !ok {
			t.Fatalf("level 2 granted %q, which is not live content", id)
		}
	}
}

// A level cap that kept banking XP would leave a number in the record that
// nothing reads and that no future edit can be trusted to interpret.
func TestAdvanceStopsBankingXPAtTheCap(t *testing.T) {
	tables := shipped(t)
	cap := tables.Progression.MaxLevel
	level, xp, _ := progression.Advance(tables, cap, 0, 10_000)
	if level != cap || xp != 0 {
		t.Fatalf("at the cap = level %d, xp %d; want the cap and no banked XP", level, xp)
	}
}

// Sync is what carries a ledger onto today's content: an entry whose row was
// retired follows the retirement, and everything the character's level has come
// to grant is added, so content added at a level it is already past is not lost.
func TestSyncFollowsRetirementAndGrantsWhatTheLevelAlreadyPassed(t *testing.T) {
	tables := edited(t, map[string]string{
		"retired.json": `{"old-bolt": {"kind": "spell", "replacement": "fire-bolt", "note": "renamed"}}`,
	})
	ledger, changed := progression.Sync(tables, model.Mage, "veteran", 5, []string{"old-bolt"})
	if !ledger.Has("fire-bolt") {
		t.Fatalf("a retired unlock did not resolve to its replacement: %v", ledger.IDs())
	}
	if !ledger.Has("starter-staff") {
		t.Fatalf("level 5 did not grant what level 2 unlocks: %v", ledger.IDs())
	}
	if !changed {
		t.Fatal("a ledger that gained entries was not reported as changed")
	}
	if _, changed := progression.Sync(tables, model.Mage, "veteran", 5, ledger.IDs()); changed {
		t.Fatal("an already-current ledger was reported as changed and would be rewritten every join")
	}
}

// A record written before the ledger existed has no unlocks at all. It is given
// a kit rather than left unable to equip anything.
func TestSyncGivesAKitToARecordThatPredatesTheLedger(t *testing.T) {
	tables := shipped(t)
	ledger, changed := progression.Sync(tables, model.Gunslinger, "legacy", 1, nil)
	if ledger.Len() == 0 || !changed {
		t.Fatalf("a ledgerless character was left with %d unlocks (changed %v)", ledger.Len(), changed)
	}
	if len(loadout.Equippable(tables, model.Gunslinger, ledger, loadout.KindWeapon)) == 0 {
		t.Fatal("a ledgerless character was left unable to equip any weapon")
	}
}

// The basic sets the shipped tables do not have yet. The spell pool is wider
// than the draw, which is what makes the draw observable; the gadget pool is
// exactly the five slots a Gunslinger has.
const basicSpells = `{
  "bolt-1": {"name": "Fire bolt", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"},
  "bolt-2": {"name": "Frost shard", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"},
  "bolt-3": {"name": "Spark", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"},
  "bolt-4": {"name": "Arcane dart", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"},
  "bolt-5": {"name": "Pebble", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"},
  "bolt-6": {"name": "Ember", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"},
  "bolt-7": {"name": "Cinder", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"},
  "bolt-8": {"name": "Flare", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"},
  "bolt-9": {"name": "Scorch", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"},
  "fire-bolt": {"name": "Kindle", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"}
}`

const basicGadgets = `{
  "smoke":     {"name": "Smoke canister", "class": "gunslinger", "starter": true, "unlock_level": 2, "ability": "rifle-shot"},
  "flash":     {"name": "Flashbang", "class": "gunslinger", "starter": true, "unlock_level": 2, "ability": "rifle-shot"},
  "shield":    {"name": "Riot shield", "class": "gunslinger", "starter": true, "unlock_level": 2, "ability": "rifle-shot"},
  "charge":    {"name": "Breaching charge", "class": "gunslinger", "starter": true, "unlock_level": 2, "ability": "rifle-shot"},
  "beacon":    {"name": "Marker beacon", "class": "gunslinger", "starter": true, "unlock_level": 2, "ability": "rifle-shot"}
}`
