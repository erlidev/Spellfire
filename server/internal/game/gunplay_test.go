package game

import (
	"math"
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

// degreesOff is how far a launched round left the aim vector, signed the same
// way the recoil pattern is: positive turns one way, negative the other.
func degreesOff(velocity Vec, aim Vec) float64 {
	return math.Atan2(aim.X*velocity.Y-aim.Y*velocity.X, aim.X*velocity.X+aim.Y*velocity.Y) * 180 / math.Pi
}

// launched returns the directions of everything a player has in flight, oldest
// identifier first, so a cone can be measured as well as a single round.
func launched(w *World, ownerID string) []Vec {
	directions := make([]Vec, 0, len(w.projectiles))
	for _, id := range sortedProjectileIDs(w.projectiles) {
		if projectile := w.projectiles[id]; projectile.OwnerID == ownerID {
			directions = append(directions, projectile.Velocity.Normalized())
		}
	}
	return directions
}

// Recoil is a fixed pattern, not a random cone: each shot steps the muzzle by
// the entry the weapon declares, from wherever the last shot left it, so a
// burst walks a shape a player can learn and compensate for.
func TestRecoilWalksTheWeaponsFixedPattern(t *testing.T) {
	w, now := testWorld()
	p := carrying(t, w, addTestPlayer(w, "p", model.Gunslinger, Vec{1500, 0}, now), "starter-rifle")
	weapon := equippedWeapon(w, p)
	cadence := equippedAbility(w, p).Interval() + time.Millisecond
	weight := w.tuning.Tables.WeightOf(weapon)
	aim := Vec{1, 0}

	// The expected muzzle position is the model stated plainly: what the last
	// shot left, settled by the quiet since, plus this shot's step.
	offset := 0.0
	for shot := range weapon.Recoil.Pattern {
		at := now.Add(time.Duration(shot) * cadence)
		if shot > 0 {
			offset = settledRecoil(offset, cadence, weapon.Recoil.Recovery())
		}
		offset += weapon.Recoil.DegreesAt(shot) * weight.RecoilMultiplier
		w.projectiles = map[string]*Projectile{}
		fire(w, p, uint32(shot+1), at)
		directions := launched(w, p.ID)
		if len(directions) != 1 {
			t.Fatalf("shot %d produced %d rounds", shot, len(directions))
		}
		// Standing still, the only thing off aim is the pattern: the shipped
		// standing spread is a fraction of a degree, so the tolerance is that.
		got := degreesOff(directions[0], aim)
		if math.Abs(got-offset) > weapon.Spread.StandingDegrees {
			t.Fatalf("shot %d left %.3f degrees off aim, want %.3f", shot, got, offset)
		}
	}
}

// A settled first shot is always true, and the walked muzzle is what the
// snapshot carries: recoil is only a skill axis if it can be seen.
func TestRecoilIsVisibleAndSettlesBackToAim(t *testing.T) {
	w, now := testWorld()
	p := carrying(t, w, addTestPlayer(w, "p", model.Gunslinger, Vec{1500, 0}, now), "starter-rifle")
	weapon := equippedWeapon(w, p)
	cadence := equippedAbility(w, p).Interval() + time.Millisecond

	fire(w, p, 1, now)
	if got := w.recoilDegrees(p, now); got != 0 {
		t.Fatalf("a settled first shot walked the muzzle %.3f degrees", got)
	}
	fire(w, p, 2, now.Add(cadence))
	walked := w.recoilDegrees(p, now.Add(cadence))
	if math.Abs(walked) < 1 {
		t.Fatalf("the second shot moved the muzzle %.3f degrees, which nobody can see", walked)
	}
	half := now.Add(cadence).Add(weapon.Recoil.Recovery() / 2)
	if settling := w.recoilDegrees(p, half); math.Abs(settling) >= math.Abs(walked) {
		t.Fatalf("the muzzle sat at %.3f degrees halfway through recovery, no closer to aim than %.3f", settling, walked)
	}
	quiet := now.Add(cadence).Add(weapon.Recoil.Recovery() + time.Millisecond)
	if settled := w.recoilDegrees(p, quiet); settled != 0 {
		t.Fatalf("the muzzle never settled: %.3f degrees off aim after the recovery window", settled)
	}
	snapshot := w.SnapshotFor(p.ID, now.Add(cadence), protocol.ServerSnapshot)
	for _, entity := range snapshot.Entities {
		if entity.ID == p.ID && entity.RecoilDegrees == 0 {
			t.Fatal("the snapshot reported a walked muzzle as sitting on aim")
		}
	}
}

// Firing while moving costs accuracy, and firing while standing does not. This
// is the accuracy-against-mobility trade the whole class is built on, so it is
// asserted as a spread that widens rather than as a magnitude.
func TestMoveSpreadWidensWithSpeed(t *testing.T) {
	w, now := testWorld()
	weapon := w.tuning.Tables.Weapons["starter-rifle"]
	cadence := w.tuning.Tables.Abilities[weapon.Ability].Interval() + time.Millisecond
	widest := func(buttons uint32) float64 {
		// A fresh body per sample, so the two runs draw the same spread seeds
		// and differ only by how fast they are travelling.
		p := carrying(t, w, addTestPlayer(w, "runner", model.Gunslinger, Vec{1500, 0}, now), "starter-rifle")
		weight := w.tuning.Tables.WeightOf(weapon)
		worst, recoil := 0.0, 0.0
		for shot := 0; shot < 8; shot++ {
			at := now.Add(time.Duration(shot) * cadence)
			if shot > 0 {
				recoil = settledRecoil(recoil, cadence, weapon.Recoil.Recovery())
			}
			recoil += weapon.Recoil.DegreesAt(shot) * weight.RecoilMultiplier
			w.projectiles = map[string]*Projectile{}
			w.ApplyInput(p.ID, protocol.Input{Sequence: uint32(shot + 1), Buttons: buttons | ButtonFire, AimX: 1, ClientTimeMS: uint64(at.UnixMilli())})
			w.Step(at)
			// The pattern is identical in both runs, so subtracting it leaves
			// exactly what the body's own speed cost the shot.
			off := math.Abs(degreesOff(launched(w, p.ID)[0], Vec{1, 0}) - recoil)
			worst = math.Max(worst, off)
		}
		w.RemovePlayer(p.ID)
		return worst
	}
	standing, moving := widest(0), widest(ButtonDown)
	if standing > weapon.Spread.StandingDegrees {
		t.Fatalf("a standing shot spread %.3f degrees, past the weapon's %.3f floor", standing, weapon.Spread.StandingDegrees)
	}
	if moving <= standing {
		t.Fatalf("moving spread %.3f is no wider than standing %.3f", moving, standing)
	}
}

// A shotgun puts a cone into the world and divides the band between its
// pellets, so a full connect is worth exactly one band hit — the category is a
// condition, never extra damage.
func TestShotgunFansPelletsAndDividesTheBand(t *testing.T) {
	w, now := testWorld()
	p := carrying(t, w, addTestPlayer(w, "p", model.Gunslinger, Vec{1500, 0}, now), "breaching-shotgun")
	spec := *equippedAbility(w, p).Projectile
	fire(w, p, 1, now)

	directions := launched(w, p.ID)
	if len(directions) != spec.PelletCount() {
		t.Fatalf("one use launched %d bodies, want %d pellets", len(directions), spec.PelletCount())
	}
	spread := degreesOff(directions[len(directions)-1], directions[0])
	if math.Abs(math.Abs(spread)-spec.PelletSpreadDegrees) > 1 {
		t.Fatalf("the cone spans %.2f degrees, want %.2f", spread, spec.PelletSpreadDegrees)
	}
	total := 0.0
	for _, id := range sortedProjectileIDs(w.projectiles) {
		total += w.projectiles[id].Damage
	}
	if band := w.tuning.Tables.BandDamage(equippedAbility(w, p).DamageBand); math.Abs(total-band) > 0.001 {
		t.Fatalf("a full cone is worth %g, want the band's %g", total, band)
	}
}

// A weight class only moves handling. A heavy weapon slows its carrier and
// throws its shots wider; it never deals more.
func TestWeightClassMovesHandlingAndNeverDamage(t *testing.T) {
	w, now := testWorld()
	light := carrying(t, w, addTestPlayer(w, "light", model.Gunslinger, Vec{1500, 0}, now), "field-pistol")
	heavy := carrying(t, w, addTestPlayer(w, "heavy", model.Gunslinger, Vec{1600, 0}, now), "support-lmg")
	if w.handlingScale(heavy) >= w.handlingScale(light) {
		t.Fatalf("heavy handling scale %g is not slower than light %g", w.handlingScale(heavy), w.handlingScale(light))
	}
	if equippedDamage(w, heavy) != equippedDamage(w, light) {
		t.Fatalf("weight changed damage: heavy %g, light %g", equippedDamage(w, heavy), equippedDamage(w, light))
	}
}

// A heavy category may only be carried as something that was actually built,
// which is how rare materials gate it economically rather than statistically.
func TestWithheldCategoriesCannotBeCarriedAsStockRows(t *testing.T) {
	w, now := testWorld()
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{}, now)
	p.Unlocks, _ = p.Unlocks.With("long-sniper")
	set := p.Loadout.Clone()
	set.Weapon = "long-sniper"
	if _, err := w.SetLoadout(p.ID, set, now); err == nil {
		t.Fatal("a withheld category was equipped without ever being built")
	}
	// Built, it equips like anything else, and its material cost is what was
	// paid for the privilege.
	if cost := w.tuning.Tables.Weapons["long-sniper"].Cost; len(cost) == 0 {
		t.Fatal("a withheld category costs no materials")
	}
}

// Scoping is a commitment: it slows the body, steadies the shot, and is the
// only state a hitscan weapon may fire from at all.
func TestScopeGatesHitscanAndCostsMovement(t *testing.T) {
	w, now := testWorld()
	p := carrying(t, w, addTestPlayer(w, "p", model.Gunslinger, Vec{1500, 0}, now), "long-sniper")

	fire(w, p, 1, now)
	if len(w.projectiles) != 0 || p.Ammo != equippedWeapon(w, p).MagazineSize {
		t.Fatalf("an unscoped sniper fired: %d rounds, %d ammo", len(w.projectiles), p.Ammo)
	}
	unscoped := w.handlingScale(p)
	w.ApplyInput(p.ID, protocol.Input{Sequence: 2, Buttons: ButtonScope | ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now.Add(time.Second))
	if !p.Scoped {
		t.Fatal("holding the scope button did not scope")
	}
	if w.handlingScale(p) >= unscoped {
		t.Fatalf("scoping did not cost movement: %g scoped, %g hip", w.handlingScale(p), unscoped)
	}
	if p.Ammo != equippedWeapon(w, p).MagazineSize-1 {
		t.Fatalf("a scoped sniper did not fire: ammo = %d", p.Ammo)
	}
}

// Inside its instant reach a scoped sniper round lands without ever becoming a
// projectile; past it, the round travels and its damage decays.
func TestHitscanLandsInstantlyAndFallsOffPastItsCap(t *testing.T) {
	w, now := testWorld()
	spec := *w.tuning.Tables.Abilities["sniper-shot"].Projectile
	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now), "long-sniper")
	target := addTestPlayer(w, "target", model.Mage, Vec{1200 + spec.HitscanRange/2, 0}, now)

	w.ApplyInput(shooter.ID, protocol.Input{Sequence: 1, Buttons: ButtonScope | ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now)
	if len(w.projectiles) != 0 {
		t.Fatalf("a hitscan round inside its cap became %d projectiles", len(w.projectiles))
	}
	if want := w.tuning.MaxHealth - equippedDamage(w, shooter); target.Health != want {
		t.Fatalf("target health = %g, want %g on the same tick the shot was fired", target.Health, want)
	}
	// Nothing inside the cap: the round travels on from the cap, already
	// carrying the distance it covered instantly.
	empty, emptyNow := testWorld()
	lone := carrying(t, empty, addTestPlayer(empty, "lone", model.Gunslinger, Vec{1200, 0}, emptyNow), "long-sniper")
	empty.ApplyInput(lone.ID, protocol.Input{Sequence: 1, Buttons: ButtonScope | ButtonFire, AimX: 1, ClientTimeMS: uint64(emptyNow.UnixMilli())})
	empty.Step(emptyNow)
	if len(empty.projectiles) != 1 {
		t.Fatalf("a hitscan round that reached nothing produced %d travelling rounds", len(empty.projectiles))
	}
	for _, id := range sortedProjectileIDs(empty.projectiles) {
		round := empty.projectiles[id]
		if round.Travelled < spec.HitscanRange {
			t.Fatalf("the travelling round starts at %g, want the %g cap it already covered", round.Travelled, spec.HitscanRange)
		}
		if round.hitDamage() >= round.Damage {
			t.Fatalf("a round past the falloff start is still worth full damage: %g of %g", round.hitDamage(), round.Damage)
		}
	}
}

// A round stops at the weapon's hard maximum range. Beyond it nothing lands at
// all, which is what makes range a real weapon property rather than a lifetime.
func TestMaxRangeStopsARoundEvenWithLifetimeLeft(t *testing.T) {
	w, now := testWorld()
	spec := tuning.Projectile{Kind: "test", Speed: 1000, LifeSeconds: 10, Radius: 4, MaxRange: 300}
	projectile := &Projectile{OwnerID: "nobody", Damage: 10, Remaining: 10, Spec: spec}
	projectile.Entity = w.newProjectileEntity("p-test", Vec{}, Vec{spec.Speed, 0}, spec.Radius)
	for step := 0; step < 60; step++ {
		if w.advanceProjectile(projectile, 1.0/60, now, false) {
			if projectile.Travelled < spec.MaxRange {
				t.Fatalf("the round stopped after %g, short of its %g range", projectile.Travelled, spec.MaxRange)
			}
			return
		}
	}
	t.Fatalf("the round flew past its %g maximum range to %g", spec.MaxRange, projectile.Travelled)
}

// The riot shield answers a burst opener and nothing else: it blocks what flies
// into its frontal arc, locks the fire that raises it, and leaves the back open.
func TestRiotShieldBlocksItsArcAndLocksFire(t *testing.T) {
	w, now := testWorld()
	defender := addTestPlayer(w, "defender", model.Gunslinger, Vec{1400, 0}, now)
	defender.Unlocks, _ = defender.Unlocks.With("riot-shield")
	defender.Loadout.Gadgets[0] = "riot-shield"
	// Slot one is the shield; the shield is raised by holding the use button.
	w.ApplyInput(defender.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: -1, SelectedSlot: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now)
	if !defender.Guarding {
		t.Fatal("holding the use button on a shield slot did not raise it")
	}
	if len(w.projectiles) != 0 {
		t.Fatal("a raised shield fired")
	}
	blocked := w.handlingScale(defender)

	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now), "starter-rifle")
	for step := 1; step <= 40; step++ {
		at := now.Add(time.Duration(step) * time.Second / 60)
		w.ApplyInput(defender.ID, protocol.Input{Sequence: uint32(step + 1), Buttons: ButtonFire, AimX: -1, SelectedSlot: 1, ClientTimeMS: uint64(at.UnixMilli())})
		w.ApplyInput(shooter.ID, protocol.Input{Sequence: uint32(step), Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(at.UnixMilli())})
		w.Step(at)
	}
	if defender.Health != w.tuning.MaxHealth {
		t.Fatalf("the shield's own arc let a round through: health %g", defender.Health)
	}
	if blocked >= 1 {
		t.Fatalf("raising a shield cost no mobility: handling scale %g", blocked)
	}

	// Turned away, the same rounds land: the arc is a direction, not immunity.
	for step := 41; step <= 80; step++ {
		at := now.Add(time.Duration(step) * time.Second / 60)
		w.ApplyInput(defender.ID, protocol.Input{Sequence: uint32(step + 1), Buttons: ButtonFire, AimX: 1, SelectedSlot: 1, ClientTimeMS: uint64(at.UnixMilli())})
		w.ApplyInput(shooter.ID, protocol.Input{Sequence: uint32(step), Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(at.UnixMilli())})
		w.Step(at)
	}
	if defender.Health == w.tuning.MaxHealth {
		t.Fatal("a shield raised away from the shooter still blocked every round")
	}
}

// Crafted ammunition is finite: the launcher spends a carried stack, there is no
// reload to fall back on, and an empty stack simply cannot fire.
func TestSpecialAmmunitionIsSpentAndCraftedRatherThanReloaded(t *testing.T) {
	w, now := testWorld()
	recipe := w.tuning.Tables.Ammunition["rocket"]
	p := carrying(t, w, addTestPlayer(w, "p", model.Gunslinger, Vec{1500, 0}, now), "field-launcher")

	fire(w, p, 1, now)
	if len(w.projectiles) != 0 || !p.ReloadEnds.IsZero() {
		t.Fatalf("an empty launcher fired or began a reload it has no magazine for: %d rounds", len(w.projectiles))
	}
	// Crafting is the only source, and it is gated to safety like every other
	// build; the materials it costs have to be carried there first.
	p.Position = Vec{}
	if _, err := w.CraftAmmunition(p.ID, "rocket"); err == nil {
		t.Fatal("a batch was built with no materials")
	}
	for material, count := range recipe.Cost {
		p.Materials[material] = count
	}
	if _, err := w.CraftAmmunition(p.ID, "rocket"); err != nil {
		t.Fatalf("an affordable batch was refused: %v", err)
	}
	if p.Materials[recipe.Material] != recipe.Count {
		t.Fatalf("carried rounds = %d, want the batch's %d", p.Materials[recipe.Material], recipe.Count)
	}
	p.Position = Vec{1500, 0}
	fire(w, p, 2, now.Add(time.Second))
	if p.Materials[recipe.Material] != recipe.Count-1 {
		t.Fatalf("firing spent %d rounds, want one", recipe.Count-p.Materials[recipe.Material])
	}
	if len(w.projectiles) != 1 {
		t.Fatalf("a paid-for rocket launched %d rounds", len(w.projectiles))
	}
}

// A rocket's area is what makes the launcher a control tool: everyone inside the
// blast takes the band and its knockback, not only whatever the round touched.
func TestRocketBlastReachesEveryoneInsideItsRadius(t *testing.T) {
	w, now := testWorld()
	ability := w.tuning.Tables.Abilities["launcher-rocket"]
	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now), "field-launcher")
	shooter.Materials[ability.Cost.Material] = 4
	struck := addTestPlayer(w, "struck", model.Mage, Vec{1500, 0}, now)
	splashed := addTestPlayer(w, "splashed", model.Mage, Vec{1500, ability.Blast.Radius * 0.6}, now)

	w.ApplyInput(shooter.ID, protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	for step := 0; step <= 120 && struck.Health == w.tuning.MaxHealth; step++ {
		w.Step(now.Add(time.Duration(step) * time.Second / 60))
	}
	if struck.Health == w.tuning.MaxHealth {
		t.Fatal("the rocket never landed")
	}
	if splashed.Health == w.tuning.MaxHealth {
		t.Fatalf("the blast missed a body %g from the impact", ability.Blast.Radius*0.6)
	}
	if len(splashed.Effects) == 0 {
		t.Fatal("the blast applied none of the effects it declares")
	}
}

// Spread is a deterministic draw from the shooter and its own shot count, so one
// shot is reproducible for a test while remaining unpredictable to a player.
func TestSpreadIsReproducibleForTheSameShooterAndShot(t *testing.T) {
	// The same shooter, the same shot count, and the same speed: two separate
	// worlds must produce the identical deviation.
	sample := func() float64 {
		w, now := testWorld()
		p := carrying(t, w, addTestPlayer(w, "p", model.Gunslinger, Vec{1500, 0}, now), "compact-smg")
		// Moving, so the draw is scaled by a spread wide enough to measure.
		w.ApplyInput(p.ID, protocol.Input{Sequence: 1, Buttons: ButtonDown, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
		w.Step(now)
		w.ApplyInput(p.ID, protocol.Input{Sequence: 2, Buttons: ButtonDown | ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
		w.Step(now.Add(time.Second / 60))
		return degreesOff(launched(w, p.ID)[0], Vec{1, 0})
	}
	first, second := sample(), sample()
	if first != second {
		t.Fatalf("the same shot spread %.6f and then %.6f degrees", first, second)
	}
	if first == 0 {
		t.Fatal("a moving SMG shot left exactly on aim, so nothing was drawn at all")
	}
}
