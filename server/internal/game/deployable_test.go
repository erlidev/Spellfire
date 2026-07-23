package game

import (
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

// throwing equips a gadget in the first gadget slot, selects it, and uses it.
// It is the shape a real Gunslinger throw takes: the slot is the binding, and
// the ability the slot names is what the use button performs.
func throwing(t *testing.T, w *World, p *Player, gadgetID string, aim Vec, at time.Time) {
	t.Helper()
	p.Unlocks, _ = p.Unlocks.With(gadgetID)
	p.Loadout.Gadgets[0] = gadgetID
	w.ApplyInput(p.ID, protocol.Input{
		Sequence: uint32(at.UnixMilli()), Buttons: ButtonFire, SelectedSlot: 1,
		AimX: float32(aim.X), AimY: float32(aim.Y), ClientTimeMS: uint64(at.UnixMilli()),
	})
	w.Step(at)
}

// visible reports whether one body reaches another's snapshot at all. It is the
// only honest test of a concealment rule: what a player cannot see, the client is
// never sent, so hiding is absence from the wire rather than a render flag.
func visible(w *World, viewerID, targetID string, now time.Time) bool {
	for _, entity := range w.SnapshotFor(viewerID, now, protocol.ServerSnapshot).Entities {
		if entity.ID == targetID {
			return true
		}
	}
	return false
}

// A thrown canister leaves a cloud standing where it stopped, and the cloud
// expires on its own: a deployable is placed, not permanent.
func TestSmokeDeploysWhereItLandsAndExpires(t *testing.T) {
	w, now := testWorld()
	p := carrying(t, w, addTestPlayer(w, "p", model.Gunslinger, Vec{1500, 0}, now), "starter-rifle")
	field := w.tuning.Tables.Abilities["smoke-throw"].Deployable

	throwing(t, w, p, "smoke-grenade", Vec{1, 0}, now)
	if len(w.projectiles) != 1 {
		t.Fatalf("throwing a canister put %d bodies into the world", len(w.projectiles))
	}
	// The canister is spent before its throw ends and the field takes its place.
	tick := time.Second / time.Duration(w.tuning.TickRate)
	landed := now
	for step := 0; step < w.tuning.TickRate*2 && len(w.deployables) == 0; step++ {
		landed = landed.Add(tick)
		w.Step(landed)
	}
	clouds := w.Deployables()
	if len(clouds) != 1 {
		t.Fatalf("the canister left %d clouds behind", len(clouds))
	}
	if clouds[0].Field.Radius != field.Radius {
		t.Fatalf("the cloud covers %g, want the authored %g", clouds[0].Field.Radius, field.Radius)
	}
	if clouds[0].Position.X <= p.Position.X {
		t.Fatal("the canister deployed behind the body that threw it")
	}
	w.Step(landed.Add(field.Duration() + time.Second))
	for _, cloud := range w.Deployables() {
		if !cloud.Deleting {
			t.Fatalf("the cloud outlived its %s window", field.Duration())
		}
	}
}

// A concealing cloud casts a shadow: a body behind it is hidden exactly as a
// wall hides one, a body beside the sightline stays visible, and a body only
// half-swallowed is still seen because occlusion is a property of the whole
// silhouette, not its centre. Standing inside the cloud a body sees only a small
// circle around itself, which is what lets it peek out at the rim.
func TestSmokeCastsShadowAndRevealsUpClose(t *testing.T) {
	w, now := testWorld()
	field := *w.tuning.Tables.Abilities["smoke-throw"].Deployable
	radius, reveal, pr := field.Radius, field.RevealRadius, w.tuning.PlayerRadius
	cloud := Vec{1350, 0}
	w.deploy("", field, cloud, "", now)

	viewer := addTestPlayer(w, "viewer", model.Gunslinger, Vec{cloud.X - radius - 200, 0}, now)
	target := addTestPlayer(w, "target", model.Gunslinger, Vec{cloud.X + radius + 100, 0}, now)
	place := func(p *Player, at Vec) { p.Position = at; w.recordHistory(p, now) }

	// Directly behind the cloud, on the sightline: hidden.
	if visible(w, viewer.ID, target.ID, now) {
		t.Fatal("a cloud failed to hide the body directly behind it")
	}
	// Beside the cloud, sightline well clear of it: visible.
	place(target, Vec{cloud.X, cloud.Y + radius + 4*pr})
	if !visible(w, viewer.ID, target.ID, now) {
		t.Fatal("a cloud hid a body whose sightline never crosses it")
	}
	// Half-swallowed at the near rim: the exposed cap has a clear line, so the
	// body is still seen. This is the any-part-visible rule the client draws.
	place(target, Vec{cloud.X - radius + pr/2, 0})
	if !visible(w, viewer.ID, target.ID, now) {
		t.Fatal("a body half out of the cloud vanished; occlusion used its centre alone")
	}
	// Inside the cloud a body sees only its reveal circle: a target inside the
	// same cloud but past that circle is swallowed.
	place(viewer, cloud)
	place(target, Vec{cloud.X + (radius+reveal)/2, 0})
	if visible(w, viewer.ID, target.ID, now) {
		t.Fatal("smoke revealed a body past the reveal circle of the viewer standing in it")
	}
	// A body inside the reveal circle is seen, so a contact fight is not decided
	// by who threw the canister.
	place(target, Vec{cloud.X + reveal/2, 0})
	if !visible(w, viewer.ID, target.ID, now) {
		t.Fatal("the cloud hid a body close enough to touch")
	}
	// Standing at the rim, the reveal circle reaches past the cloud's edge, so
	// the viewer peeks out and sees a body just outside the smoke.
	place(viewer, Vec{cloud.X + radius - reveal/4, 0})
	place(target, Vec{cloud.X + radius + reveal/4, 0})
	if !visible(w, viewer.ID, target.ID, now) {
		t.Fatal("a body at the rim could not peek out of its own smoke")
	}
}

// A flashbang takes vision whole and takes nothing else: no damage, no slow,
// and only for as long as the effect runs.
func TestFlashbangBlindsWithoutDamaging(t *testing.T) {
	w, now := testWorld()
	thrower := carrying(t, w, addTestPlayer(w, "thrower", model.Gunslinger, Vec{1500, 0}, now), "starter-rifle")
	target := addTestPlayer(w, "target", model.Gunslinger, Vec{1700, 0}, now)
	bystander := addTestPlayer(w, "bystander", model.Gunslinger, Vec{1500, 400}, now)
	health := target.Health
	blast := w.tuning.Tables.Abilities["flash-throw"].Blast

	throwing(t, w, thrower, "flashbang", Vec{1, 0}, now)
	tick := time.Second / time.Duration(w.tuning.TickRate)
	at := now
	for step := 0; step < w.tuning.TickRate*2 && !w.blinded(target); step++ {
		at = at.Add(tick)
		w.Step(at)
	}
	if !w.blinded(target) {
		t.Fatalf("the canister never went off inside its %g radius", blast.Radius)
	}
	if target.Health != health {
		t.Fatalf("a flashbang took %g health; it is a control tool, not damage", health-target.Health)
	}
	// A blinded body sees itself and nothing else: the blackout is enforced on
	// the wire, not drawn over on the client.
	snapshot := w.SnapshotFor(target.ID, at, protocol.ServerSnapshot)
	for _, entity := range snapshot.Entities {
		if entity.Type == protocol.EntityPlayer && entity.ID != target.ID {
			t.Fatalf("a blinded body was still sent %q", entity.ID)
		}
	}
	if !visible(w, bystander.ID, thrower.ID, at) {
		t.Fatal("the flash blinded someone it never reached")
	}
	blind := w.tuning.Tables.Effects["flash-blind"]
	w.Step(at.Add(blind.Duration() + time.Second))
	if w.blinded(target) {
		t.Fatalf("the blindness outlived its %s window", blind.Duration())
	}
}

// A gadget is thrown by hand, so it must not walk the pattern of the gun the
// body happens to be holding.
func TestThrowingAGadgetDoesNotWalkTheGun(t *testing.T) {
	w, now := testWorld()
	p := carrying(t, w, addTestPlayer(w, "p", model.Gunslinger, Vec{1500, 0}, now), "starter-rifle")
	cadence := equippedAbility(w, p).Interval() + time.Millisecond

	fire(w, p, 1, now)
	fire(w, p, 2, now.Add(cadence))
	walked, index := p.RecoilPeak, p.Shot
	throwing(t, w, p, "smoke-grenade", Vec{1, 0}, now.Add(2*cadence))
	if p.RecoilPeak != walked || p.Shot != index {
		t.Fatalf("a thrown canister moved the muzzle from %.3f/%d to %.3f/%d", walked, index, p.RecoilPeak, p.Shot)
	}
}

// Smoke changes what an opponent can see, never where a round may fly. A cloud
// casts a shadow, but never over the viewer's own rounds: shadowing the
// thrower's own bullets would make the cloud read as a wall it could not shoot
// through, the round vanishing at the edge and reappearing past it.
func TestSmokeDoesNotHideYourOwnRounds(t *testing.T) {
	w, now := testWorld()
	field := *w.tuning.Tables.Abilities["smoke-throw"].Deployable
	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{1000, 0}, now), "starter-rifle")
	other := carrying(t, w, addTestPlayer(w, "other", model.Gunslinger, Vec{1000, 300}, now), "starter-rifle")
	cloud := Vec{1350, 0}
	w.deploy("", field, cloud, "", now)

	fire(w, shooter, 1, now)
	fire(w, other, 1, now)
	var mine, theirs *Projectile
	for _, id := range sortedProjectileIDs(w.projectiles) {
		p := w.projectiles[id]
		p.Position = cloud // inside the cloud, on each shooter's sightline
		if p.OwnerID == shooter.ID {
			mine = p
		} else {
			theirs = p
		}
	}
	if mine == nil || theirs == nil {
		t.Fatalf("expected one round from each body, got %d", len(w.projectiles))
	}
	if !visible(w, shooter.ID, mine.ID, now) {
		t.Fatal("the cloud swallowed the shooter's own round, which reads as a wall rather than a shadow")
	}
	if visible(w, shooter.ID, theirs.ID, now) {
		t.Fatal("the cloud failed to hide an opponent's round behind it")
	}
	// An opponent's round half out of the near rim is seen: rounds follow the
	// same any-part-visible rule bodies do.
	edge := theirs.circleRadius()
	theirs.Position = Vec{cloud.X - field.Radius + edge/2, 0}
	if !visible(w, shooter.ID, theirs.ID, now) {
		t.Fatal("a round half out of the cloud vanished instead of flying past it")
	}
}

// A canister reaches the ground far more often than it hits a body, so a blast
// that only resolved on a direct impact would effectively never go off.
func TestFlashbangDetonatesWhereItLands(t *testing.T) {
	w, now := testWorld()
	thrower := carrying(t, w, addTestPlayer(w, "thrower", model.Gunslinger, Vec{1500, 0}, now), "starter-rifle")
	// Off the throw line and inside the blast where the canister comes to rest:
	// nothing is hit on the way, so only a landed detonation can reach it.
	target := addTestPlayer(w, "target", model.Gunslinger, Vec{1900, 120}, now)
	health := target.Health

	throwing(t, w, thrower, "flashbang", Vec{1, 0}, now)
	tick := time.Second / time.Duration(w.tuning.TickRate)
	at := now
	for step := 0; step < w.tuning.TickRate*2 && !w.blinded(target); step++ {
		at = at.Add(tick)
		w.Step(at)
	}
	if !w.blinded(target) {
		t.Fatal("a canister that landed on open ground never went off")
	}
	if target.Health != health {
		t.Fatalf("a flashbang took %g health; it is a control tool, not damage", health-target.Health)
	}
	// The area resolves exactly once, however the round was reaped.
	blinds := 0
	for _, active := range target.Effects {
		if active.EffectID == "flash-blind" {
			blinds++
		}
	}
	if blinds != 1 {
		t.Fatalf("the canister applied its blindness %d times", blinds)
	}
}
