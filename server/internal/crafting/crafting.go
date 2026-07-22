// Package crafting owns recipe-blueprint crafting: which finished weapon a set
// of parts resolves to, what it costs, what makes it legal, and what the item
// changes. It holds no world state — the safe-zone gate needs a position and so
// lives in game.World — and it stores nothing derived: an item is weapon and
// component references, with every implied value read from today's tables.
package crafting

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"

	"spellfire/server/internal/model"
	"spellfire/server/internal/progression"
	"spellfire/server/internal/tuning"
)

// NewItemID mints the identity of a crafted instance. It is random rather than
// derived, because two identical configurations are still two separate items.
func NewItemID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("system random source unavailable: " + err.Error())
	}
	return "itm-" + hex.EncodeToString(b)
}

// Inventory is everything a character may equip from: the permanent unlock
// ledger, which decides what it is allowed to craft and carry, and the crafted
// instances it has already made. The two are separate axes — an unlock is
// permission, an item is a thing — and a slot may name either.
type Inventory struct {
	Ledger progression.Ledger
	Items  []model.CraftedItem
}

// Item finds a crafted instance by ID.
func (i Inventory) Item(id string) (model.CraftedItem, bool) {
	for _, item := range i.Items {
		if item.ID == id {
			return item, true
		}
	}
	return model.CraftedItem{}, false
}

// Equipped resolves a weapon-slot reference to what the character actually
// fights with. The reference is either a bare weapons.json row — the stock
// configuration the starter kit and every level grant hand out — or a crafted
// instance of one. Both answer the same shape, so nothing downstream branches on
// which it was.
//
// It reports false for a reference the character may not fight with: an unknown
// ID, an instance it does not own, or a row its ledger has not unlocked.
func (i Inventory) Equipped(tables *tuning.Tables, id string) (tuning.Weapon, model.CraftedItem, bool) {
	if item, ok := i.Item(id); ok {
		weapon, live := tables.Weapons[item.Weapon]
		if !live || !i.Ledger.Has(item.Weapon) {
			return tuning.Weapon{}, model.CraftedItem{}, false
		}
		return weapon, item, true
	}
	weapon, live := tables.Weapons[id]
	// A row the economy withholds has no stock configuration to carry: the only
	// way to hold one is the crafted instance above, which is what makes its
	// material cost a real gate rather than a suggestion.
	if !live || weapon.RequiresCraft || !i.Ledger.Has(id) {
		return tuning.Weapon{}, model.CraftedItem{}, false
	}
	return weapon, model.CraftedItem{}, true
}

// Slots lists the blank boxes a weapon recipe exposes, in blueprint order.
func Slots(tables *tuning.Tables, weapon tuning.Weapon) []string {
	return append([]string(nil), tables.Components.Blueprints[weapon.Blueprint].Slots...)
}

// Result resolves a complete arrangement of parts to the one weapon recipe it
// matches. This is the rule behind "the way you build it determines the gun":
// the category sent by a UI is only a preview hint and is never authority.
func Result(tables *tuning.Tables, components map[string]string) (string, error) {
	matches := make([]string, 0, 1)
	for _, weaponID := range sortedKeys(tables.Components.Recipes) {
		recipe := tables.Components.Recipes[weaponID]
		blueprint := tables.Components.Blueprints[recipe.Blueprint]
		if len(components) != len(blueprint.Slots) {
			continue
		}
		matched := true
		for _, slot := range blueprint.Slots {
			if !contains(recipe.Slots[slot], components[slot]) {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, weaponID)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("those parts do not complete a known weapon recipe")
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("those parts ambiguously match %s", strings.Join(matches, ", "))
	}
	return matches[0], nil
}

// Validate reports why a requested craft may not be made, in language a player
// can act on. It checks the recipe only: affordability and the safe-zone gate
// are the world's, because both need state this package does not hold.
func Validate(tables *tuning.Tables, class model.Class, inventory Inventory, weaponID string, components map[string]string) error {
	weapon, ok := tables.Weapons[weaponID]
	if !ok {
		return fmt.Errorf("%q is not something you can craft", weaponID)
	}
	if weapon.Class != string(class) {
		return fmt.Errorf("%s is a %s weapon", weapon.Name, weapon.Class)
	}
	if !inventory.Ledger.Has(weaponID) {
		return fmt.Errorf("you have not unlocked %s", weapon.Name)
	}
	recipe, ok := tables.Components.Recipes[weaponID]
	if !ok {
		return fmt.Errorf("%s has no crafting recipe", weapon.Name)
	}
	blueprint, ok := tables.Components.Blueprints[recipe.Blueprint]
	if !ok {
		return fmt.Errorf("%s has no blueprint to build from", weapon.Name)
	}
	// Every blank is required. The complete arrangement, not a category field
	// supplied by the client, is what makes the result this weapon recipe.
	for _, slot := range blueprint.Slots {
		id := components[slot]
		if id == "" {
			return fmt.Errorf("%s still needs a %s", weapon.Name, slot)
		}
		if !contains(recipe.Slots[slot], id) {
			component := tables.Components.Components[id]
			name := id
			if component.Name != "" {
				name = component.Name
			}
			return fmt.Errorf("%s does not belong in the %s recipe's %s slot", name, weapon.Name, slot)
		}
	}
	for _, slot := range sortedKeys(components) {
		id := components[slot]
		if !contains(blueprint.Slots, slot) {
			return fmt.Errorf("%s has no %s slot", blueprint.Name, slot)
		}
		component, ok := tables.Components.Components[id]
		if !ok {
			return fmt.Errorf("%q is not a component", id)
		}
		if component.Blueprint != weapon.Blueprint {
			return fmt.Errorf("%s does not fit a %s", component.Name, blueprint.Name)
		}
		if component.Slot != slot {
			return fmt.Errorf("%s fills the %s slot, not %s", component.Name, component.Slot, slot)
		}
	}
	if recipe.Blueprint == "staff" {
		crystal := tables.Components.Components[components["crystal"]]
		stave := tables.Components.Components[components["stave"]]
		if crystal.Kind != "mana_crystal" || stave.Kind != "stave" {
			return fmt.Errorf("a staff requires one mana crystal and one stave")
		}
		if stave.Tier < crystal.Tier {
			return fmt.Errorf("%s is tier %d and cannot hold the tier %d %s", stave.Name, stave.Tier, crystal.Tier, crystal.Name)
		}
	}
	return nil
}

// Cost is the material ID → count one build consumes: the weapon row's own cost
// plus every component filling a slot. Most rows are free — the unlock ledger is
// what gates which categories a character may build, and materials price the
// parts — but the heavy categories carry a cost of their own, which is how rare
// materials gate them economically rather than statistically.
func Cost(tables *tuning.Tables, weaponID string, components map[string]string) map[string]int {
	cost := map[string]int{}
	for material, count := range tables.Weapons[weaponID].Cost {
		cost[material] += count
	}
	for _, slot := range sortedKeys(components) {
		for material, count := range tables.Components.Components[components[slot]].Cost {
			cost[material] += count
		}
	}
	return cost
}

// ValidateAmmunition reports why a requested batch of special ammunition may not
// be built. Unlike a weapon it is not gated by the unlock ledger: the recipe
// belongs to the class, and what gates the launcher is the launcher's own unlock
// and material cost.
func ValidateAmmunition(tables *tuning.Tables, class model.Class, id string) (tuning.Ammunition, error) {
	recipe, ok := tables.Ammunition[id]
	if !ok {
		return tuning.Ammunition{}, fmt.Errorf("%q is not ammunition you can build", id)
	}
	if recipe.Class != string(class) {
		return tuning.Ammunition{}, fmt.Errorf("%s is %s ammunition", recipe.Name, recipe.Class)
	}
	return recipe, nil
}

// Shortfall is what is still missing to pay a cost from a carried inventory, and
// is empty when the craft is affordable. The crafting UI shows it per material
// rather than as one refusal, because "you need three more" is actionable and
// "you cannot afford this" is not.
func Shortfall(cost, carried map[string]int) map[string]int {
	short := map[string]int{}
	for material, count := range cost {
		if missing := count - carried[material]; missing > 0 {
			short[material] = missing
		}
	}
	return short
}

// Spend deducts a cost from a carried inventory in place, dropping stacks it
// empties so a save never records a material the character no longer holds. The
// caller must have checked Shortfall first; spending more than is carried is a
// programming error rather than a player-facing one.
func Spend(carried, cost map[string]int) {
	for material, count := range cost {
		if carried[material] -= count; carried[material] <= 0 {
			delete(carried, material)
		}
	}
}

// Modifiers merges every component's modifiers into one multiplier per
// attribute. Two components touching the same attribute multiply, so a build
// that stacks two reload penalties pays both.
func Modifiers(tables *tuning.Tables, components map[string]string) map[string]float64 {
	merged := map[string]float64{}
	for _, slot := range sortedKeys(components) {
		for attribute, modifier := range tables.Components.Components[components[slot]].Modifiers {
			if current, ok := merged[attribute]; ok {
				merged[attribute] = current * modifier
				continue
			}
			merged[attribute] = modifier
		}
	}
	return merged
}

// Apply derives what a crafted instance actually fights with: the weapon row and
// the ability it drives, each scaled by the components filling its slots. Both
// come back together because they constrain each other — a magazine and the
// round it spends have to stay coherent — and neither is ever stored.
//
// An empty component map is the stock configuration and returns the rows
// unchanged, which is what an unmodified starter weapon resolves to.
func Apply(tables *tuning.Tables, weapon tuning.Weapon, ability tuning.Ability, components map[string]string) (tuning.Weapon, tuning.Ability) {
	modifiers := Modifiers(tables, components)
	rarity := tables.RarityMultiplier(components)
	if len(modifiers) == 0 && rarity == 1 {
		return weapon, ability
	}
	ability.DamageMultiplier = ability.DamageScale() * rarity
	if weapon.MagazineSize > 0 {
		weapon.MagazineSize = scaleCount(weapon.MagazineSize, modifiers[tuning.AttrMagazineSize])
		weapon.ReloadMS = scaleCount(weapon.ReloadMS, modifiers[tuning.AttrReloadMS])
	}
	weapon = applyHandling(weapon, modifiers)
	// A windup that scaled to zero would erase the telegraph its dodge vector
	// depends on, so it keeps at least one millisecond of warning.
	if ability.WindupMS > 0 {
		ability.WindupMS = scaleCount(ability.WindupMS, modifiers[tuning.AttrWindupMS])
	}
	if ability.CooldownMS > 0 {
		ability.CooldownMS = scaleCount(ability.CooldownMS, modifiers[tuning.AttrCooldownMS])
	}
	ability.DamageMultiplier = scaleFromOne(ability.DamageScale(), modifiers[tuning.AttrSpellDamage])
	ability.HealingMultiplier = scaleFromOne(ability.HealingScale(), modifiers[tuning.AttrSpellHealing])
	ability.EffectiveHealthMultiplier = scaleFromOne(ability.EffectiveHealthScale(), modifiers[tuning.AttrEffectiveHealth])
	ability.Cost = scaleCost(ability.Cost, modifiers[tuning.AttrCostAmount], weapon.MagazineSize)
	if ability.Projectile != nil {
		// The projectile is a pointer into the shared table, so it is copied
		// before it is scaled: modifying it in place would retune the row for
		// every other character firing the same ability.
		projectile := *ability.Projectile
		projectile.Speed = scale(projectile.Speed, modifiers[tuning.AttrProjectileSpeed])
		projectile.LifeSeconds = scale(projectile.LifeSeconds, modifiers[tuning.AttrProjectileLife])
		projectile.Radius = scale(projectile.Radius, modifiers[tuning.AttrProjectileRadius])
		ability.Projectile = &projectile
	}
	if ability.Deployable != nil {
		field := *ability.Deployable
		field.DamageMultiplier = ability.DamageScale()
		ability.Deployable = &field
	}
	return weapon, applyArea(ability, modifiers[tuning.AttrAreaRadius])
}

// applyArea widens what a cast covers: the blast, the field it leaves, and the
// telegraph that warns about both, together. Widening the area without the
// figure that draws it would let a crystal grow a danger zone the target never
// saw, which is the one thing the telegraph contract does not allow.
//
// Every one of these is a pointer into the shared table row, so each is copied
// before it is scaled.
func applyArea(ability tuning.Ability, modifier float64) tuning.Ability {
	if modifier == 0 || modifier == 1 {
		return ability
	}
	if ability.Blast != nil {
		blast := *ability.Blast
		blast.Radius *= modifier
		ability.Blast = &blast
	}
	if ability.Deployable != nil {
		field := *ability.Deployable
		field.Radius *= modifier
		field.RevealRadius *= modifier
		ability.Deployable = &field
	}
	if ability.Telegraph != nil {
		telegraph := *ability.Telegraph
		telegraph.Radius *= modifier
		telegraph.Length *= modifier
		telegraph.Width *= modifier
		ability.Telegraph = &telegraph
	}
	return ability
}

// Bias applies a mana crystal's element specialisation to one cast. It is
// separate from Apply because only the loadout knows which element the selected
// slot delivers: a staff's parts are the same whichever spell is cast through
// them, and the bias is the one modifier that is not.
func Bias(tables *tuning.Tables, ability tuning.Ability, element string, components map[string]string) tuning.Ability {
	if element == "" {
		return ability
	}
	for _, slot := range sortedKeys(components) {
		component := tables.Components.Components[components[slot]]
		if component.Element != element {
			continue
		}
		if modifier := component.Modifiers[tuning.AttrElementDamage]; modifier != 0 {
			ability.DamageMultiplier = ability.DamageScale() * modifier
			ability.HealingMultiplier = ability.HealingScale() * modifier
			if ability.Deployable != nil {
				field := *ability.Deployable
				field.DamageMultiplier = field.DamageScale() * modifier
				ability.Deployable = &field
			}
		}
	}
	return ability
}

func scaleFromOne(value, modifier float64) float64 {
	if modifier == 0 {
		return value
	}
	return value * modifier
}

// applyHandling scales a gun's gunplay: how far the muzzle walks, how wide it
// throws standing and moving, and how freely a scoped body may move. The recoil
// pattern and the scope are copied before they are scaled, for the same reason
// the projectile is: both are read straight off the shared table row, and
// scaling one in place would retune the row for every other character carrying
// the same category.
func applyHandling(weapon tuning.Weapon, modifiers map[string]float64) tuning.Weapon {
	if modifier := modifiers[tuning.AttrRecoilDegrees]; modifier != 0 && len(weapon.Recoil.Pattern) > 0 {
		pattern := make([]float64, len(weapon.Recoil.Pattern))
		for index, degrees := range weapon.Recoil.Pattern {
			pattern[index] = degrees * modifier
		}
		weapon.Recoil.Pattern = pattern
	}
	weapon.Spread.StandingDegrees = scale(weapon.Spread.StandingDegrees, modifiers[tuning.AttrSpreadDegrees])
	weapon.Spread.MovingDegrees = scale(weapon.Spread.MovingDegrees, modifiers[tuning.AttrMoveSpreadDegrees])
	if modifier := modifiers[tuning.AttrScopeMovement]; modifier != 0 && weapon.Scoped() {
		scope := *weapon.Scope
		// A scope that let its user move as freely as an unscoped one would erase
		// the committed vulnerability the whole mode is balanced on.
		scope.MovementMultiplier = math.Min(0.95, scope.MovementMultiplier*modifier)
		weapon.Scope = &scope
	}
	return weapon
}

// scaleCost charges what the components made one use cost. A magazine spends
// whole rounds and can never be asked for more than it holds, which is the same
// invariant the loader enforces on an unmodified row.
func scaleCost(cost tuning.Cost, modifier float64, magazine int) tuning.Cost {
	if modifier == 0 || cost.Amount == 0 {
		return cost
	}
	cost.Amount = cost.Amount * modifier
	if cost.Kind != tuning.CostAmmo {
		return cost
	}
	cost.Amount = math.Max(1, math.Round(cost.Amount))
	if magazine > 0 {
		cost.Amount = math.Min(cost.Amount, float64(magazine))
	}
	return cost
}

// scale applies a multiplier, treating an absent one as no change so an
// attribute no component named is left exactly as the table authored it.
func scale(value, modifier float64) float64 {
	if modifier == 0 {
		return value
	}
	return value * modifier
}

// scaleCount scales a whole-numbered attribute and never rounds it away: a
// magazine of one round, a reload, and a windup all have to stay meaningful.
func scaleCount(value int, modifier float64) int {
	if modifier == 0 {
		return value
	}
	scaled := int(math.Round(float64(value) * modifier))
	if scaled < 1 {
		return 1
	}
	return scaled
}

// Describe states in plain language what a set of component choices does, one
// line per filled slot and in blueprint order. It is what the crafting UI shows
// instead of a raw table of multipliers.
func Describe(tables *tuning.Tables, weapon tuning.Weapon, components map[string]string) []string {
	lines := make([]string, 0, len(components))
	for _, slot := range Slots(tables, weapon) {
		component, ok := tables.Components.Components[components[slot]]
		if !ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", component.Name, component.Effect))
	}
	return lines
}

// Name is what a crafted instance is called: its weapon row's name, since the
// category is what a player recognises it by and the components are shown
// beside it rather than folded into a generated title.
func Name(tables *tuning.Tables, item model.CraftedItem) string {
	return tables.Weapons[item.Weapon].Name
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
