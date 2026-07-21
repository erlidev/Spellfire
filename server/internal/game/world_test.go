package game

import (
	"math"
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

func testWorld() (*World, time.Time) {
	tuning := DefaultTuning()
	tuning.AOIRadius = 500
	world := NewWorld(tuning)
	world.colliders = nil
	return world, time.Unix(1_700_000_000, 0)
}

// starterWeapon and starterAbility resolve what a fresh character of the class
// actually carries, so the tests assert against the tuning tables rather than
// against numbers copied out of them.
func starterWeapon(w *World, class model.Class) tuning.Weapon {
	weapon, ok := w.tuning.Tables.StarterWeapon(string(class))
	if !ok {
		panic("no starter weapon for " + class)
	}
	return weapon
}

func starterAbility(w *World, class model.Class) tuning.Ability {
	ability, ok := w.tuning.Tables.WeaponAbility(starterWeapon(w, class))
	if !ok {
		panic("unresolvable starter ability for " + class)
	}
	return ability
}

// starterDamage is what one starter hit takes off, read through the shared band.
func starterDamage(w *World, class model.Class) float64 {
	return w.tuning.Tables.BandDamage(starterAbility(w, class).DamageBand)
}

func addTestPlayer(world *World, id string, class model.Class, position Vec, now time.Time) *Player {
	p := world.AddPlayer(model.Character{ID: id, Name: id, Class: class, Level: 1}, now)
	p.Position = position
	world.recordHistory(p, now)
	return p
}

func TestMovementNormalizesDiagonalsAndAcknowledgesInput(t *testing.T) {
	w, now := testWorld()
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{}, now)
	input := protocol.Input{Sequence: 1, Buttons: ButtonRight | ButtonDown, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())}
	if !w.ApplyInput(p.ID, input) {
		t.Fatal("fresh input rejected")
	}
	w.Step(now.Add(time.Second / 60))
	got, _ := w.PlayerState(p.ID)
	expectedAxis := w.tuning.PlayerSpeed / math.Sqrt2 / 60
	if math.Abs(got.Position.X-expectedAxis) > .001 || math.Abs(got.Position.Y-expectedAxis) > .001 {
		t.Fatalf("position = %#v, want axis %f", got.Position, expectedAxis)
	}
	if got.Acknowledged != input.Sequence {
		t.Fatalf("ack = %d", got.Acknowledged)
	}
	if w.ApplyInput(p.ID, input) {
		t.Fatal("duplicate input accepted")
	}
}

func TestDashIsEdgeTriggeredAndRespectsCooldown(t *testing.T) {
	w, now := testWorld()
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{}, now)
	tick := time.Second / 60
	ticks := w.tuning.dashTicks()
	perTick := w.tuning.DashDistance / float64(ticks)
	sequence := uint32(0)
	press := func(buttons uint32, at time.Time) {
		sequence++
		w.ApplyInput(p.ID, protocol.Input{Sequence: sequence, Buttons: buttons, AimX: 1, ClientTimeMS: uint64(at.UnixMilli())})
		w.Step(at)
	}
	press(ButtonRight|ButtonDash, now)
	if math.Abs(p.Position.X-perTick) > .001 {
		t.Fatalf("first dash tick moved %f, want %f", p.Position.X, perTick)
	}
	for i := 1; i < ticks; i++ {
		press(ButtonRight|ButtonDash, now.Add(time.Duration(i)*tick))
	}
	first := p.Position.X
	if math.Abs(first-w.tuning.DashDistance) > .001 {
		t.Fatalf("dash distance = %f, want %f", first, w.tuning.DashDistance)
	}
	press(ButtonRight|ButtonDash, now.Add(time.Duration(ticks)*tick))
	if p.Position.X-first > w.tuning.PlayerSpeed/60+.01 {
		t.Fatalf("held dash repeated: %f -> %f", first, p.Position.X)
	}
	press(ButtonRight, now.Add(time.Duration(ticks+1)*tick))
	press(ButtonRight|ButtonDash, now.Add(time.Second))
	before := p.Position.X
	if before-first > 30 {
		t.Fatalf("dash ignored cooldown but movement delta is excessive: %f", before-first)
	}
	press(ButtonRight, now.Add(2200*time.Millisecond))
	recovered := p.Position.X
	for i := 0; i < ticks; i++ {
		press(ButtonRight|ButtonDash, now.Add(2300*time.Millisecond+time.Duration(i)*tick))
	}
	if math.Abs(p.Position.X-recovered-w.tuning.DashDistance) > .001 {
		t.Fatalf("dash did not recover after cooldown: %f", p.Position.X-recovered)
	}
}

func TestDashCarriesPlayerAgainstMovementInput(t *testing.T) {
	w, now := testWorld()
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{}, now)
	tick := time.Second / 60
	ticks := w.tuning.dashTicks()
	w.ApplyInput(p.ID, protocol.Input{Sequence: 1, Buttons: ButtonRight | ButtonDash, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now)
	for i := 1; i < ticks; i++ {
		w.ApplyInput(p.ID, protocol.Input{Sequence: uint32(i + 1), Buttons: ButtonLeft, AimX: 1})
		w.Step(now.Add(time.Duration(i) * tick))
	}
	if math.Abs(p.Position.X-w.tuning.DashDistance) > .001 || math.Abs(p.Position.Y) > .001 {
		t.Fatalf("dash was steerable: %#v", p.Position)
	}
}

func TestPlayerCannotMoveThroughTreeOrWorldBoundary(t *testing.T) {
	w, now := testWorld()
	w.colliders = []Collider{{ID: "tree", Kind: "tree", Position: Vec{50, 0}, Radius: 20}}
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{}, now)
	for i := 1; i <= 30; i++ {
		w.ApplyInput(p.ID, protocol.Input{Sequence: uint32(i), Buttons: ButtonRight, AimX: 1})
		w.Step(now.Add(time.Duration(i) * time.Second / 60))
	}
	if p.Position.X >= 10.1 {
		t.Fatalf("player penetrated tree: x=%f", p.Position.X)
	}
	p.Position = Vec{w.tuning.WorldRadius - w.tuning.PlayerRadius - 1, 0}
	w.ApplyInput(p.ID, protocol.Input{Sequence: 31, Buttons: ButtonRight, AimX: 1})
	w.Step(now.Add(time.Second))
	if p.Position.X > w.tuning.WorldRadius-w.tuning.PlayerRadius+.001 {
		t.Fatalf("player left world: x=%f", p.Position.X)
	}
}

func TestProjectileCombatOutsidePvPProtection(t *testing.T) {
	w, now := testWorld()
	shooter := addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now)
	target := addTestPlayer(w, "target", model.Mage, Vec{1400, 0}, now)
	w.ApplyInput(shooter.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now)
	for i := 1; i <= 30 && target.Health == w.tuning.MaxHealth; i++ {
		w.Step(now.Add(time.Duration(i) * time.Second / 60))
	}
	if target.Health != w.tuning.MaxHealth-starterDamage(w, model.Gunslinger) {
		t.Fatalf("target health = %f", target.Health)
	}
	if shooter.Ammo >= starterWeapon(w, model.Gunslinger).MagazineSize {
		t.Fatalf("firing did not consume ammo: %d", shooter.Ammo)
	}
}

func TestPvPProtectedFringePreventsDamage(t *testing.T) {
	w, now := testWorld()
	shooter := addTestPlayer(w, "shooter", model.Gunslinger, Vec{500, 0}, now)
	target := addTestPlayer(w, "target", model.Mage, Vec{700, 0}, now)
	w.ApplyInput(shooter.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now)
	for i := 1; i <= 30; i++ {
		w.Step(now.Add(time.Duration(i) * time.Second / 60))
	}
	if target.Health != w.tuning.MaxHealth {
		t.Fatalf("safe target took damage: %f", target.Health)
	}
}

func TestServerRewindHitsHistoricalPosition(t *testing.T) {
	w, now := testWorld()
	shotAt := now.Add(-150 * time.Millisecond)
	shooter := addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now.Add(-200*time.Millisecond))
	target := addTestPlayer(w, "target", model.Mage, Vec{1300, 0}, now.Add(-200*time.Millisecond))
	w.SetPlayerPosition(shooter.ID, Vec{1200, 0}, shotAt)
	w.SetPlayerPosition(target.ID, Vec{1300, 0}, shotAt)
	w.SetPlayerPosition(target.ID, Vec{1300, 0}, now.Add(-40*time.Millisecond))
	w.SetPlayerPosition(target.ID, Vec{1300, 200}, now)
	w.ApplyInput(shooter.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(shotAt.UnixMilli())})
	w.Step(now)
	if target.Health != w.tuning.MaxHealth-starterDamage(w, model.Gunslinger) {
		t.Fatalf("rewound shot missed, health = %f", target.Health)
	}
}

func TestDeathAndRespawnResetAuthoritativeState(t *testing.T) {
	w, now := testWorld()
	shooter := addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now)
	target := addTestPlayer(w, "target", model.Mage, Vec{1280, 0}, now)
	target.Health = starterDamage(w, model.Gunslinger)
	w.ApplyInput(shooter.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now)
	for i := 1; i <= 10 && target.Alive; i++ {
		w.Step(now.Add(time.Duration(i) * time.Second / 60))
	}
	if target.Alive || target.Health != 0 {
		t.Fatalf("target survived: %#v", target)
	}
	if !w.Respawn(target.ID, now.Add(time.Second)) {
		t.Fatal("respawn rejected")
	}
	if !target.Alive || target.Health != w.tuning.MaxHealth || target.Position != (Vec{}) {
		t.Fatalf("respawn state = %#v", target)
	}
	if w.Respawn(target.ID, now.Add(2*time.Second)) {
		t.Fatal("living player respawned")
	}
}

func TestResourcesEnforceReloadAndManaCosts(t *testing.T) {
	w, now := testWorld()
	rifle := starterWeapon(w, model.Gunslinger)
	cadence := starterAbility(w, model.Gunslinger).Interval() + time.Millisecond
	gunner := addTestPlayer(w, "gunner", model.Gunslinger, Vec{1500, 0}, now)
	for i := 0; i < rifle.MagazineSize; i++ {
		at := now.Add(time.Duration(i) * cadence)
		w.ApplyInput(gunner.ID, protocol.Input{Sequence: uint32(i + 1), Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(at.UnixMilli())})
		w.Step(at)
	}
	if gunner.Ammo != 0 {
		t.Fatalf("ammo after magazine = %d", gunner.Ammo)
	}
	at := now.Add(time.Duration(rifle.MagazineSize+1) * cadence)
	w.ApplyInput(gunner.ID, protocol.Input{Sequence: uint32(rifle.MagazineSize + 1), Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(at.UnixMilli())})
	w.Step(at)
	if gunner.ReloadEnds.IsZero() {
		t.Fatal("empty gun did not begin reload")
	}
	w.ApplyInput(gunner.ID, protocol.Input{Sequence: uint32(rifle.MagazineSize + 2), AimX: 1})
	w.Step(at.Add(rifle.ReloadDuration()))
	if gunner.Ammo != rifle.MagazineSize {
		t.Fatalf("reloaded ammo = %d", gunner.Ammo)
	}
	cost := starterAbility(w, model.Mage).Cost.Amount
	mage := addTestPlayer(w, "mage", model.Mage, Vec{1800, 0}, now)
	mage.Mana = cost
	w.ApplyInput(mage.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now.Add(5 * time.Second))
	if mage.Mana >= cost {
		t.Fatalf("spell did not consume mana: %f", mage.Mana)
	}
}

func TestSnapshotAppliesAreaOfInterestAndIncludesColliders(t *testing.T) {
	w, now := testWorld()
	addTestPlayer(w, "viewer", model.Gunslinger, Vec{}, now)
	addTestPlayer(w, "near", model.Mage, Vec{100, 0}, now)
	addTestPlayer(w, "far", model.Mage, Vec{800, 0}, now)
	w.colliders = []Collider{{ID: "near-tree", Kind: "tree", Position: Vec{200, 0}, Radius: 30}, {ID: "far-tree", Kind: "tree", Position: Vec{900, 0}, Radius: 30}}
	snapshot := w.SnapshotFor("viewer", now, protocol.ServerSnapshot)
	if len(snapshot.Entities) != 2 {
		t.Fatalf("AOI entities = %#v", snapshot.Entities)
	}
	if len(snapshot.Colliders) != 1 || snapshot.Colliders[0].ID != "near-tree" {
		t.Fatalf("AOI colliders = %#v", snapshot.Colliders)
	}
}
