package game

import (
	"math"
	"testing"
	"testing/fstest"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

// The effects table ships empty: no design document has settled a burn's
// magnitude or a slow's severity, and inventing them here would be exactly the
// false precision the tables refuse elsewhere. The layer is therefore exercised
// against rows a test patch adds, which is also how Phase 2's elements will
// author them.
func withEffects(t *testing.T, rows map[string]any, applied ...string) fstest.MapFS {
	t.Helper()
	files := edit(t, shipped(t), "effects.json", func(document map[string]any) {
		for id, row := range rows {
			document[id] = row
		}
	})
	if len(applied) == 0 {
		return files
	}
	return edit(t, files, "abilities.json", func(document map[string]any) {
		effects := make([]any, 0, len(applied))
		for _, id := range applied {
			effects = append(effects, id)
		}
		document["rifle-shot"].(map[string]any)["effects"] = effects
	})
}

func effectRow(kind string, durationMS int, fields map[string]any) map[string]any {
	row := map[string]any{"name": kind, "kind": kind, "duration_ms": float64(durationMS), "stacking": "refresh"}
	for key, value := range fields {
		row[key] = value
	}
	return row
}

// steps advances the world by whole ticks from a start time.
func steps(w *World, from time.Time, count int) time.Time {
	at := from
	for i := 0; i < count; i++ {
		at = from.Add(time.Duration(i+1) * time.Second / 60)
		w.Step(at)
	}
	return at
}

// A burn deals its damage over time, drawn from the shared band rather than
// authored on the effect, and stops when its window closes.
func TestBurnTicksFromTheSharedBandAndExpires(t *testing.T) {
	w, now := worldFrom(t, withEffects(t, map[string]any{
		"burn-test": effectRow("burn", 250, map[string]any{"tick_ms": 100.0, "damage_fraction": 0.5, "damage_band": "standard"}),
	}))
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{1200, 0}, now)
	perTick := 0.5 * w.tuning.Tables.BandDamage("standard")

	w.applyEffects(p, []string{"burn-test"}, "source", Vec{1, 0}, now)
	steps(w, now, 6) // 100 ms: one tick has landed
	if math.Abs(p.Health-(w.tuning.MaxHealth-perTick)) > 0.001 {
		t.Fatalf("health after one burn tick = %g, want %g", p.Health, w.tuning.MaxHealth-perTick)
	}
	steps(w, now, 60) // one second: the window closed after the second tick
	if math.Abs(p.Health-(w.tuning.MaxHealth-2*perTick)) > 0.001 {
		t.Fatalf("health after the burn = %g, want %g", p.Health, w.tuning.MaxHealth-2*perTick)
	}
	if len(p.Effects) != 0 {
		t.Fatalf("an expired burn is still running: %#v", p.Effects)
	}
}

// A slow scales movement; two slows take the strongest rather than compounding
// into a root that no dodge answers.
func TestSlowScalesMovementAndDoesNotCompound(t *testing.T) {
	w, now := worldFrom(t, withEffects(t, map[string]any{
		"slow-half":    effectRow("slow", 5000, map[string]any{"speed_multiplier": 0.5}),
		"slow-quarter": effectRow("slow", 5000, map[string]any{"speed_multiplier": 0.25}),
	}))
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{}, now)
	w.ApplyInput(p.ID, protocol.Input{Sequence: 1, Buttons: ButtonRight, AimX: 1})

	w.applyEffects(p, []string{"slow-half", "slow-quarter"}, "source", Vec{1, 0}, now)
	w.Step(now.Add(time.Second / 60))
	want := w.tuning.PlayerSpeed * 0.25 / 60
	if math.Abs(p.Position.X-want) > 0.001 {
		t.Fatalf("slowed step = %g, want the strongest slow's %g", p.Position.X, want)
	}
}

// A root takes movement and the dash; it does not take the ability to act.
func TestRootStopsMovementButNotAction(t *testing.T) {
	w, now := worldFrom(t, withEffects(t, map[string]any{
		"root-test": effectRow("root", 5000, nil),
	}))
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{1200, 0}, now)
	w.applyEffects(p, []string{"root-test"}, "source", Vec{1, 0}, now)

	w.ApplyInput(p.ID, protocol.Input{Sequence: 1, Buttons: ButtonRight | ButtonDash | ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now.Add(time.Second / 60))
	if p.Position.X != 1200 || p.DashTicksLeft != 0 {
		t.Fatalf("a rooted player moved to %#v with %d dash ticks left", p.Position, p.DashTicksLeft)
	}
	if len(w.projectiles) != 1 {
		t.Fatal("a rooted player could not act")
	}
}

// A stun takes everything: movement, the dash, the reload, and the ability.
func TestStunSuppressesEveryAction(t *testing.T) {
	w, now := worldFrom(t, withEffects(t, map[string]any{
		"stun-test": effectRow("stun", 5000, nil),
	}))
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{1200, 0}, now)
	p.Ammo = 1
	w.applyEffects(p, []string{"stun-test"}, "source", Vec{1, 0}, now)

	w.ApplyInput(p.ID, protocol.Input{Sequence: 1, Buttons: ButtonRight | ButtonDash | ButtonFire | ButtonReload, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	w.Step(now.Add(time.Second / 60))
	if p.Position.X != 1200 || len(w.projectiles) != 0 || !p.ReloadEnds.IsZero() {
		t.Fatalf("a stunned player acted: position %#v, %d projectiles, reload %v", p.Position, len(w.projectiles), p.ReloadEnds)
	}
	if p.Ammo != 1 {
		t.Fatalf("a stunned player spent ammunition: %d", p.Ammo)
	}
}

// A knockback carries the body along the direction of the hit, overriding both
// input and an in-flight dash, and stops when its window closes.
func TestKnockbackOverridesInputAndDash(t *testing.T) {
	w, now := worldFrom(t, withEffects(t, map[string]any{
		"knock-test": effectRow("knockback", 200, map[string]any{"speed": 600.0}),
	}))
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{}, now)
	w.ApplyInput(p.ID, protocol.Input{Sequence: 1, Buttons: ButtonLeft | ButtonDash, AimX: -1})
	w.Step(now.Add(time.Second / 60))
	if p.DashTicksLeft == 0 {
		t.Fatal("the dash under test never started")
	}

	w.applyEffects(p, []string{"knock-test"}, "source", Vec{1, 0}, now)
	pushed := steps(w, now, 12) // 200 ms of knockback
	if p.Position.X <= 0 {
		t.Fatalf("the knockback did not carry the body against its input: %#v", p.Position)
	}
	if p.DashTicksLeft != 0 {
		t.Fatal("the knockback did not cancel the dash it overrode")
	}
	// Once it expires the body answers its own input again.
	displaced := p.Position.X
	steps(w, pushed, 6)
	if p.Position.X >= displaced {
		t.Fatalf("the knockback outlived its duration: %g then %g", displaced, p.Position.X)
	}
}

// A shield absorbs before health and falls away once its pool is spent, and its
// pool is a multiple of the shared band rather than a number of its own.
func TestShieldAbsorbsBeforeHealthAndFallsAway(t *testing.T) {
	w, now := worldFrom(t, withEffects(t, map[string]any{
		"shield-test": effectRow("shield", 5000, map[string]any{"absorb_hits": 1.0, "damage_band": "standard"}),
	}))
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{1200, 0}, now)
	hit := w.tuning.Tables.BandDamage("standard")
	w.applyEffects(p, []string{"shield-test"}, "source", Vec{1, 0}, now)

	w.damage(p, hit*1.5, "source", now)
	if want := w.tuning.MaxHealth - hit*0.5; math.Abs(p.Health-want) > 0.001 {
		t.Fatalf("health = %g, want %g: the shield should absorb one band hit first", p.Health, want)
	}
	steps(w, now, 1)
	if len(p.Effects) != 0 {
		t.Fatalf("a spent shield is still running: %#v", p.Effects)
	}
	w.damage(p, hit, "source", now)
	if want := w.tuning.MaxHealth - hit*1.5; math.Abs(p.Health-want) > 0.001 {
		t.Fatalf("health = %g, want %g: the spent shield still absorbed", p.Health, want)
	}
}

// The whole framework end to end: an ability declares an effect, its projectile
// carries it, and the hit applies it. PvP protection covers the status as well
// as the damage — a slow landed from inside safety would make a safe zone an
// offensive tool.
func TestAbilityEffectsTravelWithTheProjectileAndRespectPvPProtection(t *testing.T) {
	files := withEffects(t, map[string]any{
		"slow-test": effectRow("slow", 5000, map[string]any{"speed_multiplier": 0.5}),
	}, "slow-test")

	w, now := worldFrom(t, files)
	// The edited row is the rifle's, so the shooter carries a rifle rather than
	// whatever category its starter draw happened to hand it.
	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now), "starter-rifle")
	target := addTestPlayer(w, "target", model.Mage, Vec{1300, 0}, now)
	fire(w, shooter, 1, now)
	for i := 1; i <= 60 && len(target.Effects) == 0; i++ {
		w.Step(now.Add(time.Duration(i) * time.Second / 60))
	}
	if len(target.Effects) != 1 || target.Effects[0].EffectID != "slow-test" {
		t.Fatalf("the hit did not apply the ability's effect: %#v", target.Effects)
	}
	if target.Effects[0].SourceID != shooter.ID {
		t.Fatalf("effect source = %q, want the shooter", target.Effects[0].SourceID)
	}

	protected, protectedNow := worldFrom(t, files)
	inside := carrying(t, protected, addTestPlayer(protected, "inside", model.Gunslinger, Vec{100, 0}, protectedNow), "starter-rifle")
	bystander := addTestPlayer(protected, "bystander", model.Mage, Vec{200, 0}, protectedNow)
	fire(protected, inside, 1, protectedNow)
	for i := 1; i <= 60; i++ {
		protected.Step(protectedNow.Add(time.Duration(i) * time.Second / 60))
	}
	if len(bystander.Effects) != 0 || bystander.Health != protected.tuning.MaxHealth {
		t.Fatalf("PvP protection let a status through: %#v at %g health", bystander.Effects, bystander.Health)
	}
}

// Death clears what the body was carrying, so a respawn is not haunted by the
// burn that killed it.
func TestDeathClearsRunningEffects(t *testing.T) {
	w, now := worldFrom(t, withEffects(t, map[string]any{
		"slow-test": effectRow("slow", 60000, map[string]any{"speed_multiplier": 0.5}),
	}))
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{1200, 0}, now)
	w.applyEffects(p, []string{"slow-test"}, "source", Vec{1, 0}, now)

	w.damage(p, w.tuning.MaxHealth, "source", now)
	if p.Alive || len(p.Effects) != 0 {
		t.Fatalf("a dead body still carries %#v", p.Effects)
	}
	w.Respawn(p.ID, now)
	if len(p.Effects) != 0 || len(p.Cooldowns) != 0 {
		t.Fatalf("a respawned body carries %#v and %#v", p.Effects, p.Cooldowns)
	}
}
