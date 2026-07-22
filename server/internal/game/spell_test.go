package game

import (
	"math"
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

// castFor holds the use button down and then runs the world far enough for a
// committed windup to deliver, which is what every telegraphed spell needs
// before anything it does exists. It answers with the time it left off at.
func castFor(w *World, p *Player, aim Vec, at time.Time, ticks int) time.Time {
	w.ApplyInput(p.ID, protocol.Input{
		Sequence: uint32(at.UnixMilli()), Buttons: ButtonFire, SelectedSlot: uint32(p.Selected),
		AimX: float32(aim.X), AimY: float32(aim.Y), ClientTimeMS: uint64(at.UnixMilli()),
	})
	tick := time.Second / time.Duration(w.tuning.TickRate)
	for step := 0; step < ticks; step++ {
		at = at.Add(tick)
		w.Step(at)
	}
	return at
}

// The whole Mage contract in one test: no spell may land damage the moment the
// button goes down. Every damaging row has to travel, or be warned about, or
// both — and none of them may take the sniper's instant exception, which is
// bought with a scope no staff has.
func TestNoSpellDeliversInstantDamage(t *testing.T) {
	tables := DefaultTuning().Tables
	for id, spell := range tables.Spells {
		ability, ok := tables.Abilities[spell.Ability]
		if !ok {
			t.Fatalf("%s references missing ability %q", id, spell.Ability)
		}
		if !ability.DealsDamage() {
			continue
		}
		if ability.DodgeVector == "" {
			t.Fatalf("%s deals damage with no declared dodge vector", id)
		}
		if ability.RequiresScope || (ability.Projectile != nil && ability.Projectile.HitscanRange > 0) {
			t.Fatalf("%s lands instantly; hitscan is the sniper's scoped exception and no staff has a scope", id)
		}
		travels := ability.Projectile != nil && ability.Projectile.Speed > 0
		warned := ability.Telegraph != nil && ability.WindupMS > 0
		if !travels && !warned {
			t.Fatalf("%s is point-and-click damage: nothing travels and nothing warns", id)
		}
		if ability.Projectile != nil && ability.Projectile.Homing != nil && ability.Projectile.Homing.TurnDegreesPerSecond > 360 {
			t.Fatalf("%s homes freely, which is a lock-on rather than a travelling round", id)
		}
	}
}

// A placed field is the tier-2 and tier-4 zoning shape: it damages and burns
// whoever stands in it, on its own cadence, and it stops when it expires.
func TestPlacedFieldPulsesOnWhoStandsInIt(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "cinder-patch")
	field := w.tuning.Tables.Abilities["cinder-patch-cast"].Deployable
	place := w.tuning.Tables.Abilities["cinder-patch-cast"].Placement
	target := addTestPlayer(w, "target", model.Gunslinger, Vec{1200 + place.Range, 0}, now)

	at := castFor(w, mage, Vec{1, 0}, now, w.tuning.TickRate*2)
	if len(w.deployables) != 1 {
		t.Fatalf("the cast left %d fields standing", len(w.deployables))
	}
	if placed := w.Deployables()[0]; math.Abs(placed.Position.X-(1200+place.Range)) > 1 {
		t.Fatalf("the field landed at %v, not on the ground the telegraph drew", placed.Position)
	}
	if target.Health >= target.MaxHealth {
		t.Fatal("a body standing in a burning patch took nothing")
	}
	if !w.hasEffectKind(target, "burn") {
		t.Fatal("the patch left no burn behind")
	}
	// Stepping out stops the pulse; the burn it already left runs on.
	burned := target.Health
	target.Position = Vec{1200, -900}
	target.Effects = nil
	at = at.Add(time.Duration(field.DurationMS) * time.Millisecond)
	w.Step(at)
	if target.Health != burned {
		t.Fatal("the field reached a body standing well outside it")
	}
}

// A trap waits, springs once on the first body that reaches it, and is spent.
func TestTrapSpringsOnceOnTheFirstBodyIn(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "ice-trap")
	place := w.tuning.Tables.Abilities["ice-trap-cast"].Placement
	at := castFor(w, mage, Vec{1, 0}, now, w.tuning.TickRate)
	if len(w.deployables) != 1 {
		t.Fatalf("the cast left %d traps standing", len(w.deployables))
	}
	// Nothing has touched it, so it is still armed well past a pulse cadence.
	if w.Deployables()[0].Spent {
		t.Fatal("an untouched trap sprang on its own")
	}
	target := addTestPlayer(w, "target", model.Gunslinger, Vec{1200 + place.Range, 0}, now)
	at = castFor(w, mage, Vec{1, 0}, at, w.tuning.TickRate/2)
	if !w.rooted(target) {
		t.Fatal("the body that sprang the trap was not rooted")
	}
	if fields := w.Deployables(); len(fields) != 1 || !fields[0].Spent {
		t.Fatalf("the trap was not spent by the body that sprang it: %+v", fields)
	}
	// A spent trap does nothing to the next body through it.
	target.Effects = nil
	at = at.Add(time.Second)
	w.Step(at)
	if w.rooted(target) {
		t.Fatal("a spent trap caught a second body")
	}
}

// Blizzard's stacking slow ends in a stun: the closing pulse is what a zone
// leaves behind when its window shuts.
func TestFieldClosesWithItsFinalPulse(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "blizzard")
	place := w.tuning.Tables.Abilities["blizzard-cast"].Placement
	field := w.tuning.Tables.Abilities["blizzard-cast"].Deployable
	target := addTestPlayer(w, "target", model.Gunslinger, Vec{1200 + place.Range, 0}, now)

	at := castFor(w, mage, Vec{1, 0}, now, w.tuning.TickRate*2)
	if !w.hasEffectKind(target, "slow") {
		t.Fatal("a body inside the zone was not slowed")
	}
	if w.stunned(target) {
		t.Fatal("the closing stun landed while the zone was still running")
	}
	at = at.Add(time.Duration(field.DurationMS) * time.Millisecond)
	w.Step(at)
	if !w.stunned(target) {
		t.Fatal("the zone closed without its final pulse")
	}
}

// A blink is a walk, not a jump: it stops at cover rather than crossing it.
func TestBlinkCarriesTheCasterAndStopsAtCover(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "thunderstep")
	distance := w.tuning.Tables.Abilities["thunderstep-cast"].Blink.Distance

	at := castFor(w, mage, Vec{1, 0}, now, w.tuning.TickRate/2)
	moved := mage.Position.X - 1200
	if math.Abs(moved-distance) > 1 {
		t.Fatalf("the blink covered %g, want the authored %g", moved, distance)
	}

	// A wall in the way is where the blink ends, because a blink that crossed
	// cover would make every piece of cover in the world optional.
	w.worldItems = append(w.worldItems, testWorldItem(w, "block", "wall", Vec{mage.Position.X + 120, 0}, CollisionObject{Type: CollisionCircle, Radius: 40}))
	mage.Cooldowns = map[string]time.Time{}
	before := mage.Position
	castFor(w, mage, Vec{1, 0}, at.Add(time.Second), w.tuning.TickRate/2)
	if mage.Position.X > before.X+120 {
		t.Fatalf("the blink crossed cover: %v to %v", before, mage.Position)
	}
	if mage.Position.X <= before.X {
		t.Fatal("the blink did not move the caster toward the cover at all")
	}
}

// Chain lightning arcs from the body it landed on, never back onto it and never
// onto its caster.
func TestChainLightningArcsToNearbyBodies(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "chain-lightning")
	first := addTestPlayer(w, "first", model.Gunslinger, Vec{1500, 0}, now)
	second := addTestPlayer(w, "second", model.Gunslinger, Vec{1650, 0}, now)
	far := addTestPlayer(w, "far", model.Gunslinger, Vec{1500, 900}, now)

	castFor(w, mage, Vec{1, 0}, now, w.tuning.TickRate*2)
	if first.Health >= first.MaxHealth {
		t.Fatal("the bolt never landed")
	}
	if second.Health >= second.MaxHealth {
		t.Fatal("the hit did not arc to the body beside the one it struck")
	}
	if far.Health < far.MaxHealth {
		t.Fatal("the arc reached a body outside its range")
	}
	if mage.Health < mage.MaxHealth {
		t.Fatal("the arc came back onto its caster")
	}
}

// A homing round turns toward what it follows, and no faster than its rate.
func TestHomingTurnsNoFasterThanItsRate(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "arcane-missile")
	addTestPlayer(w, "target", model.Gunslinger, Vec{1400, 300}, now)
	homing := w.tuning.Tables.Abilities["arcane-missile-cast"].Projectile.Homing

	at := castFor(w, mage, Vec{1, 0}, now, w.tuning.TickRate/2)
	var round *Projectile
	for _, projectile := range w.projectiles {
		round = projectile
	}
	if round == nil {
		t.Fatal("the cast delivered no round")
	}
	before := math.Atan2(round.Velocity.Y, round.Velocity.X) * 180 / math.Pi
	tick := time.Second / time.Duration(w.tuning.TickRate)
	w.Step(at.Add(tick))
	after := math.Atan2(round.Velocity.Y, round.Velocity.X) * 180 / math.Pi
	turned := after - before
	if turned <= 0 {
		t.Fatalf("the round did not turn toward its target: %g to %g", before, after)
	}
	if limit := homing.TurnDegreesPerSecond / float64(w.tuning.TickRate); turned > limit+0.001 {
		t.Fatalf("the round turned %g degrees in one tick, past its %g limit", turned, limit)
	}
}

// The dispel is indiscriminate and pays for what it takes.
func TestNullifyStripsBothSidesAndReturnsMana(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "nullify")
	target := addTestPlayer(w, "target", model.Gunslinger, Vec{1260, 0}, now)
	distant := addTestPlayer(w, "distant", model.Gunslinger, Vec{1200, 900}, now)
	rule := w.tuning.Tables.Abilities["nullify-cast"].Cleanse

	w.applyEffects(mage, []string{"rime-armor"}, mage.ID, Vec{1, 0}, now)
	w.applyEffects(target, []string{"arcane-ward", "bulwark-armor"}, target.ID, Vec{1, 0}, now)
	w.applyEffects(distant, []string{"arcane-ward"}, distant.ID, Vec{1, 0}, now)
	mage.Mana = 40

	castFor(w, mage, Vec{1, 0}, now, 2)
	if len(target.Effects) != 0 {
		t.Fatalf("a body inside the burst kept %d effects", len(target.Effects))
	}
	if len(mage.Effects) != 0 {
		t.Fatal("the caster's own effects survived its dispel")
	}
	if len(distant.Effects) != 1 {
		t.Fatal("the burst reached a body outside its radius")
	}
	// Three effects were stripped: the caster's one and the target's two.
	// Regeneration runs over the ticks the cast took, so the floor is what the
	// dispel itself returned.
	want := 40 - w.tuning.Tables.Abilities["nullify-cast"].Cost.Amount + 3*rule.ManaPerEffect
	if mage.Mana < want || mage.Mana > want+w.tuning.ManaRegen {
		t.Fatalf("mana after the dispel is %g, want %g", mage.Mana, want)
	}
}

// Armor is mitigation without a pool: it scales what arrives and never absorbs.
func TestArmorMitigatesWithoutAbsorbing(t *testing.T) {
	w, now := testWorld()
	plain := addTestPlayer(w, "plain", model.Gunslinger, Vec{1200, 0}, now)
	warded := addTestPlayer(w, "warded", model.Gunslinger, Vec{1200, 200}, now)
	w.applyEffects(warded, []string{"bulwark-armor"}, "caster", Vec{1, 0}, now)

	w.damage(plain, 40, "attacker", now)
	w.damage(warded, 40, "attacker", now)
	scale := w.tuning.Tables.Effects["bulwark-armor"].DamageMultiplier
	if want := warded.MaxHealth - 40*scale; math.Abs(warded.Health-want) > 0.001 {
		t.Fatalf("armored body is at %g, want %g", warded.Health, want)
	}
	if plain.Health != plain.MaxHealth-40 {
		t.Fatalf("unarmored body is at %g", plain.Health)
	}
	// Mitigation runs for its whole window rather than being spent by one hit.
	w.damage(warded, 40, "attacker", now)
	if want := warded.MaxHealth - 80*scale; math.Abs(warded.Health-want) > 0.001 {
		t.Fatalf("armor was spent by the first hit: %g, want %g", warded.Health, want)
	}
}

// The wall is real terrain: it blocks movement and rounds, one per caster, and
// it comes down on its own.
func TestStoneWallStandsBlocksAndExpires(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "stone-wall")
	wall := w.tuning.Tables.Abilities["stone-wall-cast"].Wall

	at := castFor(w, mage, Vec{1, 0}, now, w.tuning.TickRate/2)
	segments := w.Walls(mage.ID)
	if len(segments) != wall.Segments {
		t.Fatalf("the cast raised %d segments, want %d", len(segments), wall.Segments)
	}
	if span := wallSpan(*wall); span <= 0 {
		t.Fatalf("the wall spans %g", span)
	}
	// The wall is in the world's collision set, so nothing may walk through it.
	if !w.collides(segments[0].Position, 1) {
		t.Fatal("a raised wall segment does not collide")
	}
	// And it stops a round the same way a tree does.
	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now), "starter-rifle")
	shooter.Position = Vec{segments[0].Position.X - 120, segments[0].Position.Y}
	behind := addTestPlayer(w, "behind", model.Gunslinger, Vec{segments[0].Position.X + 120, segments[0].Position.Y}, now)
	at = castFor(w, shooter, Vec{1, 0}, at, w.tuning.TickRate/2)
	if behind.Health < behind.MaxHealth {
		t.Fatal("a round crossed the wall")
	}

	// One wall per caster: raising a second drops the first.
	mage.Cooldowns = map[string]time.Time{}
	first := segments[0].ID
	at = castFor(w, mage, Vec{0, 1}, at.Add(time.Second), w.tuning.TickRate/2)
	for _, segment := range w.Walls(mage.ID) {
		if segment.ID == first {
			t.Fatal("the second wall kept a segment of the first")
		}
	}
	// And a wall expires on its own.
	at = at.Add(wall.Duration())
	w.Step(at)
	if len(w.Walls(mage.ID)) != 0 {
		t.Fatal("the wall outlived its duration")
	}
}

// Safety is never an offensive tool, and a wall may not box a body in. Both
// refusals happen before the cast is charged.
func TestWallPlacementRefusalsCostNothing(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{0, 0}, now), "stone-wall")
	mage.Mana = w.tuning.MaxMana

	castFor(w, mage, Vec{1, 0}, now, 2)
	if len(w.Walls(mage.ID)) != 0 {
		t.Fatal("a wall was raised inside the safe zone")
	}
	if mage.Mana != w.tuning.MaxMana {
		t.Fatal("a refused wall still charged its cost")
	}

	// Out in the field, but with a body standing exactly where it would go.
	mage.Position = Vec{1200, 0}
	place := w.tuning.Tables.Abilities["stone-wall-cast"].Placement
	addTestPlayer(w, "blocker", model.Gunslinger, Vec{1200 + place.Range, 0}, now)
	castFor(w, mage, Vec{1, 0}, now.Add(time.Second), 2)
	if len(w.Walls(mage.ID)) != 0 {
		t.Fatal("a wall was raised on top of an actor")
	}
	if mage.Mana != w.tuning.MaxMana {
		t.Fatal("a refused wall still charged its cost")
	}
}

// A wall's lifetime is part of the rewind history: a shot rewound to a moment
// when the wall stood is stopped by it, even though it has since come down.
func TestRewindHonoursWallLifetime(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "stone-wall")
	wall := w.tuning.Tables.Abilities["stone-wall-cast"].Wall
	at := castFor(w, mage, Vec{1, 0}, now, w.tuning.TickRate/2)
	segments := w.Walls(mage.ID)
	if len(segments) == 0 {
		t.Fatal("no wall to rewind past")
	}
	middle := segments[len(segments)/2].Position

	// The wall runs out. It is gone from the live world from this moment on.
	at = at.Add(wall.Duration())
	w.Step(at)
	if len(w.Walls(mage.ID)) != 0 {
		t.Fatal("the wall outlived its duration")
	}

	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{middle.X - 60, middle.Y}, now), "starter-rifle")
	target := addTestPlayer(w, "target", model.Gunslinger, Vec{middle.X + 150, middle.Y}, now)
	tick := time.Second / time.Duration(w.tuning.TickRate)

	// Claimed inside the rewind window, at a moment the wall still stood.
	claimed := at.Add(-50 * time.Millisecond)
	w.ApplyInput(shooter.ID, protocol.Input{
		Sequence: 900, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(claimed.UnixMilli()),
	})
	at = at.Add(tick)
	w.Step(at)
	if target.Health < target.MaxHealth {
		t.Fatal("a shot rewound to while the wall stood passed through it")
	}

	// The same shot claimed now finds the ground it actually left behind.
	weapon := equippedWeapon(w, shooter)
	at = at.Add(weapon.ReloadDuration() + time.Second)
	w.ApplyInput(shooter.ID, protocol.Input{
		Sequence: 901, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(at.UnixMilli()),
	})
	for step := 0; step < w.tuning.TickRate && target.Health >= target.MaxHealth; step++ {
		at = at.Add(tick)
		w.Step(at)
	}
	if target.Health >= target.MaxHealth {
		t.Fatal("a shot fired after the wall came down was still blocked by it")
	}
}

// Skyfall's blink is conditioned on landing the area, not on casting it.
func TestBlinkOnHitOnlyPaysOutOnLanding(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "skyfall")
	place := w.tuning.Tables.Abilities["skyfall-cast"].Placement

	// Nobody is standing on the marked ground, so the caster stays put.
	at := castFor(w, mage, Vec{1, 0}, now, w.tuning.TickRate*2)
	if mage.Position.X != 1200 {
		t.Fatalf("a missed area moved the caster to %v", mage.Position)
	}

	landing := Vec{1200 + place.Range, 0}
	target := addTestPlayer(w, "target", model.Gunslinger, landing, now)
	mage.Cooldowns, mage.Mana = map[string]time.Time{}, w.tuning.MaxMana
	castFor(w, mage, Vec{1, 0}, at.Add(time.Second), w.tuning.TickRate*2)
	if target.Health >= target.MaxHealth {
		t.Fatal("the area never landed")
	}
	if mage.Position.Sub(landing).LengthSq() > 4 {
		t.Fatalf("landing the area did not carry the caster to it: %v", mage.Position)
	}
}

// Per-spell cooldowns are the second resource axis: mana alone does not gate a
// defining spell, and the shared cadence is not what holds it back either.
func TestSpellCooldownsGateBeyondManaAndCadence(t *testing.T) {
	w, now := testWorld()
	mage := casting(t, w, addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now), "ward")
	ability := w.tuning.Tables.Abilities["ward-cast"]

	castFor(w, mage, Vec{1, 0}, now, 2)
	if !w.hasEffectKind(mage, "shield") {
		t.Fatal("the ward left no shield")
	}
	mage.Effects, mage.Mana = nil, w.tuning.MaxMana
	// Well past the shared cadence, but well inside the spell's own lockout.
	at := castFor(w, mage, Vec{1, 0}, now.Add(ability.Interval()*3), 2)
	if w.hasEffectKind(mage, "shield") {
		t.Fatal("a spell on cooldown was cast again")
	}
	if mage.Mana != w.tuning.MaxMana {
		t.Fatal("a refused cast still spent mana")
	}
	castFor(w, mage, Vec{1, 0}, at.Add(ability.Cooldown()), 2)
	if !w.hasEffectKind(mage, "shield") {
		t.Fatal("the spell never came off cooldown")
	}
}
