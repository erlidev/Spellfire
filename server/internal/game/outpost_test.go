package game

import (
	"math"
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

// shippedOutpost picks one outpost from the live table by predicate, so these
// tests follow the shipped geography rather than restating it.
func shippedOutpost(t *testing.T, w *World, want func(tuning.Outpost) bool) tuning.Outpost {
	t.Helper()
	for _, id := range sortedKeys(w.tuning.Tables.Outposts) {
		if outpost := w.tuning.Tables.Outposts[id]; want(outpost) {
			return outpost
		}
	}
	t.Fatal("no shipped outpost matches")
	return tuning.Outpost{}
}

func outpostPosition(o tuning.Outpost) Vec { return Vec{o.Position[0], o.Position[1]} }

// Protection is no longer a radius from the origin: an outpost carries its own
// no-PvP bubble, and every zone rule resolves against it.
func TestProtectionFollowsOutpostsRatherThanTheOrigin(t *testing.T) {
	w := NewWorld(DefaultTuning())
	outpost := shippedOutpost(t, w, func(o tuning.Outpost) bool { return o.Band == "frontier" })
	at := outpostPosition(outpost)
	if !w.Protected(at) || !w.Safe(at) {
		t.Fatalf("%s is in the Frontier but its bubble does not protect: protected=%v safe=%v", outpost.ID, w.Protected(at), w.Safe(at))
	}
	// Just outside the bubble the Frontier is hostile again.
	outside := at.Add(Vec{outpost.SafeRadius + 50, 0})
	if w.Protected(outside) || w.Safe(outside) {
		t.Fatalf("%s protects ground outside its safe radius", outpost.ID)
	}
	// The band underfoot is still the radial field's answer: an outpost changes
	// safety, never geography.
	if w.DangerAt(at).ID != "frontier" {
		t.Fatalf("danger band at %s = %q, want frontier", outpost.ID, w.DangerAt(at).ID)
	}
}

// A forward outpost may offer less than the hub, and the gate is the service
// rather than the bubble.
func TestOutpostServicesGateLoadoutAndCrafting(t *testing.T) {
	w := NewWorld(DefaultTuning())
	full := shippedOutpost(t, w, func(o tuning.Outpost) bool { return o.Offers("loadout") })
	respawnOnly := shippedOutpost(t, w, func(o tuning.Outpost) bool { return !o.Offers("crafting") && o.Offers("respawn") })
	if !w.serviceAt(outpostPosition(full), "loadout") || !w.serviceAt(outpostPosition(full), "crafting") {
		t.Fatalf("%s declares loadout but does not offer it", full.ID)
	}
	if w.serviceAt(outpostPosition(respawnOnly), "crafting") {
		t.Fatalf("%s offers crafting it never declared", respawnOnly.ID)
	}
	// The hub offers everything without declaring anything.
	if !w.serviceAt(Vec{}, "loadout") || !w.serviceAt(Vec{}, "crafting") {
		t.Fatal("the central hub must offer every service")
	}
}

// Reaching an outpost unlocks it once, awards the discovery source, and marks
// the character for an immediate save.
func TestReachingAnOutpostDiscoversItOnce(t *testing.T) {
	w := NewWorld(DefaultTuning())
	now := time.Unix(1_700_000_000, 0)
	outpost := shippedOutpost(t, w, func(o tuning.Outpost) bool { return o.Band == "fringe" })
	p := addTestPlayer(w, "scout", model.Gunslinger, outpostPosition(outpost), now)
	if award := w.tuning.Tables.Progression.Award(tuning.SourceDiscovery); award <= 0 {
		t.Fatalf("the discovery source is priced at %d; nothing would be awarded", award)
	}
	beforeLevel, beforeXP := p.Level, p.XP
	w.stepDiscovery(p)
	if len(p.Outposts) != 1 || p.Outposts[0] != outpost.ID {
		t.Fatalf("unlocked outposts = %v, want [%s]", p.Outposts, outpost.ID)
	}
	// The award may cross a level, so progression is compared as a whole rather
	// than as a bare XP delta.
	if p.Level == beforeLevel && p.XP == beforeXP {
		t.Fatal("discovery awarded no progression at all")
	}
	if !w.stateDirty[p.ID] {
		t.Fatal("a discovery must be persisted immediately, like a loadout commit")
	}
	// Standing there does not award it again.
	level, xp := p.Level, p.XP
	w.stepDiscovery(p)
	if len(p.Outposts) != 1 || p.Level != level || p.XP != xp {
		t.Fatalf("re-entering the same outpost awarded again: outposts=%v level=%d xp=%d", p.Outposts, p.Level, p.XP)
	}
	if drained := w.DrainDirtyState(); len(drained) != 1 || drained[0] != p.ID {
		t.Fatalf("drained %v, want [%s] exactly once", drained, p.ID)
	}
	if len(w.DrainDirtyState()) != 0 {
		t.Fatal("draining twice must not report the same character again")
	}
}

// The rim has no outposts, so a Deadlands death lands on the nearest Frontier
// one — the walk back is the penalty, with no separate rule.
func TestADeadlandsDeathRespawnsAtTheNearestUnlockedOutpost(t *testing.T) {
	w := NewWorld(DefaultTuning())
	now := time.Unix(1_700_000_000, 0)
	outpost := shippedOutpost(t, w, func(o tuning.Outpost) bool { return o.Band == "frontier" })
	at := outpostPosition(outpost)
	// Die out past the Frontier, on the same heading as the chosen outpost so it
	// is unambiguously the nearest unlocked one.
	rim := at.Normalized().Mul(w.tuning.Tables.World.Radius - 200)
	p := addTestPlayer(w, "hauler", model.Gunslinger, rim, now)
	p.Outposts = []string{outpost.ID}
	p.Health, p.Alive = 0, false
	if !w.Respawn(p.ID, now) {
		t.Fatal("respawn rejected")
	}
	if p.Position != at {
		t.Fatalf("respawned at %#v, want the unlocked outpost at %#v", p.Position, at)
	}
	// With nothing unlocked, the same death falls back to the hub spawn ring.
	other := addTestPlayer(w, "greenhorn", model.Gunslinger, rim, now)
	other.Health, other.Alive = 0, false
	w.Respawn(other.ID, now)
	if distance := math.Sqrt(other.Position.LengthSq()); math.Abs(distance-w.tuning.Tables.World.SpawnRadius) > 0.001 {
		t.Fatalf("an undiscovered character respawned %g from the origin, want the hub spawn ring", distance)
	}
}

// Leaving a no-PvP bubble covers the transition out, and the player's own
// hostile action ends it early.
func TestExitInvulnerabilityCoversTheTransitionAndBreaksOnHostileAction(t *testing.T) {
	w := NewWorld(DefaultTuning())
	now := time.Unix(1_700_000_000, 0)
	outpost := shippedOutpost(t, w, func(o tuning.Outpost) bool { return o.Band == "frontier" })
	at := outpostPosition(outpost)
	p := carrying(t, w, addTestPlayer(w, "leaver", model.Gunslinger, at, now), "starter-rifle")

	// One tick inside the bubble records it as protected; the next, outside,
	// grants the invulnerability.
	w.updateExitInvuln(p, now)
	if p.exitInvulnerable(now) {
		t.Fatal("standing inside the bubble must not grant exit invulnerability")
	}
	w.SetPlayerPosition(p.ID, at.Add(Vec{outpost.SafeRadius + 60, 0}), now)
	w.updateExitInvuln(p, now)
	if !p.exitInvulnerable(now) {
		t.Fatal("leaving the bubble must grant exit invulnerability")
	}
	// It refuses damage whole for its window.
	p.Health = 100
	w.damage(p, 40, "attacker", now)
	if p.Health != 100 {
		t.Fatalf("health = %g while exit-invulnerable, want untouched", p.Health)
	}
	// Firing is the body's own hostile action: it ends there and then.
	p.Aim = Vec{1, 0}
	p.Input = protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())}
	if !w.useAbility(p, now) {
		t.Fatal("the body could not fire")
	}
	if p.exitInvulnerable(now) {
		t.Fatal("firing must break exit invulnerability")
	}
	w.damage(p, 40, "attacker", now)
	if p.Health != 60 {
		t.Fatalf("health = %g after the invulnerability broke, want 60", p.Health)
	}
	// It does not re-arm while standing outside.
	w.updateExitInvuln(p, now.Add(time.Second))
	if p.exitInvulnerable(now.Add(time.Second)) {
		t.Fatal("exit invulnerability re-armed without re-entering safety")
	}
}

// Terrain generation defers to an outpost's footprint, so the ground it stands
// on is always open.
func TestOutpostFootprintsStayClearOfTerrain(t *testing.T) {
	w := NewWorld(DefaultTuning())
	for _, id := range sortedKeys(w.tuning.Tables.Outposts) {
		outpost := w.tuning.Tables.Outposts[id]
		at := outpostPosition(outpost)
		w.loadChunksAround(at)
		if !w.standable(at) {
			t.Errorf("outpost %s stands on ground a body cannot occupy", id)
		}
		if w.collides(at, outpost.SafeRadius*0.5) {
			t.Errorf("outpost %s has terrain inside half its safe radius", id)
		}
	}
}
