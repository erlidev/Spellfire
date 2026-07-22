package game

import (
	"testing"

	"spellfire/server/internal/crafting"
	"spellfire/server/internal/model"
	"spellfire/server/internal/tuning"
)

func weaponBuild(t *testing.T, w *World, weaponID string) map[string]string {
	t.Helper()
	recipe := w.tuning.Tables.Components.Recipes[weaponID]
	chosen := map[string]string{}
	for _, slot := range w.tuning.Tables.Components.Blueprints[recipe.Blueprint].Slots {
		if len(recipe.Slots[slot]) == 0 {
			t.Fatalf("recipe %s has no %s option", weaponID, slot)
		}
		chosen[slot] = recipe.Slots[slot][0]
	}
	return chosen
}

func fundBuild(w *World, p *Player, weaponID string, chosen map[string]string) map[string]int {
	cost := crafting.Cost(w.tuning.Tables, weaponID, chosen)
	for material, count := range cost {
		p.Materials[material] += count
	}
	return cost
}

func TestCraftSpendsMaterialsAndKeepsTheItem(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger}, now)
	p.Position = Vec{}
	chosen := weaponBuild(t, world, p.Loadout.Weapon)
	feed := world.tuning.Tables.Components.Components[chosen["feed"]]
	fundBuild(world, p, p.Loadout.Weapon, chosen)
	item, err := world.Craft("g1", CraftRequest{Weapon: p.Loadout.Weapon, Components: chosen}, "itm-test")
	if err != nil {
		t.Fatalf("an affordable craft inside the hub was refused: %v", err)
	}
	if item.Weapon != p.Loadout.Weapon || item.Components["feed"] != feed.ID {
		t.Fatalf("crafted item = %+v", item)
	}
	if len(p.Materials) != 0 {
		t.Fatalf("materials left after paying the exact cost: %v", p.Materials)
	}
	if len(p.Items) != 1 || p.Items[0].ID != "itm-test" {
		t.Fatalf("owned items = %+v", p.Items)
	}
	// The item is a reference pair, never a stat snapshot, so equipping it must
	// derive the modified weapon from today's tables.
	set := p.Loadout.Clone()
	set.Weapon = item.ID
	if _, err := world.SetLoadout("g1", set, now); err != nil {
		t.Fatalf("a crafted weapon could not be equipped: %v", err)
	}
	stock := world.tuning.Tables.Weapons[item.Weapon]
	crafted, ok := world.weapon(p)
	if !ok {
		t.Fatal("an equipped crafted weapon resolved to nothing")
	}
	if modifier := feed.Modifiers[tuning.AttrMagazineSize]; modifier != 0 && crafted.MagazineSize == stock.MagazineSize {
		t.Fatalf("a magazine component scaled by %g left %d rounds", modifier, crafted.MagazineSize)
	}
	// Committing the loadout reloads the weapon, so the magazine the body holds
	// must be the crafted one rather than the row's.
	if p.Ammo != crafted.MagazineSize {
		t.Fatalf("ammo = %d, want the crafted magazine's %d", p.Ammo, crafted.MagazineSize)
	}
}

// The keystone economy rule: raw materials have to be hauled back to safety
// before they become anything.
func TestCraftIsRefusedOutsideSafety(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger}, now)
	chosen := weaponBuild(t, world, p.Loadout.Weapon)
	cost := fundBuild(world, p, p.Loadout.Weapon, chosen)
	p.Position = Vec{X: world.tuning.SafeRadius + 10}
	if _, err := world.Craft("g1", CraftRequest{Weapon: p.Loadout.Weapon, Components: chosen}, "itm-test"); err != ErrCraftingLocked {
		t.Fatalf("crafting outside safety returned %v, want the safe-zone refusal", err)
	}
	if len(p.Items) != 0 {
		t.Fatal("a refused craft left an item behind")
	}
	for material, count := range cost {
		if p.Materials[material] != count {
			t.Fatalf("a refused craft spent %s", material)
		}
	}
}

// A refusal must spend nothing. Naming what is missing is the whole point of
// the shortfall message, so it is checked rather than a bare failure.
func TestCraftRefusesAndSpendsNothingWhenMaterialsAreShort(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger}, now)
	p.Position = Vec{}
	chosen := weaponBuild(t, world, p.Loadout.Weapon)
	cost := fundBuild(world, p, p.Loadout.Weapon, chosen)
	for material := range cost {
		p.Materials[material]--
		if p.Materials[material] == 0 {
			delete(p.Materials, material)
		}
		break
	}
	carried := p.CarriedMaterials()
	_, err := world.Craft("g1", CraftRequest{Weapon: p.Loadout.Weapon, Components: chosen}, "itm-test")
	if err == nil {
		t.Fatal("a craft the character could not pay for succeeded")
	}
	if len(p.Items) != 0 {
		t.Fatal("a refused craft left an item behind")
	}
	for material, count := range carried {
		if p.Materials[material] != count {
			t.Fatalf("a refused craft spent %s: %d, want %d", material, p.Materials[material], count)
		}
	}
}

// Weapon type comes from a complete arrangement; an empty generic blueprint is
// not a free stock craft.
func TestCraftRefusesAnIncompleteBlueprint(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "m1", Name: "Mage", Class: model.Mage}, now)
	p.Position = Vec{}
	if _, err := world.Craft("m1", CraftRequest{Weapon: p.Loadout.Weapon}, "itm-stock"); err == nil {
		t.Fatal("an empty blueprint produced a staff")
	}
	if len(p.Items) != 0 {
		t.Fatal("an incomplete craft left an item")
	}
}

// A staff is the delivery device, so its components change the cast — and a
// gadget slot, which the weapon has no part in throwing, must not be touched.
func TestStaffComponentsChangeTheCastTheyDeliver(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "m1", Name: "Mage", Class: model.Mage}, now)
	p.Position = Vec{}
	var crystal tuning.Component
	for _, id := range world.tuning.Tables.ComponentsFor("staff", "crystal") {
		if row := world.tuning.Tables.Components.Components[id]; row.Modifiers[tuning.AttrCostAmount] != 0 {
			crystal = row
			break
		}
	}
	if crystal.ID == "" {
		t.Skip("no mana crystal changes what a cast costs")
	}
	chosen := weaponBuild(t, world, p.Loadout.Weapon)
	chosen["crystal"] = crystal.ID
	for _, id := range world.tuning.Tables.Components.Recipes[p.Loadout.Weapon].Slots["stave"] {
		if stave := world.tuning.Tables.Components.Components[id]; stave.Tier >= crystal.Tier {
			chosen["stave"] = id
			break
		}
	}
	fundBuild(world, p, p.Loadout.Weapon, chosen)
	item, err := world.Craft("m1", CraftRequest{Weapon: p.Loadout.Weapon, Components: chosen}, "itm-staff")
	if err != nil {
		t.Fatalf("crafting a staff was refused: %v", err)
	}
	stock, ok := world.ability(p)
	if !ok {
		t.Fatal("a fresh Mage resolves to no ability")
	}
	set := p.Loadout.Clone()
	set.Weapon = item.ID
	if _, err := world.SetLoadout("m1", set, now); err != nil {
		t.Fatalf("a crafted staff could not be equipped: %v", err)
	}
	crafted, ok := world.ability(p)
	if !ok {
		t.Fatal("an equipped crafted staff resolves to no ability")
	}
	if crafted.Cost.Amount == stock.Cost.Amount {
		t.Fatalf("a crystal scaling mana cost by %g left it at %g", crystal.Modifiers[tuning.AttrCostAmount], crafted.Cost.Amount)
	}
	if crafted.DamageBand != stock.DamageBand {
		t.Fatal("crafting moved the damage band, which is the one thing it may never do")
	}
}

// Crafted items survive a disconnect and stay equippable, because they are
// permanent like unlocks rather than carried like materials.
func TestCraftedItemsRejoinWithTheCharacter(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger}, now)
	p.Position = Vec{}
	chosen := weaponBuild(t, world, p.Loadout.Weapon)
	fundBuild(world, p, p.Loadout.Weapon, chosen)
	item, err := world.Craft("g1", CraftRequest{Weapon: p.Loadout.Weapon, Components: chosen}, "itm-kept")
	if err != nil {
		t.Fatalf("craft: %v", err)
	}
	set := p.Loadout.Clone()
	set.Weapon = item.ID
	if _, err := world.SetLoadout("g1", set, now); err != nil {
		t.Fatalf("equip: %v", err)
	}
	state, _ := world.StateOf("g1", now)
	world.RemovePlayer("g1")
	rejoined := world.AddPlayer(model.Character{
		ID: "g1", Name: "Gun", Class: model.Gunslinger, State: state, Items: []model.CraftedItem{item},
	}, now)
	if rejoined.Loadout.Weapon != item.ID {
		t.Fatalf("a crafted weapon was unequipped on rejoin: %q", rejoined.Loadout.Weapon)
	}
	// Without the item, the same saved set must fall back to a stock row rather
	// than leaving the character unarmed.
	world.RemovePlayer("g1")
	stripped := world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger, State: state}, now)
	if stripped.Loadout.Weapon == "" || stripped.Loadout.Weapon == item.ID {
		t.Fatalf("a withdrawn item left the character holding %q", stripped.Loadout.Weapon)
	}
}

func TestPreRevampPartsResolveOntoLiveSlotsOnJoin(t *testing.T) {
	world, now := testWorld()
	legacy := model.CraftedItem{
		ID: "itm-legacy", CharacterID: "g1", Weapon: "starter-rifle",
		Components: map[string]string{"muzzle": "muzzle-brake", "magazine": "extended-magazine"},
	}
	p := world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger, Items: []model.CraftedItem{legacy}}, now)
	if len(p.Items) != 1 {
		t.Fatalf("legacy item was lost: %+v", p.Items)
	}
	for slot, id := range p.Items[0].Components {
		part := world.tuning.Tables.Components.Components[id]
		if part.ID == "" || part.Slot != slot {
			t.Fatalf("legacy component %q remained in stale slot %q", id, slot)
		}
	}
	if p.Items[0].Components["barrel"] == "" || p.Items[0].Components["feed"] == "" {
		t.Fatalf("legacy slots resolved to %+v", p.Items[0].Components)
	}
}

// The material grant is developer-mode only, and the world still validates every
// ID and bound rather than trusting the caller.
func TestGrantMaterialsValidatesAgainstTheCatalog(t *testing.T) {
	world, now := testWorld()
	world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger}, now)
	bound := world.tuning.Tables.Materials.AdminGrant
	if _, err := world.GrantMaterials("g1", map[string]int{"not-a-material": 1}); err == nil {
		t.Fatal("an unknown material was granted")
	}
	if _, err := world.GrantMaterials("g1", map[string]int{"salvaged-plate": int(*bound.Maximum) + 1}); err == nil {
		t.Fatal("a grant past the catalog bound was accepted")
	}
	carried, err := world.GrantMaterials("g1", map[string]int{"salvaged-plate": 5})
	if err != nil {
		t.Fatalf("a bounded grant was refused: %v", err)
	}
	if carried["salvaged-plate"] != 5 {
		t.Fatalf("carried = %v", carried)
	}
}

// Crafted gear is permanent, so the capacity is the outcome the crafting UI
// owes a full inventory.
func TestCraftRefusesPastTheInventoryCapacity(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger}, now)
	p.Position = Vec{}
	capacity := world.tuning.Tables.Progression.CraftedItemCapacity
	chosen := weaponBuild(t, world, p.Loadout.Weapon)
	for index := 0; index < capacity; index++ {
		fundBuild(world, p, p.Loadout.Weapon, chosen)
		if _, err := world.Craft("g1", CraftRequest{Weapon: p.Loadout.Weapon, Components: chosen}, "itm-"+string(rune('a'+index))); err != nil {
			t.Fatalf("craft %d of %d was refused: %v", index+1, capacity, err)
		}
	}
	if _, err := world.Craft("g1", CraftRequest{Weapon: p.Loadout.Weapon, Components: chosen}, "itm-over"); err == nil {
		t.Fatalf("a %dth crafted weapon was accepted past the capacity of %d", capacity+1, capacity)
	}
}

// A developer fixture has no character row to hang an item off and nothing about
// it is ever saved, so it is not an economy.
func TestAdminFixturesCannotCraft(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "fixture", Name: "Training", Class: model.Gunslinger}, now)
	p.Position, p.AdminSpawned = Vec{}, true
	if _, err := world.Craft("fixture", CraftRequest{Weapon: p.Loadout.Weapon}, "itm-x"); err != ErrCraftingUnavailable {
		t.Fatalf("a developer fixture crafted an item: %v", err)
	}
}
