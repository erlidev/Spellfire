package loadout_test

import (
	"testing"
	"testing/fstest"

	"spellfire/data"
	"spellfire/server/internal/crafting"
	"spellfire/server/internal/loadout"
	"spellfire/server/internal/model"
	"spellfire/server/internal/progression"
	"spellfire/server/internal/tuning"
)

// kit is the ledger a freshly created character owns, which is what every test
// about the default set and the starter kit must run against.
func kit(tables *tuning.Tables, class model.Class) crafting.Inventory {
	return crafting.Inventory{Ledger: progression.New(progression.StarterKit(tables, class, "ledger-test"))}
}

// everything is the ledger of a character that has unlocked all live content.
// Tests about class, affinity, and retirement rules use it so ownership is not
// the reason a case passes or fails.
func everything(tables *tuning.Tables) crafting.Inventory {
	return crafting.Inventory{Ledger: progression.New(tables.UnlocksThrough(tables.Progression.MaxLevel))}
}

func shipped(t *testing.T) *tuning.Tables {
	t.Helper()
	tables, err := tuning.Load()
	if err != nil {
		t.Fatalf("load tables: %v", err)
	}
	return tables
}

// edited parses the shipped tables with some files replaced, which is how these
// tests exercise content the game does not ship yet — a fourth-tier spell, a
// gadget — without authoring balance nobody has settled.
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

func TestDefaultIsEquippableForBothClasses(t *testing.T) {
	tables := shipped(t)
	for _, class := range []model.Class{model.Gunslinger, model.Mage} {
		set := loadout.Default(tables, class, kit(tables, class))
		if err := loadout.Validate(tables, class, kit(tables, class), set); err != nil {
			t.Fatalf("%s default loadout is invalid: %v", class, err)
		}
		slots := loadout.Bar(tables, class, kit(tables, class), set)
		if len(slots) != tables.Loadout.BarSlots() {
			t.Fatalf("%s bar has %d slots, want %d", class, len(slots), tables.Loadout.BarSlots())
		}
		// The starter kit must be able to fight immediately: slot one always
		// resolves to something the use button can perform.
		if slots[0].AbilityID == "" {
			t.Fatalf("%s first slot performs nothing", class)
		}
	}
}

func TestBarLaysClassesOutOverTheSameBindings(t *testing.T) {
	tables := shipped(t)
	gunslinger := loadout.Bar(tables, model.Gunslinger, kit(tables, model.Gunslinger), loadout.Default(tables, model.Gunslinger, kit(tables, model.Gunslinger)))
	mage := loadout.Bar(tables, model.Mage, kit(tables, model.Mage), loadout.Default(tables, model.Mage, kit(tables, model.Mage)))
	if len(gunslinger) != len(mage) {
		t.Fatalf("bars differ: gunslinger %d, mage %d", len(gunslinger), len(mage))
	}
	if gunslinger[0].Kind != loadout.KindWeapon {
		t.Fatalf("gunslinger slot one is %q, want the weapon", gunslinger[0].Kind)
	}
	for _, slot := range gunslinger[1:] {
		if slot.Kind != loadout.KindGadget {
			t.Fatalf("gunslinger slot %d is %q, want a gadget", slot.Index, slot.Kind)
		}
	}
	for _, slot := range mage {
		if slot.Kind != loadout.KindSpell {
			t.Fatalf("mage slot %d is %q, want a spell", slot.Index, slot.Kind)
		}
	}
}

// The affinity rule is the Mage's specialisation cost: a tier-N spell needs N−1
// others of its element in the same set.
func TestAffinityGatesHighTierSpells(t *testing.T) {
	tables := edited(t, map[string]string{"spells.json": grid})
	signature := model.Loadout{Weapon: "starter-staff", Spells: []string{"fire-4", "", "", "", "", ""}}
	if err := loadout.Validate(tables, model.Mage, everything(tables), signature); err == nil {
		t.Fatal("a lone tier-4 spell was accepted; affinity is not enforced")
	}
	built := model.Loadout{Weapon: "starter-staff", Spells: []string{"fire-4", "fire-bolt", "fire-2", "fire-3", "", ""}}
	if err := loadout.Validate(tables, model.Mage, everything(tables), built); err != nil {
		t.Fatalf("the 4 + 2 build its own rule describes was refused: %v", err)
	}
	// Padding with the same spell twice must not satisfy the requirement.
	padded := model.Loadout{Weapon: "starter-staff", Spells: []string{"fire-4", "fire-bolt", "fire-bolt", "fire-bolt", "", ""}}
	if err := loadout.Validate(tables, model.Mage, everything(tables), padded); err == nil {
		t.Fatal("a duplicated spell was accepted as its own affinity company")
	}
}

func TestValidateRejectsCrossClassAndUnknownContent(t *testing.T) {
	tables := shipped(t)
	cases := map[string]model.Loadout{
		"a mage weapon on a gunslinger": {Weapon: "starter-staff"},
		"an unknown weapon":             {Weapon: "no-such-gun"},
		"spells on a gunslinger":        {Weapon: "starter-rifle", Spells: []string{"fire-bolt"}},
		"an unknown gadget":             {Weapon: "starter-rifle", Gadgets: []string{"no-such-gadget"}},
		"more slots than exist":         {Weapon: "starter-rifle", Gadgets: make([]string, tables.Loadout.GadgetSlots+1)},
	}
	for name, set := range cases {
		if err := loadout.Validate(tables, model.Gunslinger, everything(tables), set); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

// A retired row must never confiscate a slot silently: it resolves to its
// replacement, and only an ID that reaches nothing empties the slot.
func TestResolveFollowsRetirementAndGrantsRespec(t *testing.T) {
	tables := edited(t, map[string]string{
		"spells.json":  grid,
		"retired.json": `{"old-bolt": {"kind": "spell", "replacement": "fire-bolt", "note": "renamed"}}`,
	})
	saved := model.Loadout{Weapon: "starter-staff", Spells: []string{"old-bolt", "", "", "", "", ""}, Version: tables.Manifest.Version}
	set, respec := loadout.Resolve(tables, model.Mage, everything(tables), saved)
	if set.Spells[0] != "fire-bolt" {
		t.Fatalf("retired spell resolved to %q, want the replacement", set.Spells[0])
	}
	if !respec {
		t.Fatal("a set the content changed under did not grant a respec")
	}
	if err := loadout.Validate(tables, model.Mage, everything(tables), set); err != nil {
		t.Fatalf("resolved set is invalid: %v", err)
	}
}

// A balance patch entitles every character to the global respec, which here
// means the set is re-validated and reported rather than silently carried.
func TestResolveGrantsRespecOnAContentVersionBump(t *testing.T) {
	tables := shipped(t)
	saved := loadout.Default(tables, model.Mage, kit(tables, model.Mage))
	if _, respec := loadout.Resolve(tables, model.Mage, everything(tables), saved); respec {
		t.Fatal("an unchanged set at the current version granted a respec")
	}
	saved.Version = tables.Manifest.Version - 1
	set, respec := loadout.Resolve(tables, model.Mage, everything(tables), saved)
	if !respec {
		t.Fatal("a set saved before a balance patch was not granted a respec")
	}
	if set.Version != tables.Manifest.Version {
		t.Fatalf("resolved set is stamped %d, want %d", set.Version, tables.Manifest.Version)
	}
}

// An arrangement a content change made illegal must be repaired, not carried
// into the world and not dropped whole.
func TestResolveDropsWhatNoLongerValidates(t *testing.T) {
	tables := edited(t, map[string]string{"spells.json": grid})
	saved := model.Loadout{Weapon: "starter-staff", Spells: []string{"fire-bolt", "fire-4", "", "", "", ""}}
	set, respec := loadout.Resolve(tables, model.Mage, everything(tables), saved)
	if !respec {
		t.Fatal("a repaired set did not grant a respec")
	}
	if set.Spells[1] != "" {
		t.Fatalf("the unsupportable tier-4 spell survived as %q", set.Spells[1])
	}
	if set.Spells[0] != "fire-bolt" {
		t.Fatalf("a legal slot was dropped: %q", set.Spells[0])
	}
	if err := loadout.Validate(tables, model.Mage, everything(tables), set); err != nil {
		t.Fatalf("resolved set is still invalid: %v", err)
	}
}

func TestResolveRearmsACharacterWhoseWeaponWasWithdrawn(t *testing.T) {
	tables := shipped(t)
	set, respec := loadout.Resolve(tables, model.Gunslinger, everything(tables), model.Loadout{Weapon: "withdrawn-gun"})
	// The fallback is a stock row of the class the character owns, never a
	// category the economy withholds until it has been built.
	rearmed, live := tables.Weapons[set.Weapon]
	if !live || rearmed.Class != "gunslinger" || rearmed.RequiresCraft {
		t.Fatalf("weapon resolved to %q, want a carryable gunslinger row", set.Weapon)
	}
	if !respec {
		t.Fatal("a rearmed character was not granted a respec")
	}
}

func TestGadgetsFillTheGunslingerBar(t *testing.T) {
	tables := edited(t, map[string]string{
		"gadgets.json": `{"smoke": {"name": "Smoke canister", "class": "gunslinger", "starter": true, "unlock_level": 2, "ability": "rifle-shot"}}`,
	})
	set := loadout.Default(tables, model.Gunslinger, kit(tables, model.Gunslinger))
	if set.Gadgets[0] != "smoke" {
		t.Fatalf("starter gadget was not equipped: %v", set.Gadgets)
	}
	slots := loadout.Bar(tables, model.Gunslinger, kit(tables, model.Gunslinger), set)
	if slots[1].Name != "Smoke canister" || slots[1].AbilityID == "" {
		t.Fatalf("gadget slot resolved to %+v", slots[1])
	}
	if err := loadout.Validate(tables, model.Mage, everything(tables), model.Loadout{Weapon: "starter-staff", Gadgets: []string{"smoke"}}); err == nil {
		t.Fatal("a Mage was allowed to equip a gadget")
	}
}

// The ledger is what narrows the equippable set from "every live row" to what
// this character owns. Unowned content must be refused on the mutation path,
// never merely hidden by the menu.
func TestValidateRefusesContentTheLedgerDoesNotOwn(t *testing.T) {
	tables := edited(t, map[string]string{"spells.json": grid})
	owned := crafting.Inventory{Ledger: progression.New([]string{"starter-staff", "fire-bolt", "fire-2", "fire-3"})}
	built := model.Loadout{Weapon: "starter-staff", Spells: []string{"fire-bolt", "fire-2", "fire-3", "", "", ""}}
	if err := loadout.Validate(tables, model.Mage, owned, built); err != nil {
		t.Fatalf("a set built entirely from owned spells was refused: %v", err)
	}
	// fire-4 is legal by affinity here — three Fire spells sit beside it — so
	// only ownership can be what refuses it.
	unowned := model.Loadout{Weapon: "starter-staff", Spells: []string{"fire-4", "fire-bolt", "fire-2", "fire-3", "", ""}}
	if err := loadout.Validate(tables, model.Mage, owned, unowned); err == nil {
		t.Fatal("an unlocked-content check let a character equip a spell it does not own")
	}
	if err := loadout.Validate(tables, model.Mage, everything(tables), unowned); err != nil {
		t.Fatalf("the same set was refused for a character that owns it: %v", err)
	}
	if equippable := loadout.Equippable(tables, model.Mage, owned, loadout.KindSpell); len(equippable) != 3 {
		t.Fatalf("equippable spells = %v, want only the three owned", equippable)
	}
}

// A ledger that shrinks under a content withdrawal must repair the set rather
// than leave a character holding something it no longer owns.
func TestResolveUnequipsContentTheLedgerLost(t *testing.T) {
	tables := edited(t, map[string]string{"spells.json": grid})
	owned := crafting.Inventory{Ledger: progression.New([]string{"starter-staff", "fire-bolt"})}
	saved := model.Loadout{Weapon: "starter-staff", Spells: []string{"fire-bolt", "fire-2", "", "", "", ""}, Version: tables.Manifest.Version}
	set, respec := loadout.Resolve(tables, model.Mage, owned, saved)
	if set.Spells[1] != "" {
		t.Fatalf("an unowned spell survived resolution as %q", set.Spells[1])
	}
	if set.Spells[0] != "fire-bolt" {
		t.Fatalf("an owned spell was dropped: %q", set.Spells[0])
	}
	if !respec {
		t.Fatal("a repaired set did not grant a respec")
	}
}

// grid is a Fire column to tier 4, which is what the affinity rule needs to be
// testable before Phase 2.5 authors the real twenty rows.
const grid = `{
  "fire-bolt": {"name": "Fire bolt", "element": "fire", "tier": 1, "starter": true, "unlock_level": 2, "ability": "fire-bolt-cast"},
  "fire-2":    {"name": "Cinder patch", "element": "fire", "tier": 2, "unlock_level": 4, "ability": "fire-bolt-cast"},
  "fire-3":    {"name": "Flame wave", "element": "fire", "tier": 3, "unlock_level": 7, "ability": "fire-bolt-cast"},
  "fire-4":    {"name": "Firestorm", "element": "fire", "tier": 4, "unlock_level": 11, "ability": "fire-bolt-cast"}
}`

// The weapon slot holds something usable: either a stock row or a crafted
// instance of one. Materials and components never reach the action bar.
func TestWeaponSlotHoldsStockRowsAndCraftedInstances(t *testing.T) {
	tables := shipped(t)
	weapon, ok := tables.StarterWeapon("gunslinger")
	if !ok {
		t.Fatal("no starter gun")
	}
	item := model.CraftedItem{ID: "itm-1", CharacterID: "c1", Weapon: weapon.ID, Components: map[string]string{}}
	inventory := everything(tables)
	inventory.Items = []model.CraftedItem{item}
	set := loadout.Default(tables, model.Gunslinger, inventory)
	// The default is the plain configuration; a crafted weapon is a choice.
	if set.Weapon == item.ID {
		t.Fatal("the default set equipped a crafted instance")
	}
	set.Weapon = item.ID
	if err := loadout.Validate(tables, model.Gunslinger, inventory, set); err != nil {
		t.Fatalf("an owned crafted weapon was refused: %v", err)
	}
	if slot := loadout.Bar(tables, model.Gunslinger, inventory, set)[0]; slot.Item.ID != item.ID || slot.AbilityID != weapon.Ability {
		t.Fatalf("weapon slot = %+v, want the instance over the row's ability", slot)
	}
	// Stock rows sort ahead of instances, so the deterministic first choice is
	// the plain one.
	equippable := loadout.Equippable(tables, model.Gunslinger, inventory, loadout.KindWeapon)
	if equippable[len(equippable)-1] != item.ID || equippable[0] == item.ID {
		t.Fatalf("equippable weapons = %v, want stock rows before instances", equippable)
	}
	// An instance this character does not own is refused rather than hidden.
	stranger := everything(tables)
	if err := loadout.Validate(tables, model.Gunslinger, stranger, set); err == nil {
		t.Fatal("a character equipped an item it does not own")
	}
	// A saved set naming a vanished instance falls back to a stock row: being
	// unarmed is a state no rule allows.
	repaired, respec := loadout.Resolve(tables, model.Gunslinger, stranger, set)
	if repaired.Weapon == "" || repaired.Weapon == item.ID {
		t.Fatalf("resolved weapon = %q, want a stock fallback", repaired.Weapon)
	}
	if !respec {
		t.Fatal("a repaired set did not grant a respec")
	}
}
