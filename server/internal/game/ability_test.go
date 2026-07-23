package game

import (
	"math"
	"testing"
	"testing/fstest"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

// worldFrom builds a world on an edited copy of the shipped tables, the way a
// content patch would ship them.
func worldFrom(t *testing.T, files fstest.MapFS) (*World, time.Time) {
	t.Helper()
	tables, err := tuning.Parse(files)
	if err != nil {
		t.Fatalf("edited tables rejected: %v", err)
	}
	balance := FromTables(tables)
	balance.AOIRadius = 500
	// The compact test arena testWorld() explains: these tests are about
	// mechanics, not geography.
	balance.SafeRadius, balance.PvPRadius = 430, 1000
	world := NewWorld(balance)
	world.setWorldItems()
	return world, time.Unix(1_700_000_000, 0)
}

// ownedProjectiles counts what one player has in flight, so a test that shares
// the world with another shooter still measures only its own subject.
func ownedProjectiles(w *World, ownerID string) int {
	count := 0
	for _, projectile := range w.projectiles {
		if projectile.OwnerID == ownerID {
			count++
		}
	}
	return count
}

// fire holds the use button down for one tick at the given time.
func fire(w *World, p *Player, sequence uint32, at time.Time) {
	w.ApplyInput(p.ID, protocol.Input{Sequence: sequence, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(at.UnixMilli())})
	w.Step(at)
}

// An ability's cooldown is a second gate on top of the cadence every ability
// shares, which is the resource axis the Mage kit is built on.
func TestAbilityCooldownGatesUseBeyondTheSharedCadence(t *testing.T) {
	files := edit(t, shipped(t), "abilities.json", func(document map[string]any) {
		document["rifle-shot"].(map[string]any)["cooldown_ms"] = 2000.0
	})
	w, now := worldFrom(t, files)
	p := carrying(t, w, addTestPlayer(w, "p", model.Gunslinger, Vec{1200, 0}, now), "starter-rifle")

	fire(w, p, 1, now)
	if len(w.projectiles) != 1 {
		t.Fatalf("first use produced %d projectiles", len(w.projectiles))
	}
	// Past the shared cadence, still inside the ability's own lockout.
	past := now.Add(equippedAbility(w, p).Interval() + 50*time.Millisecond)
	fire(w, p, 2, past)
	if len(w.projectiles) != 1 {
		t.Fatal("the ability fired again inside its cooldown")
	}
	if p.Ammo != equippedWeapon(w, p).MagazineSize-1 {
		t.Fatalf("a use blocked by cooldown still charged its cost: ammo = %d", p.Ammo)
	}
	fire(w, p, 3, now.Add(2100*time.Millisecond))
	if len(w.projectiles) != 2 {
		t.Fatal("the ability did not come off cooldown")
	}
}

// The cost is the ability's, not the class's: the same executor charges a
// magazine and a mana pool, and a use that cannot be paid delivers nothing.
func TestAbilityChargesTheCostItDeclares(t *testing.T) {
	w, now := testWorld()
	gunslinger := addTestPlayer(w, "g", model.Gunslinger, Vec{1200, 0}, now)
	mage := addTestPlayer(w, "m", model.Mage, Vec{1200, 400}, now)
	cost := equippedAbility(w, mage).Cost

	fire(w, gunslinger, 1, now)
	fire(w, mage, 1, now)
	if gunslinger.Ammo != equippedWeapon(w, gunslinger).MagazineSize-1 {
		t.Fatalf("ammo = %d", gunslinger.Ammo)
	}
	if mage.Mana > w.tuning.MaxMana-cost.Amount {
		t.Fatalf("mana = %g, want at most %g", mage.Mana, w.tuning.MaxMana-cost.Amount)
	}

	// Drained, and stepped just past the cadence gate so only the refusal can
	// explain the result. One step regenerates one tick's worth and no more.
	before := w.nextTelegraph
	mage.Mana = 0
	fire(w, mage, 2, now.Add(equippedAbility(w, mage).Interval()+10*time.Millisecond))
	if w.nextTelegraph != before {
		t.Fatal("the mage cast a spell it could not pay for")
	}
	if regenerated := w.tuning.ManaRegen / float64(w.tuning.TickRate); math.Abs(mage.Mana-regenerated) > 0.0001 {
		t.Fatalf("mana = %g, want only the tick's regeneration %g", mage.Mana, regenerated)
	}
}

// A magazine that cannot pay commits the weapon to its reload, which is the
// downtime the invariant pairs with the magazine.
func TestSpentMagazineCommitsToReloadInsteadOfFiring(t *testing.T) {
	w, now := testWorld()
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{1200, 0}, now)
	p.Ammo = 0

	fire(w, p, 1, now)
	if len(w.projectiles) != 0 {
		t.Fatal("an empty magazine fired")
	}
	if p.ReloadEnds.IsZero() {
		t.Fatal("an empty magazine did not start its reload")
	}
	// Release the button, or the refilled magazine is spent again in the same
	// step that completes the reload.
	weapon := equippedWeapon(w, p)
	w.ApplyInput(p.ID, protocol.Input{Sequence: 2, AimX: 1})
	w.Step(now.Add(weapon.ReloadDuration() + time.Millisecond))
	if p.Ammo != weapon.MagazineSize {
		t.Fatalf("magazine after reload = %d, want %d", p.Ammo, weapon.MagazineSize)
	}
}
