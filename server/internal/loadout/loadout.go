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

	"spellfire/server/internal/model"
	"spellfire/server/internal/tuning"
)

// Slot kinds. A Gunslinger's bar is one weapon followed by gadget slots; a
// Mage's is spell slots, with its staff cast through whichever spell is
// selected. Both lay out to the same width, which the tables validate.
const (
	KindWeapon = "weapon"
	KindGadget = "gadget"
	KindSpell  = "spell"
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
}

// Filled reports whether the slot holds content that can be used.
func (s Slot) Filled() bool { return s.ID != "" }

// Bar lays an equipped set out as the selectable action bar, in binding order.
// It is the single answer to "what does the use button do", shared by the
// simulation and the menu.
func Bar(tables *tuning.Tables, class model.Class, set model.Loadout) []Slot {
	table := tables.Loadout
	slots := make([]Slot, 0, table.BarSlots())
	if class == model.Gunslinger {
		slots = append(slots, weaponSlot(tables, 0, set.Weapon))
		for index := 0; index < table.GadgetSlots; index++ {
			slots = append(slots, gadgetSlot(tables, len(slots), at(set.Gadgets, index)))
		}
		return slots
	}
	for index := 0; index < table.SpellSlots; index++ {
		slots = append(slots, spellSlot(tables, tables.Weapons[set.Weapon], index, at(set.Spells, index)))
	}
	return slots
}

func weaponSlot(tables *tuning.Tables, index int, id string) Slot {
	slot := Slot{Index: index, Kind: KindWeapon, ID: id}
	weapon, ok := tables.Weapons[id]
	if !ok {
		return Slot{Index: index, Kind: KindWeapon}
	}
	slot.Name, slot.AbilityID = weapon.Name, weapon.Ability
	return slot
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
func spellSlot(tables *tuning.Tables, staff tuning.Weapon, index int, id string) Slot {
	if id == "" && index == 0 {
		id = staff.Spell
	}
	slot := Slot{Index: index, Kind: KindSpell, ID: id}
	spell, ok := tables.Spells[id]
	if !ok {
		return Slot{Index: index, Kind: KindSpell}
	}
	slot.Name, slot.AbilityID, slot.Element = spell.Name, spell.Ability, spell.Element
	return slot
}

// Default is the set a character fights with before it has chosen one: the
// class starter weapon, and the starter content of its slot kind packed from
// slot zero. Phase 2.2 replaces the flat starter list with a random draw
// against the unlock ledger without changing this shape.
func Default(tables *tuning.Tables, class model.Class) model.Loadout {
	table := tables.Loadout
	set := model.Loadout{
		Gadgets: make([]string, table.GadgetSlots),
		Spells:  make([]string, table.SpellSlots),
		Version: tables.Manifest.Version,
	}
	if weapon, ok := tables.StarterWeapon(string(class)); ok {
		set.Weapon = weapon.ID
	}
	if class == model.Gunslinger {
		fill(set.Gadgets, tables.StarterGadgets(string(class)))
		return set
	}
	fill(set.Spells, tables.StarterSpells())
	return set
}

// Resolve carries a saved set onto today's content and guarantees the result is
// legal. Retired IDs follow the retirement chain; anything that no longer
// resolves, or that a content change has made illegal, is unequipped rather
// than dropping the whole set. It reports whether the character is owed a
// respec: a balance patch bumped the manifest, or the set itself had to change.
func Resolve(tables *tuning.Tables, class model.Class, saved model.Loadout) (model.Loadout, bool) {
	if saved.Empty() {
		return Default(tables, class), false
	}
	table := tables.Loadout
	set := model.Loadout{
		Weapon:  live(tables, KindWeapon, saved.Weapon),
		Gadgets: resize(saved.Gadgets, table.GadgetSlots),
		Spells:  resize(saved.Spells, table.SpellSlots),
		Version: tables.Manifest.Version,
	}
	if weapon, ok := tables.Weapons[set.Weapon]; !ok || weapon.Class != string(class) {
		// A withdrawn weapon leaves the character unarmed, which no rule
		// allows. The class starter is the guaranteed-available replacement.
		set.Weapon = ""
		if starter, ok := tables.StarterWeapon(string(class)); ok {
			set.Weapon = starter.ID
		}
	}
	for index, id := range set.Gadgets {
		set.Gadgets[index] = live(tables, KindGadget, id)
	}
	for index, id := range set.Spells {
		set.Spells[index] = live(tables, KindSpell, id)
	}
	dropIllegal(tables, class, &set)
	changed := saved.Version != tables.Manifest.Version || !equal(saved, set)
	return set, changed
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
func Validate(tables *tuning.Tables, class model.Class, set model.Loadout) error {
	table := tables.Loadout
	weapon, ok := tables.Weapons[set.Weapon]
	if !ok {
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
	if class == model.Mage && filled(set.Gadgets) > 0 {
		return fmt.Errorf("a Mage equips spells, not gadgets")
	}
	if class == model.Gunslinger && filled(set.Spells) > 0 {
		return fmt.Errorf("a Gunslinger equips gadgets, not spells")
	}
	if err := checkSlots(set.Gadgets, "gadget", func(id string) (string, bool) {
		gadget, ok := tables.Gadgets[id]
		return gadget.Name, ok && gadget.Class == string(class)
	}); err != nil {
		return err
	}
	if err := checkSlots(set.Spells, "spell", func(id string) (string, bool) {
		spell, ok := tables.Spells[id]
		return spell.Name, ok
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

// checkSlots rejects unknown or duplicated content in one kind of slot.
func checkSlots(ids []string, kind string, lookup func(string) (string, bool)) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		name, ok := lookup(id)
		if !ok {
			return fmt.Errorf("%q is not a %s you can equip", id, kind)
		}
		if seen[id] {
			return fmt.Errorf("%s is already equipped in another slot", name)
		}
		seen[id] = true
	}
	return nil
}

// Equippable lists the content of a slot kind a character may choose from, in
// stable order. Phase 2.2 intersects it with the unlock ledger; today every
// live row of the right class is available.
func Equippable(tables *tuning.Tables, class model.Class, kind string) []string {
	var ids []string
	switch kind {
	case KindWeapon:
		for id, weapon := range tables.Weapons {
			if weapon.Class == string(class) {
				ids = append(ids, id)
			}
		}
	case KindGadget:
		for id, gadget := range tables.Gadgets {
			if gadget.Class == string(class) {
				ids = append(ids, id)
			}
		}
	case KindSpell:
		if class != model.Mage {
			return nil
		}
		for id := range tables.Spells {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func live(tables *tuning.Tables, kind, id string) string {
	if id == "" {
		return ""
	}
	resolved, ok := tables.Resolve(kind, id)
	if !ok {
		return ""
	}
	// A refunded retirement has no live replacement, so the slot empties. The
	// refund itself is owed against the material ledger, not the slot.
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
		key(a.Spells) == key(b.Spells)
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
