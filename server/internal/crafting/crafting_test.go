package crafting_test

import (
	"testing"

	"spellfire/server/internal/crafting"
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

// everything is a character that has unlocked all live content, so ownership is
// never the reason a recipe case passes or fails.
func everything(tables *tuning.Tables, items ...model.CraftedItem) crafting.Inventory {
	return crafting.Inventory{
		Ledger: progression.New(tables.UnlocksThrough(tables.Progression.MaxLevel)),
		Items:  items,
	}
}

// gunComponent finds a live component filling a slot of a blueprint, so the
// tests follow the tables rather than hard-coding a row that a balance pass may
// rename.
func component(t *testing.T, tables *tuning.Tables, blueprint, slot string) tuning.Component {
	t.Helper()
	for _, id := range tables.ComponentsFor(blueprint, slot) {
		return tables.Components.Components[id]
	}
	t.Fatalf("no component fills %s/%s", blueprint, slot)
	return tuning.Component{}
}

func TestCostSumsEveryFilledSlot(t *testing.T) {
	tables := shipped(t)
	muzzle := component(t, tables, "gun", "muzzle")
	barrel := component(t, tables, "gun", "barrel")
	cost := crafting.Cost(tables, map[string]string{"muzzle": muzzle.ID, "barrel": barrel.ID})
	for material, count := range muzzle.Cost {
		if cost[material] < count {
			t.Fatalf("cost of %s = %d, want at least the muzzle's %d", material, cost[material], count)
		}
	}
	// Two components naming one material must add rather than overwrite, or a
	// build would be cheaper than the parts it is made of.
	for material, count := range barrel.Cost {
		want := count + muzzle.Cost[material]
		if cost[material] != want {
			t.Fatalf("cost of %s = %d, want %d", material, cost[material], want)
		}
	}
	if len(crafting.Cost(tables, map[string]string{})) != 0 {
		t.Fatal("a stock build costs materials")
	}
}

func TestShortfallNamesWhatIsMissing(t *testing.T) {
	cost := map[string]int{"salvaged-plate": 5, "tempered-plate": 2}
	short := crafting.Shortfall(cost, map[string]int{"salvaged-plate": 3})
	if short["salvaged-plate"] != 2 || short["tempered-plate"] != 2 {
		t.Fatalf("shortfall = %v, want two of each", short)
	}
	if len(crafting.Shortfall(cost, map[string]int{"salvaged-plate": 5, "tempered-plate": 9})) != 0 {
		t.Fatal("an affordable build reported a shortfall")
	}
}

func TestSpendEmptiesStacksItExhausts(t *testing.T) {
	carried := map[string]int{"salvaged-plate": 5, "tempered-plate": 2}
	crafting.Spend(carried, map[string]int{"salvaged-plate": 5, "tempered-plate": 1})
	if _, ok := carried["salvaged-plate"]; ok {
		t.Fatalf("an exhausted stack survived the spend: %v", carried)
	}
	if carried["tempered-plate"] != 1 {
		t.Fatalf("carried = %v, want one tempered plate left", carried)
	}
}

// The recipe rules are the ones a player can hit by hand: a slot the blueprint
// does not expose, a component from another blueprint, and one in the wrong
// slot.
func TestValidateRefusesIncoherentRecipes(t *testing.T) {
	tables := shipped(t)
	gun, staff := starter(t, tables, "gunslinger"), starter(t, tables, "mage")
	muzzle := component(t, tables, "gun", "muzzle")
	core := component(t, tables, "staff", "core")
	inventory := everything(tables)
	if err := crafting.Validate(tables, "gunslinger", inventory, gun.ID, map[string]string{"muzzle": muzzle.ID}); err != nil {
		t.Fatalf("a coherent gun was refused: %v", err)
	}
	if err := crafting.Validate(tables, "gunslinger", inventory, gun.ID, map[string]string{}); err != nil {
		t.Fatalf("a stock build was refused: %v", err)
	}
	if err := crafting.Validate(tables, "gunslinger", inventory, gun.ID, map[string]string{"core": core.ID}); err == nil {
		t.Fatal("a gun accepted a slot its blueprint does not expose")
	}
	if err := crafting.Validate(tables, "mage", inventory, staff.ID, map[string]string{"core": muzzle.ID}); err == nil {
		t.Fatal("a staff accepted a gun component")
	}
	if err := crafting.Validate(tables, "gunslinger", inventory, gun.ID, map[string]string{"barrel": muzzle.ID}); err == nil {
		t.Fatal("a muzzle was accepted into the barrel slot")
	}
	if err := crafting.Validate(tables, "mage", inventory, gun.ID, nil); err == nil {
		t.Fatal("a Mage was allowed to build a gun")
	}
	// The ledger is what gates which categories may be built, so an unowned row
	// must be refused even when the recipe itself is coherent.
	empty := crafting.Inventory{Ledger: progression.New(nil)}
	if err := crafting.Validate(tables, "gunslinger", empty, gun.ID, nil); err == nil {
		t.Fatal("a character built a weapon category it has not unlocked")
	}
}

// Crafting moves handling and ceiling. This asserts the applied direction of a
// modifier rather than a magnitude, so a balance edit retunes the row without
// breaking the test.
func TestApplyScalesTheAttributesComponentsName(t *testing.T) {
	tables := shipped(t)
	gun := starter(t, tables, "gunslinger")
	ability := tables.Abilities[gun.Ability]
	for _, id := range tables.ComponentsFor("gun", "magazine") {
		row := tables.Components.Components[id]
		modifier, ok := row.Modifiers[tuning.AttrMagazineSize]
		if !ok {
			continue
		}
		weapon, _ := crafting.Apply(tables, gun, ability, map[string]string{"magazine": id})
		if modifier > 1 && weapon.MagazineSize <= gun.MagazineSize {
			t.Fatalf("%s scales the magazine by %g but left %d rounds", id, modifier, weapon.MagazineSize)
		}
		if modifier < 1 && weapon.MagazineSize >= gun.MagazineSize {
			t.Fatalf("%s scales the magazine by %g but left %d rounds", id, modifier, weapon.MagazineSize)
		}
		if weapon.MagazineSize < 1 {
			t.Fatalf("%s rounded the magazine away to %d", id, weapon.MagazineSize)
		}
	}
}

// The shared projectile row is a pointer into the tables. Applying components
// must never write through it, or one character's crafted weapon would retune
// the ability for everyone firing it.
func TestApplyNeverMutatesTheSharedTables(t *testing.T) {
	tables := shipped(t)
	gun := starter(t, tables, "gunslinger")
	ability := tables.Abilities[gun.Ability]
	if ability.Projectile == nil {
		t.Skip("the starter gun fires no projectile")
	}
	before := *ability.Projectile
	barrel := component(t, tables, "gun", "barrel")
	if _, modified := crafting.Apply(tables, gun, ability, map[string]string{"barrel": barrel.ID}); modified.Projectile == ability.Projectile {
		t.Fatal("the modified ability shares the table's projectile row")
	}
	if *tables.Abilities[gun.Ability].Projectile != before {
		t.Fatal("applying components edited the shared projectile row in place")
	}
}

// A stock build must resolve to exactly the table rows, because that is what an
// unmodified starter weapon is.
func TestApplyLeavesAStockBuildUntouched(t *testing.T) {
	tables := shipped(t)
	gun := starter(t, tables, "gunslinger")
	ability := tables.Abilities[gun.Ability]
	weapon, applied := crafting.Apply(tables, gun, ability, nil)
	if weapon != gun || applied.Projectile != ability.Projectile || applied.Cost != ability.Cost {
		t.Fatal("a stock build changed the rows it was built from")
	}
}

// A weapon slot names either a stock row or a crafted instance, and an instance
// belongs to the character that made it.
func TestEquippedResolvesRowsAndInstances(t *testing.T) {
	tables := shipped(t)
	gun := starter(t, tables, "gunslinger")
	item := model.CraftedItem{ID: "itm-1", CharacterID: "c1", Weapon: gun.ID, Components: map[string]string{}}
	inventory := everything(tables, item)
	if weapon, instance, ok := inventory.Equipped(tables, gun.ID); !ok || instance.ID != "" || weapon.ID != gun.ID {
		t.Fatalf("a stock row resolved to %v/%v (ok=%t)", weapon.ID, instance.ID, ok)
	}
	if weapon, instance, ok := inventory.Equipped(tables, item.ID); !ok || instance.ID != item.ID || weapon.ID != gun.ID {
		t.Fatalf("an owned instance resolved to %v/%v (ok=%t)", weapon.ID, instance.ID, ok)
	}
	if _, _, ok := inventory.Equipped(tables, "itm-someone-else"); ok {
		t.Fatal("an instance the character does not own resolved")
	}
	// An instance of a category the ledger does not own is not equippable: the
	// item is a thing, the unlock is the permission, and both are required.
	unowned := crafting.Inventory{Ledger: progression.New(nil), Items: []model.CraftedItem{item}}
	if _, _, ok := unowned.Equipped(tables, item.ID); ok {
		t.Fatal("an instance of an unlocked-away category stayed equippable")
	}
}

func starter(t *testing.T, tables *tuning.Tables, class string) tuning.Weapon {
	t.Helper()
	weapon, ok := tables.StarterWeapon(class)
	if !ok {
		t.Fatalf("no starter weapon for %s", class)
	}
	return weapon
}
