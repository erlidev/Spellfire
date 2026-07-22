package game

import (
	"errors"
	"testing"

	"spellfire/server/internal/loadout"
	"spellfire/server/internal/model"
)

func TestJoinResolvesTheSavedLoadout(t *testing.T) {
	world, now := testWorld()
	character := model.Character{ID: "m1", Name: "Mage", Class: model.Mage}
	p := world.AddPlayer(character, now)
	if p.Loadout.Weapon == "" {
		t.Fatal("a character entered the world unarmed")
	}
	slots := world.bar(p)
	if len(slots) != world.tuning.Tables.Loadout.BarSlots() {
		t.Fatalf("bar has %d slots, want %d", len(slots), world.tuning.Tables.Loadout.BarSlots())
	}
	if _, ok := world.ability(p); !ok {
		t.Fatal("the first slot resolves to no ability, so a fresh character cannot fight")
	}
}

// The saved loadout must survive a disconnect exactly as the position does.
func TestLoadoutRoundTripsThroughPersistedState(t *testing.T) {
	world, now := testWorld()
	character := model.Character{ID: "m1", Name: "Mage", Class: model.Mage}
	p := world.AddPlayer(character, now)
	p.Position = Vec{}
	set := p.Loadout.Clone()
	set.Spells[1] = "fire-bolt"
	set.Spells[0] = ""
	if _, err := world.SetLoadout("m1", set, now); err != nil {
		t.Fatalf("commit inside the hub was refused: %v", err)
	}
	state, ok := world.StateOf("m1", now)
	if !ok {
		t.Fatal("no state for a present player")
	}
	if state.Loadout.Spells[1] != "fire-bolt" {
		t.Fatalf("saved loadout lost the slot: %+v", state.Loadout)
	}
	character.State = state
	world.RemovePlayer("m1")
	returned := world.AddPlayer(character, now)
	if returned.Loadout.Spells[1] != "fire-bolt" {
		t.Fatalf("rejoined with %+v, want the saved arrangement", returned.Loadout)
	}
}

// The keystone economy rule: the equipped set is committed to before leaving
// safety and cannot be rearranged in the field.
func TestLoadoutIsLockedOutsideTheSafeZone(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "m1", Name: "Mage", Class: model.Mage}, now)
	p.Position = Vec{world.tuning.SafeRadius + 1, 0}
	before := p.Loadout.Clone()
	set := p.Loadout.Clone()
	set.Spells[0], set.Spells[3] = "", "fire-bolt"
	returned, err := world.SetLoadout("m1", set, now)
	if !errors.Is(err, ErrLoadoutLocked) {
		t.Fatalf("a field respec was allowed: %v", err)
	}
	if p.Loadout.Spells[3] != "" {
		t.Fatal("a refused commit changed the equipped set")
	}
	if returned.Spells[3] != before.Spells[3] {
		t.Fatal("the refusal did not report the set the player still holds")
	}
	// Back inside safety the same request must take, at no cost.
	p.Position = Vec{}
	if _, err := world.SetLoadout("m1", set, now); err != nil {
		t.Fatalf("respec inside safety was refused: %v", err)
	}
	if p.Loadout.Spells[3] != "fire-bolt" {
		t.Fatal("the accepted commit did not equip the slot")
	}
}

func TestLoadoutCommitIsRefusedForABodyThatCannotAct(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "m1", Name: "Mage", Class: model.Mage}, now)
	p.Position, p.Alive = Vec{}, false
	if _, err := world.SetLoadout("m1", p.Loadout.Clone(), now); !errors.Is(err, ErrLoadoutUnavailable) {
		t.Fatalf("a dead body committed a loadout: %v", err)
	}
	p.Alive = true
	world.BeginLinger("m1", now)
	if _, err := world.SetLoadout("m1", p.Loadout.Clone(), now); !errors.Is(err, ErrLoadoutUnavailable) {
		t.Fatalf("a lingering body committed a loadout: %v", err)
	}
}

func TestSetLoadoutRejectsAnIllegalSetWithoutChangingAnything(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger}, now)
	p.Position = Vec{}
	equipped := p.Loadout.Weapon
	set := p.Loadout.Clone()
	set.Weapon = "starter-staff" // the other class's weapon
	if _, err := world.SetLoadout("g1", set, now); err == nil {
		t.Fatal("a Gunslinger equipped a staff")
	}
	if p.Loadout.Weapon != equipped {
		t.Fatalf("a refused commit changed the weapon to %q", p.Loadout.Weapon)
	}
}

// The use button acts through the selected slot, which travels with the input.
func TestSelectedSlotChoosesTheAbility(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "m1", Name: "Mage", Class: model.Mage}, now)
	p.Position = Vec{}
	set := p.Loadout.Clone()
	set.Spells[0], set.Spells[2] = "", "fire-bolt"
	if _, err := world.SetLoadout("m1", set, now); err != nil {
		t.Fatalf("commit refused: %v", err)
	}
	// An unfilled slot performs nothing; that is a slot the player has not
	// filled, not an error.
	p.Selected = 1
	if _, ok := world.ability(p); ok {
		t.Fatal("an empty slot resolved to an ability")
	}
	p.Selected = 2
	ability, ok := world.ability(p)
	if !ok || ability.ID != world.tuning.Tables.Spells["fire-bolt"].Ability {
		t.Fatalf("slot three resolved to %+v", ability)
	}
	// An index past the end of the bar falls back rather than doing nothing.
	p.Selected = 99
	if slot, ok := world.selectedSlot(p); !ok || slot.Index != 0 {
		t.Fatalf("an out-of-range selection resolved to %+v", slot)
	}
}

func TestInputCarriesTheSelectedSlot(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "m1", Name: "Mage", Class: model.Mage}, now)
	p.Input.Sequence, p.Input.SelectedSlot = 1, 4
	world.stepPlayer(p, now, 1/float64(world.tuning.TickRate))
	if p.Selected != 4 {
		t.Fatalf("selected slot is %d, want 4", p.Selected)
	}
	// A selection past the bar is clamped in, never rejected into a dead button.
	p.Input.Sequence, p.Input.SelectedSlot = 2, 250
	world.stepPlayer(p, now, 1/float64(world.tuning.TickRate))
	if p.Selected < 0 || p.Selected >= len(world.bar(p)) {
		t.Fatalf("selected slot %d is outside the bar", p.Selected)
	}
}

// A committed change is a fresh kit, reachable only inside safety.
func TestCommitReloadsAndClearsCooldowns(t *testing.T) {
	world, now := testWorld()
	p := world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger}, now)
	p.Position, p.Ammo = Vec{}, 1
	p.Cooldowns["rifle-shot"] = now.Add(world.tuning.DashCooldown)
	if _, err := world.SetLoadout("g1", p.Loadout.Clone(), now); err != nil {
		t.Fatalf("commit refused: %v", err)
	}
	weapon, _ := world.weapon(p)
	if p.Ammo != weapon.MagazineSize {
		t.Fatalf("ammo is %d, want a full magazine of %d", p.Ammo, weapon.MagazineSize)
	}
	if len(p.Cooldowns) != 0 {
		t.Fatalf("cooldowns survived the commit: %v", p.Cooldowns)
	}
}

func TestBarKindsMatchTheClass(t *testing.T) {
	world, now := testWorld()
	gun := world.AddPlayer(model.Character{ID: "g1", Name: "Gun", Class: model.Gunslinger}, now)
	if slots := world.bar(gun); slots[0].Kind != loadout.KindWeapon {
		t.Fatalf("gunslinger slot one is %q", slots[0].Kind)
	}
	mage := world.AddPlayer(model.Character{ID: "m1", Name: "Mage", Class: model.Mage}, now)
	if slots := world.bar(mage); slots[0].Kind != loadout.KindSpell {
		t.Fatalf("mage slot one is %q", slots[0].Kind)
	}
}
