package crafting_test

import (
	"math"
	"reflect"
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

func build(t *testing.T, tables *tuning.Tables, weaponID string) map[string]string {
	t.Helper()
	recipe := tables.Components.Recipes[weaponID]
	chosen := map[string]string{}
	for _, slot := range tables.Components.Blueprints[recipe.Blueprint].Slots {
		if len(recipe.Slots[slot]) == 0 {
			t.Fatalf("recipe %s has no %s option", weaponID, slot)
		}
		chosen[slot] = recipe.Slots[slot][0]
	}
	return chosen
}

func TestCostSumsEveryFilledSlot(t *testing.T) {
	tables := shipped(t)
	chosen := build(t, tables, "starter-rifle")
	receiver := tables.Components.Components[chosen["receiver"]]
	barrel := tables.Components.Components[chosen["barrel"]]
	cost := crafting.Cost(tables, "starter-rifle", chosen)
	for material, count := range receiver.Cost {
		if cost[material] < count {
			t.Fatalf("cost of %s = %d, want at least the receiver's %d", material, cost[material], count)
		}
	}
	// Two components naming one material must add rather than overwrite, or a
	// build would be cheaper than the parts it is made of.
	for material, count := range barrel.Cost {
		minimum := count + receiver.Cost[material]
		if cost[material] < minimum {
			t.Fatalf("cost of %s = %d, want at least %d", material, cost[material], minimum)
		}
	}
	if len(crafting.Cost(tables, "starter-rifle", chosen)) == 0 {
		t.Fatal("a complete recipe costs no materials")
	}
	// A heavy category's own chassis cost is independent of its required parts.
	if len(crafting.Cost(tables, "long-sniper", map[string]string{})) == 0 {
		t.Fatal("a heavy category costs nothing to build")
	}
}

func TestSignatureRarityRaisesDamageOnceWithoutChangingCadence(t *testing.T) {
	tables := shipped(t)
	gun := tables.Weapons["starter-rifle"]
	base := tables.Abilities[gun.Ability]
	parts := map[string]string{"receiver": "prototype-receiver", "barrel": "prototype-barrel", "action": "prototype-action", "feed": "prototype-feed", "sight": "prototype-sight"}
	_, applied := crafting.Apply(tables, gun, base, parts)
	if applied.DamageScale() != 1.3 {
		t.Fatalf("Signature damage scale = %g, want one rarity multiplier of 1.3", applied.DamageScale())
	}
	if applied.Interval() != base.Interval() {
		t.Fatalf("Signature parts changed cadence from %s to %s", base.Interval(), applied.Interval())
	}

	staff := tables.Weapons["starter-staff"]
	_, defended := crafting.Apply(tables, staff, tables.Abilities[staff.Spell], map[string]string{"crystal": "aegis-prism-crystal", "stave": "oak-stave"})
	if defended.EffectiveHealthScale() != 1.15 {
		t.Fatalf("Aegis effective-health scale = %g, want 1.15", defended.EffectiveHealthScale())
	}
}

func TestStaffRarityAndElementBiasReachPersistentFields(t *testing.T) {
	tables := shipped(t)
	staff := tables.Weapons["starter-staff"]
	base := tables.Abilities["cinder-patch-cast"]
	parts := map[string]string{"crystal": "pyre-heart-crystal", "stave": "oak-stave"}
	_, applied := crafting.Apply(tables, staff, base, parts)
	applied = crafting.Bias(tables, applied, "fire", parts)
	want := 1.08 * 0.94 * 1.25
	if applied.Deployable == nil || math.Abs(applied.Deployable.DamageScale()-want) > 1e-9 {
		t.Fatalf("field damage scale = %#v, want %g", applied.Deployable, want)
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
	gunBuild, staffBuild := build(t, tables, gun.ID), build(t, tables, staff.ID)
	receiver := component(t, tables, "gun", "receiver")
	crystal := component(t, tables, "staff", "crystal")
	inventory := everything(tables)
	if err := crafting.Validate(tables, "gunslinger", inventory, gun.ID, gunBuild); err != nil {
		t.Fatalf("a coherent gun was refused: %v", err)
	}
	if result, err := crafting.Result(tables, gunBuild); err != nil || result != gun.ID {
		t.Fatalf("gun parts resolved to %q: %v", result, err)
	}
	broken := build(t, tables, gun.ID)
	delete(broken, "barrel")
	if err := crafting.Validate(tables, "gunslinger", inventory, gun.ID, broken); err == nil {
		t.Fatal("a gun with an empty blank was accepted")
	}
	gunBuild["crystal"] = crystal.ID
	if err := crafting.Validate(tables, "gunslinger", inventory, gun.ID, gunBuild); err == nil {
		t.Fatal("a gun accepted a slot its blueprint does not expose")
	}
	staffBuild["crystal"] = receiver.ID
	if err := crafting.Validate(tables, "mage", inventory, staff.ID, staffBuild); err == nil {
		t.Fatal("a staff accepted a gun component")
	}
	if err := crafting.Validate(tables, "mage", inventory, gun.ID, build(t, tables, gun.ID)); err == nil {
		t.Fatal("a Mage was allowed to build a gun")
	}
	// The ledger is what gates which categories may be built, so an unowned row
	// must be refused even when the recipe itself is coherent.
	empty := crafting.Inventory{Ledger: progression.New(nil)}
	if err := crafting.Validate(tables, "gunslinger", empty, gun.ID, build(t, tables, gun.ID)); err == nil {
		t.Fatal("a character built a weapon category it has not unlocked")
	}
}

func TestCompletePartsDetermineTheWeaponCategory(t *testing.T) {
	tables := shipped(t)
	for _, weaponID := range []string{"field-pistol", "service-revolver", "compact-smg", "breaching-shotgun", "starter-rifle", "marksman-rifle", "long-sniper", "support-lmg", "field-launcher", "starter-staff"} {
		chosen := build(t, tables, weaponID)
		result, err := crafting.Result(tables, chosen)
		if err != nil || result != weaponID {
			t.Fatalf("%s recipe resolved to %q: %v", weaponID, result, err)
		}
	}
}

func TestStaffRequiresAStaveAtLeastAsStrongAsItsCrystal(t *testing.T) {
	tables := shipped(t)
	chosen := build(t, tables, "starter-staff")
	chosen["crystal"] = "stormglass-crystal"
	chosen["stave"] = "ash-stave"
	if err := crafting.Validate(tables, model.Mage, everything(tables), "starter-staff", chosen); err == nil {
		t.Fatal("a tier 1 stave accepted a tier 3 crystal")
	}
	chosen["stave"] = "ironwood-stave"
	if err := crafting.Validate(tables, model.Mage, everything(tables), "starter-staff", chosen); err != nil {
		t.Fatalf("a tier 3 stave refused a tier 3 crystal: %v", err)
	}
}

func TestManaCrystalEffectsApplyToEverySpellCast(t *testing.T) {
	tables := shipped(t)
	staff := tables.Weapons["starter-staff"]
	ability := tables.Abilities[tables.Spells[staff.Spell].Ability]
	ability.CooldownMS = 1000 // exercises the general cooldown contract even before the full spell grid lands

	_, damaging := crafting.Apply(tables, staff, ability, map[string]string{"crystal": "ember-prism-crystal"})
	if damaging.DamageScale() <= 1 {
		t.Fatalf("damage crystal scale = %g, want an increase", damaging.DamageScale())
	}
	_, healing := crafting.Apply(tables, staff, ability, map[string]string{"crystal": "mercy-pearl-crystal"})
	if healing.HealingScale() <= 1 {
		t.Fatalf("healing crystal scale = %g, want an increase", healing.HealingScale())
	}
	_, quickened := crafting.Apply(tables, staff, ability, map[string]string{"crystal": "quartz-clock-crystal"})
	if quickened.CooldownMS >= ability.CooldownMS {
		t.Fatalf("quickened cooldown = %d, want less than %d", quickened.CooldownMS, ability.CooldownMS)
	}
}

// Crafting moves handling and ceiling. This asserts the applied direction of a
// modifier rather than a magnitude, so a balance edit retunes the row without
// breaking the test.
func TestApplyScalesTheAttributesComponentsName(t *testing.T) {
	tables := shipped(t)
	gun := starter(t, tables, "gunslinger")
	ability := tables.Abilities[gun.Ability]
	for _, id := range tables.ComponentsFor("gun", "feed") {
		row := tables.Components.Components[id]
		modifier, ok := row.Modifiers[tuning.AttrMagazineSize]
		if !ok {
			continue
		}
		weapon, _ := crafting.Apply(tables, gun, ability, map[string]string{"feed": id})
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
	if !reflect.DeepEqual(weapon, gun) || applied.Projectile != ability.Projectile || applied.Cost != ability.Cost {
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

// A crystal that widens what a cast covers must widen the figure that warns
// about it too. An area that grew past its telegraph would be damage the target
// was never shown.
func TestAreaCrystalWidensTheFieldAndItsTelegraphTogether(t *testing.T) {
	tables := shipped(t)
	staff := tables.Weapons["starter-staff"]
	ability := tables.Abilities["cinder-patch-cast"]
	if ability.Deployable == nil || ability.Telegraph == nil {
		t.Fatal("the placed field row lost its field or its telegraph")
	}
	_, widened := crafting.Apply(tables, staff, ability, map[string]string{"crystal": "geomancer-crystal"})
	if widened.Deployable.Radius <= ability.Deployable.Radius {
		t.Fatalf("field radius = %g, want more than %g", widened.Deployable.Radius, ability.Deployable.Radius)
	}
	if widened.Telegraph.Radius <= ability.Telegraph.Radius {
		t.Fatalf("telegraph radius = %g, want more than %g", widened.Telegraph.Radius, ability.Telegraph.Radius)
	}
	// The rows are pointers into the tables, so widening one build must never
	// widen the ability for everyone casting it.
	if tables.Abilities["cinder-patch-cast"].Deployable.Radius != ability.Deployable.Radius {
		t.Fatal("applying an area crystal edited the shared field row in place")
	}
}

// An element bias is the specialisation a rarer crystal has to earn: it lifts
// one school and nothing else.
func TestElementBiasAppliesOnlyToItsSchool(t *testing.T) {
	tables := shipped(t)
	parts := map[string]string{"crystal": "pyre-heart-crystal", "stave": "oak-stave"}
	crystal := tables.Components.Components["pyre-heart-crystal"]
	ability := tables.Abilities["fire-bolt-cast"]

	biased := crafting.Bias(tables, ability, crystal.Element, parts)
	if biased.DamageScale() <= 1 {
		t.Fatalf("a fire spell through a fire crystal scales by %g", biased.DamageScale())
	}
	other := crafting.Bias(tables, ability, "frost", parts)
	if other.DamageScale() != ability.DamageScale() {
		t.Fatalf("a frost spell through a fire crystal scales by %g", other.DamageScale())
	}
	// A slot with no element — a gun or a gadget — is never biased.
	if none := crafting.Bias(tables, ability, "", parts); none.DamageScale() != ability.DamageScale() {
		t.Fatal("an elementless slot picked up a crystal's bias")
	}
}
