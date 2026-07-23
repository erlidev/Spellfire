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
	// The shipped world is 45,000 units across and its PvP protection reaches
	// 9,000 of them. These tests are about combat, prediction, and lifecycle
	// rather than about geography, so they run in a compact arena — the same
	// deliberate override AOIRadius already gets here. The scale itself is
	// covered by the tuning suite and by the world-scale tests.
	tuning.scaleSafety(430, 1000)
	world := NewWorld(tuning)
	world.setWorldItems()
	return world, time.Unix(1_700_000_000, 0)
}

func testWorldItem(w *World, id, kind string, position Vec, object CollisionObject) *Entity {
	objects := []CollisionObject{object}
	entity := newEntity(id, kind, position, w.tuning.Tables.Entities[kind], EntityOverrides{CollisionObjects: &objects})
	return &entity
}

// worldItemByKind finds one generated item of a kind. Terrain is chunked, so
// the chunks around the hub are materialised first: an untouched world holds
// nothing but its authored fixtures.
func worldItemByKind(w *World, kind string) *Entity {
	w.loadChunksAround(Vec{})
	var found *Entity
	w.terrain.all(func(item *Entity) bool {
		if item.Kind == kind {
			found = item
		}
		return found == nil
	})
	return found
}

// equippedWeapon and equippedAbility resolve what a body is actually holding,
// so the tests assert against the tuning tables rather than against numbers
// copied out of them — and against the row this character drew rather than a
// category the basic set happens to sort first.
func equippedWeapon(w *World, p *Player) tuning.Weapon {
	weapon, ok := w.weapon(p)
	if !ok {
		panic("player " + p.ID + " carries no weapon")
	}
	return weapon
}

func equippedAbility(w *World, p *Player) tuning.Ability {
	ability, ok := w.ability(p)
	if !ok {
		panic("player " + p.ID + " has no ability on its selected slot")
	}
	return ability
}

// equippedDamage is what one hit of the equipped weapon takes off, read through
// the shared band and divided between the pellets a use puts into the world.
func equippedDamage(w *World, p *Player) float64 {
	return w.pelletDamage(equippedAbility(w, p))
}

func addTestPlayer(world *World, id string, class model.Class, position Vec, now time.Time) *Player {
	p := world.AddPlayer(model.Character{ID: id, Name: id, Class: class, Progress: model.Progress{Level: 1}}, now)
	// SetPlayerPosition rather than a bare assignment: a body that moves has to
	// be re-bucketed in the spatial index, or every query keeps finding it where
	// it entered.
	world.SetPlayerPosition(p.ID, position, now)
	return p
}

// carrying equips a specific weapon row on a test body, for the tests that are
// about one category's behaviour rather than about whatever a character drew. A
// category the economy withholds is given as the built instance it can only be
// carried as, which is the same shape a real craft produces.
func carrying(t *testing.T, w *World, p *Player, weaponID string) *Player {
	t.Helper()
	p.Unlocks, _ = p.Unlocks.With(weaponID)
	p.Loadout.Weapon = weaponID
	if w.tuning.Tables.Weapons[weaponID].RequiresCraft {
		item := model.CraftedItem{ID: "itm-" + p.ID + "-" + weaponID, CharacterID: p.ID, Weapon: weaponID, Components: map[string]string{}}
		p.Items = append(p.Items, item)
		p.Loadout.Weapon = item.ID
	}
	weapon, ok := w.weapon(p)
	if !ok {
		t.Fatalf("%s is not equippable as a stock row", weaponID)
	}
	p.Ammo = weapon.MagazineSize
	return p
}

// casting is carrying's counterpart for a Mage: it puts one named spell in the
// selected slot and grants the unlock behind it, for the tests that are about
// one spell rather than about whatever the starter draw handed the character.
// Affinity is satisfied trivially, because the rest of the bar is emptied.
func casting(t *testing.T, w *World, p *Player, spellID string) *Player {
	t.Helper()
	spell, ok := w.tuning.Tables.Spells[spellID]
	if !ok {
		t.Fatalf("%s is not a spell", spellID)
	}
	p.Unlocks, _ = p.Unlocks.With(spellID)
	for index := range p.Loadout.Spells {
		p.Loadout.Spells[index] = ""
	}
	// A tier above one needs same-element company, which a single-spell bar
	// cannot give it; those tests fill the rest of the bar themselves.
	p.Loadout.Spells[0], p.Selected = spellID, 0
	if spell.Tier > 1 {
		filled := 1
		for _, id := range sortedKeys(w.tuning.Tables.Spells) {
			other := w.tuning.Tables.Spells[id]
			if id == spellID || other.Element != spell.Element || filled >= len(p.Loadout.Spells) {
				continue
			}
			p.Unlocks, _ = p.Unlocks.With(id)
			p.Loadout.Spells[filled] = id
			filled++
		}
	}
	if _, ok := w.ability(p); !ok {
		t.Fatalf("%s did not resolve to an ability", spellID)
	}
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
	// Movement is scaled by the weight class of whatever the character drew, so
	// the expectation reads the same handling the simulation applied.
	expectedAxis := w.tuning.PlayerSpeed * w.handlingScale(p) / math.Sqrt2 / 60
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
	w.setWorldItems(testWorldItem(w, "tree", "tree", Vec{50, 0}, CollisionObject{Type: CollisionCircle, Radius: 20}))
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
	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now), "starter-rifle")
	target := addTestPlayer(w, "target", model.Mage, Vec{1400, 0}, now)
	w.ApplyInput(shooter.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now)
	for i := 1; i <= 30 && target.Health == w.tuning.MaxHealth; i++ {
		w.Step(now.Add(time.Duration(i) * time.Second / 60))
	}
	if target.Health != w.tuning.MaxHealth-equippedDamage(w, shooter) {
		t.Fatalf("target health = %f", target.Health)
	}
	if shooter.Ammo >= equippedWeapon(w, shooter).MagazineSize {
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
	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now.Add(-200*time.Millisecond)), "starter-rifle")
	target := addTestPlayer(w, "target", model.Mage, Vec{1300, 0}, now.Add(-200*time.Millisecond))
	w.SetPlayerPosition(shooter.ID, Vec{1200, 0}, shotAt)
	w.SetPlayerPosition(target.ID, Vec{1300, 0}, shotAt)
	w.SetPlayerPosition(target.ID, Vec{1300, 0}, now.Add(-40*time.Millisecond))
	w.SetPlayerPosition(target.ID, Vec{1300, 200}, now)
	w.ApplyInput(shooter.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(shotAt.UnixMilli())})
	w.Step(now)
	if target.Health != w.tuning.MaxHealth-equippedDamage(w, shooter) {
		t.Fatalf("rewound shot missed, health = %f", target.Health)
	}
}

func TestDeathAndRespawnResetAuthoritativeState(t *testing.T) {
	w, now := testWorld()
	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now), "starter-rifle")
	target := addTestPlayer(w, "target", model.Mage, Vec{1280, 0}, now)
	target.Health = equippedDamage(w, shooter)
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
	gunner := carrying(t, w, addTestPlayer(w, "gunner", model.Gunslinger, Vec{1500, 0}, now), "starter-rifle")
	rifle := equippedWeapon(w, gunner)
	cadence := equippedAbility(w, gunner).Interval() + time.Millisecond
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
	mage := addTestPlayer(w, "mage", model.Mage, Vec{1800, 0}, now)
	cost := equippedAbility(w, mage).Cost.Amount
	mage.Mana = cost
	w.ApplyInput(mage.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now.Add(5 * time.Second))
	if mage.Mana >= cost {
		t.Fatalf("spell did not consume mana: %f", mage.Mana)
	}
}

func TestSnapshotAppliesAreaOfInterestAndIncludesWorldItems(t *testing.T) {
	w, now := testWorld()
	addTestPlayer(w, "viewer", model.Gunslinger, Vec{}, now)
	addTestPlayer(w, "near", model.Mage, Vec{100, 0}, now)
	addTestPlayer(w, "far", model.Mage, Vec{800, 0}, now)
	w.setWorldItems(
		testWorldItem(w, "near-tree", "tree", Vec{200, 0}, CollisionObject{Type: CollisionCircle, Radius: 30}),
		testWorldItem(w, "far-tree", "tree", Vec{900, 0}, CollisionObject{Type: CollisionCircle, Radius: 30}),
	)
	snapshot := w.SnapshotFor("viewer", now, protocol.ServerSnapshot)
	if len(snapshot.Entities) != 3 || snapshot.Entities[2].ID != "near-tree" || snapshot.Entities[2].Type != protocol.EntityWorldItem {
		t.Fatalf("AOI entities = %#v", snapshot.Entities)
	}
	if len(snapshot.Colliders) != 1 || snapshot.Colliders[0].EntityID != "near-tree" {
		t.Fatalf("AOI colliders = %#v", snapshot.Colliders)
	}
}
