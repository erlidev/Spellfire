// Package loadout owns the equipped set: which content fills which slot, what
// arrangement is legal, and how a saved arrangement is carried onto today's
// content. It holds no world state, so the simulation and the store agree on
// one answer without either owning the rule.
//
// The economy's keystone rule — a loadout may only be changed inside a safe
// zone — is not here, because it needs a position. `game.World.SetLoadout`
// enforces it; everything about *what* is a legal set lives in this package.
package loadout

import (
	"fmt"
	"sort"
	"strings"

	"spellfire/server/internal/crafting"
	"spellfire/server/internal/model"
	"spellfire/server/internal/progression"
	"spellfire/server/internal/tuning"
)

// Slot kinds. A Gunslinger's bar is one weapon followed by gadget slots; a
// Mage's is spell slots, with its staff cast through whichever spell is
// selected. Both lay out to the same width, which the tables validate.
const (
	KindWeapon   = "weapon"
	KindGadget   = "gadget"
	KindSpell    = "spell"
	KindKeystone = "keystone"
)

// Slot is one selectable position on the action bar, resolved against the
// tables. An empty slot carries its kind and nothing else.
type Slot struct {
	Index     int
	Kind      string
	ID        string
	Name      string
	AbilityID string
	Element   string
	// Item is the crafted instance a weapon slot holds, and is empty when the
	// slot holds a stock weapon row. It is what the simulation applies component
	// modifiers from.
	Item model.CraftedItem
}

// Filled reports whether the slot holds content that can be used.
func (s Slot) Filled() bool { return s.ID != "" }

// Bar lays an equipped set out as the selectable action bar, in binding order.
// It is the single answer to "what does the use button do", shared by the
// simulation and the menu.
func Bar(tables *tuning.Tables, class model.Class, inventory crafting.Inventory, set model.Loadout) []Slot {
	table := tables.Loadout
	slots := make([]Slot, 0, table.BarSlots())
	if class == model.Gunslinger {
		slots = append(slots, weaponSlot(tables, inventory, 0, set.Weapon))
		for index := 0; index < table.GadgetSlots; index++ {
			slots = append(slots, gadgetSlot(tables, len(slots), at(set.Gadgets, index)))
		}
		return slots
	}
	staff, item, _ := inventory.Equipped(tables, set.Weapon)
	for index := 0; index < table.SpellSlots; index++ {
		slots = append(slots, spellSlot(tables, staff, item, index, at(set.Spells, index)))
	}
	return slots
}

// weaponSlot resolves the one weapon slot. The reference is either a stock
// weapon row or a crafted instance of one; both answer with the row's ability,
// and the instance rides along so the simulation can apply its components.
func weaponSlot(tables *tuning.Tables, inventory crafting.Inventory, index int, id string) Slot {
	weapon, item, ok := inventory.Equipped(tables, id)
	if !ok {
		return Slot{Index: index, Kind: KindWeapon}
	}
	return Slot{Index: index, Kind: KindWeapon, ID: id, Name: weapon.Name, AbilityID: weapon.Ability, Item: item}
}

func gadgetSlot(tables *tuning.Tables, index int, id string) Slot {
	slot := Slot{Index: index, Kind: KindGadget, ID: id}
	gadget, ok := tables.Gadgets[id]
	if !ok {
		return Slot{Index: index, Kind: KindGadget}
	}
	slot.Name, slot.AbilityID = gadget.Name, gadget.Ability
	return slot
}

// spellSlot resolves one of a Mage's slots. The staff is the delivery device
// and the spell is what it casts, so an empty slot on a Mage falls back to the
// staff's own declared spell only in slot zero — that keeps a Mage whose
// loadout has been emptied by a content withdrawal still able to fight.
func spellSlot(tables *tuning.Tables, staff tuning.Weapon, item model.CraftedItem, index int, id string) Slot {
	if id == "" && index == 0 {
		id = staff.Spell
	}
	slot := Slot{Index: index, Kind: KindSpell, ID: id, Item: item}
	spell, ok := tables.Spells[id]
	if !ok {
		return Slot{Index: index, Kind: KindSpell}
	}
	slot.Name, slot.AbilityID, slot.Element = spell.Name, spell.Ability, spell.Element
	return slot
}

// Default is the set a character fights with before it has chosen one: what it
// owns, packed from slot zero. A character's ledger always contains a weapon of
// its class — the starter kit draws one — so the result is a fightable set
// rather than an empty bar, and dropIllegal guarantees it is a legal one.
func Default(tables *tuning.Tables, class model.Class, inventory crafting.Inventory) model.Loadout {
	table := tables.Loadout
	set := model.Loadout{
		Gadgets:   make([]string, table.GadgetSlots),
		Spells:    make([]string, table.SpellSlots),
		Keystones: make([]string, table.KeystoneSlots),
		Version:   tables.Manifest.Version,
	}
	set.Weapon = defaultWeapon(tables, class, inventory)
	if class == model.Gunslinger {
		fill(set.Gadgets, Equippable(tables, class, inventory, KindGadget))
	} else {
		fill(set.Spells, Equippable(tables, class, inventory, KindSpell))
	}
	fill(set.Keystones, Equippable(tables, class, inventory, KindKeystone))
	dropIllegal(tables, class, &set)
	return set
}

// defaultWeapon is the weapon a character falls back to: the first stock row of
// its class it owns, or the class starter when a content withdrawal has left it
// owning none. Being unarmed is not a state any rule allows, so this never
// answers empty while the tables have a starter. It never falls back to a
// crafted instance: the default is the plain configuration, and which crafted
// weapon to carry is a choice the player makes.
func defaultWeapon(tables *tuning.Tables, class model.Class, inventory crafting.Inventory) string {
	if owned := stockWeapons(tables, class, inventory); len(owned) > 0 {
		return owned[0]
	}
	if starter, ok := tables.StarterWeapon(string(class)); ok {
		return starter.ID
	}
	return ""
}

// Resolve carries a saved set onto today's content and guarantees the result is
// legal. Retired IDs follow the retirement chain; anything that no longer
// resolves, that a content change has made illegal, or that the character does
// not own is unequipped rather than dropping the whole set. It reports whether
// the character is owed a respec: a balance patch bumped the manifest, or the
// set itself had to change.
func Resolve(tables *tuning.Tables, class model.Class, inventory crafting.Inventory, saved model.Loadout) (model.Loadout, bool) {
	if saved.Empty() {
		return Default(tables, class, inventory), false
	}
	table := tables.Loadout
	set := model.Loadout{
		Weapon:    resolveWeapon(tables, class, inventory, saved.Weapon),
		Gadgets:   resize(saved.Gadgets, table.GadgetSlots),
		Spells:    resize(saved.Spells, table.SpellSlots),
		Keystones: resize(saved.Keystones, table.KeystoneSlots),
		Version:   tables.Manifest.Version,
	}
	for index, id := range set.Gadgets {
		set.Gadgets[index] = owned(tables, inventory, KindGadget, id)
	}
	for index, id := range set.Spells {
		set.Spells[index] = owned(tables, inventory, KindSpell, id)
	}
	for index, id := range set.Keystones {
		set.Keystones[index] = owned(tables, inventory, KindKeystone, id)
	}
	dropIllegal(tables, class, &set)
	changed := saved.Version != tables.Manifest.Version || !equal(saved, set)
	return set, changed
}

// resolveWeapon carries a saved weapon slot onto today's content. A crafted
// instance the character still owns is kept as it is; a stock row follows its
// retirement chain like any other reference. Anything that no longer resolves,
// belongs to the other class, or is not owned falls back to the default, because
// a withdrawn weapon leaving the character unarmed is a state no rule allows.
func resolveWeapon(tables *tuning.Tables, class model.Class, inventory crafting.Inventory, saved string) string {
	if weapon, _, ok := inventory.Equipped(tables, saved); ok && weapon.Class == string(class) {
		return saved
	}
	if _, isItem := inventory.Item(saved); !isItem {
		if id := owned(tables, inventory, KindWeapon, saved); id != "" && tables.Weapons[id].Class == string(class) {
			return id
		}
	}
	return defaultWeapon(tables, class, inventory)
}

// stockWeapons lists the plain weapon rows of a class the ledger owns, in stable
// order. A row that may only be carried as a crafted instance has no stock
// configuration and never appears here.
func stockWeapons(tables *tuning.Tables, class model.Class, inventory crafting.Inventory) []string {
	ids := make([]string, 0, len(tables.Weapons))
	for id, weapon := range tables.Weapons {
		if weapon.Class == string(class) && !weapon.RequiresCraft && inventory.Ledger.Has(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// dropIllegal unequips whatever keeps a set from validating, highest slot
// first, so the deterministic casualty is the last thing equipped rather than
// the character's signature. It terminates because each pass empties one slot.
func dropIllegal(tables *tuning.Tables, class model.Class, set *model.Loadout) {
	if class == model.Gunslinger {
		set.Spells = make([]string, tables.Loadout.SpellSlots)
	} else {
		set.Gadgets = make([]string, tables.Loadout.GadgetSlots)
	}
	deduplicate(set.Gadgets)
	deduplicate(set.Spells)
	deduplicate(set.Keystones)
	for index, id := range set.Keystones {
		if id != "" && tables.Keystones[id].Class != string(class) {
			set.Keystones[index] = ""
		}
	}
	for pass := 0; pass < len(set.Spells); pass++ {
		index := worstAffinity(tables, set.Spells)
		if index < 0 {
			return
		}
		set.Spells[index] = ""
	}
}

// worstAffinity finds the highest-indexed slot whose spell does not have the
// same-element company its tier requires, and -1 when every slot is satisfied.
func worstAffinity(tables *tuning.Tables, spells []string) int {
	for index := len(spells) - 1; index >= 0; index-- {
		if spells[index] == "" {
			continue
		}
		if affinityShortfall(tables, spells, index) > 0 {
			return index
		}
	}
	return -1
}

// affinityShortfall is how many more same-element spells the slot's tier needs
// beside it. Zero means the slot is satisfied.
func affinityShortfall(tables *tuning.Tables, spells []string, index int) int {
	spell, ok := tables.Spells[spells[index]]
	if !ok {
		return 0
	}
	required := tables.Loadout.RequiredSameElement(spell.Tier)
	company := 0
	for other, id := range spells {
		if other == index || id == "" {
			continue
		}
		if tables.Spells[id].Element == spell.Element {
			company++
		}
	}
	if company >= required {
		return 0
	}
	return required - company
}

// Validate reports why a requested set may not be equipped, in language a
// player can act on. It is the authority the mutation path runs before it
// commits, and it never consults the world: legality is a property of the set.
func Validate(tables *tuning.Tables, class model.Class, inventory crafting.Inventory, set model.Loadout) error {
	table := tables.Loadout
	ledger := inventory.Ledger
	weapon, _, ok := inventory.Equipped(tables, set.Weapon)
	if !ok {
		// The slot names either a stock row or a crafted instance, so a failure
		// here is one of three things: nothing of that name, an instance another
		// character owns, or a category this one has not unlocked. Naming the row
		// when there is one keeps the message actionable.
		if item, isItem := inventory.Item(set.Weapon); isItem {
			if row, live := tables.Weapons[item.Weapon]; live {
				return fmt.Errorf("you have not unlocked %s", row.Name)
			}
		}
		if row, live := tables.Weapons[set.Weapon]; live {
			if row.RequiresCraft {
				return fmt.Errorf("%s has to be built before it can be carried", row.Name)
			}
			return fmt.Errorf("you have not unlocked %s", row.Name)
		}
		return fmt.Errorf("%q is not a weapon you can equip", set.Weapon)
	}
	if weapon.Class != string(class) {
		return fmt.Errorf("%s is a %s weapon", weapon.Name, weapon.Class)
	}
	if len(set.Gadgets) > table.GadgetSlots {
		return fmt.Errorf("a loadout holds %d gadget slots, not %d", table.GadgetSlots, len(set.Gadgets))
	}
	if len(set.Spells) > table.SpellSlots {
		return fmt.Errorf("a loadout holds %d spell slots, not %d", table.SpellSlots, len(set.Spells))
	}
	if len(set.Keystones) > table.KeystoneSlots {
		return fmt.Errorf("a loadout holds %d keystone slots, not %d", table.KeystoneSlots, len(set.Keystones))
	}
	if class == model.Mage && filled(set.Gadgets) > 0 {
		return fmt.Errorf("a Mage equips spells, not gadgets")
	}
	if class == model.Gunslinger && filled(set.Spells) > 0 {
		return fmt.Errorf("a Gunslinger equips gadgets, not spells")
	}
	if err := checkSlots(ledger, set.Gadgets, "gadget", func(id string) (string, bool) {
		gadget, ok := tables.Gadgets[id]
		return gadget.Name, ok && gadget.Class == string(class)
	}); err != nil {
		return err
	}
	if err := checkSlots(ledger, set.Spells, "spell", func(id string) (string, bool) {
		spell, ok := tables.Spells[id]
		return spell.Name, ok
	}); err != nil {
		return err
	}
	if err := checkSlots(ledger, set.Keystones, "keystone", func(id string) (string, bool) {
		keystone, ok := tables.Keystones[id]
		return keystone.Name, ok && keystone.Class == string(class)
	}); err != nil {
		return err
	}
	for index := range set.Spells {
		if set.Spells[index] == "" {
			continue
		}
		if shortfall := affinityShortfall(tables, set.Spells, index); shortfall > 0 {
			spell := tables.Spells[set.Spells[index]]
			element := tables.Elements[spell.Element].Name
			return fmt.Errorf("%s is tier %d, so it needs %d more %s spell%s beside it",
				spell.Name, spell.Tier, shortfall, element, plural(shortfall))
		}
	}
	return nil
}

// checkSlots rejects unknown, unowned, or duplicated content in one kind of
// slot.
func checkSlots(ledger progression.Ledger, ids []string, kind string, lookup func(string) (string, bool)) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		name, ok := lookup(id)
		if !ok {
			return fmt.Errorf("%q is not a %s you can equip", id, kind)
		}
		if !ledger.Has(id) {
			return fmt.Errorf("you have not unlocked %s", name)
		}
		if seen[id] {
			return fmt.Errorf("%s is already equipped in another slot", name)
		}
		seen[id] = true
	}
	return nil
}

// Equippable lists the content of a slot kind a character may choose from, in
// stable order: the live rows of the right class that its ledger owns. Owning
// more options improves preparation, never the power carried into one fight, so
// this is the only place the ledger narrows.
func Equippable(tables *tuning.Tables, class model.Class, inventory crafting.Inventory, kind string) []string {
	ledger := inventory.Ledger
	var ids []string
	switch kind {
	case KindWeapon:
		// Stock rows first, then the crafted instances of them, so the plain
		// configuration is the deterministic default and a crafted weapon is
		// something the player picks on purpose.
		stock := stockWeapons(tables, class, inventory)
		crafted := make([]string, 0, len(inventory.Items))
		for _, item := range inventory.Items {
			if weapon, ok := tables.Weapons[item.Weapon]; ok && weapon.Class == string(class) && ledger.Has(item.Weapon) {
				crafted = append(crafted, item.ID)
			}
		}
		sort.Strings(crafted)
		return append(stock, crafted...)
	case KindGadget:
		for id, gadget := range tables.Gadgets {
			if gadget.Class == string(class) && ledger.Has(id) {
				ids = append(ids, id)
			}
		}
	case KindSpell:
		if class != model.Mage {
			return nil
		}
		for id := range tables.Spells {
			if ledger.Has(id) {
				ids = append(ids, id)
			}
		}
	case KindKeystone:
		for id, keystone := range tables.Keystones {
			if keystone.Class == string(class) && ledger.Has(id) {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// owned carries one saved slot onto today's content: the retirement chain is
// followed, and the result is kept only if the character actually owns it. A
// slot holding something the ledger does not have empties rather than granting
// it — the ledger is the authority on ownership, not the saved set.
func owned(tables *tuning.Tables, inventory crafting.Inventory, kind, id string) string {
	if id == "" {
		return ""
	}
	resolved, ok := tables.Resolve(kind, id)
	// A refunded retirement has no live replacement, so the slot empties. The
	// refund itself is owed against the material ledger, not the slot.
	if !ok || !inventory.Ledger.Has(resolved.ID) {
		return ""
	}
	return resolved.ID
}

// deduplicate empties the later of any two slots holding the same content, so
// one spell cannot pad its own affinity requirement.
func deduplicate(ids []string) {
	seen := map[string]bool{}
	for index, id := range ids {
		if id == "" {
			continue
		}
		if seen[id] {
			ids[index] = ""
			continue
		}
		seen[id] = true
	}
}

func resize(ids []string, size int) []string {
	sized := make([]string, size)
	copy(sized, ids)
	return sized
}

func fill(slots []string, ids []string) {
	for index := range slots {
		if index >= len(ids) {
			return
		}
		slots[index] = ids[index]
	}
}

func at(ids []string, index int) string {
	if index >= len(ids) {
		return ""
	}
	return ids[index]
}

func filled(ids []string) int {
	count := 0
	for _, id := range ids {
		if id != "" {
			count++
		}
	}
	return count
}

// equal compares two sets by what they equip, ignoring how many trailing empty
// slots each one happens to carry — a record saved before the slot count grew
// equips exactly what a padded one does.
func equal(a, b model.Loadout) bool {
	return a.Weapon == b.Weapon &&
		key(a.Gadgets) == key(b.Gadgets) &&
		key(a.Spells) == key(b.Spells) &&
		key(a.Keystones) == key(b.Keystones)
}

func key(ids []string) string {
	end := len(ids)
	for end > 0 && ids[end-1] == "" {
		end--
	}
	return strings.Join(ids[:end], "\x00")
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
