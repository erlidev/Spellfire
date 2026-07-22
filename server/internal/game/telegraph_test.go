package game

import (
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

func TestWindupLocksTelegraphGeometryAndDelaysDelivery(t *testing.T) {
	w, now := testWorld()
	mage := addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now)
	ability := equippedAbility(w, mage)
	startMana := mage.Mana

	fire(w, mage, 1, now)
	if len(w.telegraphs) != 1 || ownedProjectiles(w, mage.ID) != 0 {
		t.Fatalf("committed cast has telegraphs=%d projectiles=%d", len(w.telegraphs), ownedProjectiles(w, mage.ID))
	}
	telegraph := w.telegraphs[sortedTelegraphIDs(w.telegraphs)[0]]
	if telegraph.Position != (Vec{1200, 0}) || telegraph.Direction != (Vec{1, 0}) {
		t.Fatalf("initial telegraph geometry = %#v", telegraph)
	}
	if mage.Mana > startMana-ability.Cost.Amount {
		t.Fatalf("windup did not commit its cost: mana=%g", mage.Mana)
	}

	// The caster moves and reverses aim during the windup; the readable danger
	// area and eventual projectile stay on the geometry committed at use.
	w.ApplyInput(mage.ID, protocol.Input{Sequence: 2, Buttons: ButtonDown, AimX: -1, ClientTimeMS: uint64(now.Add(100 * time.Millisecond).UnixMilli())})
	w.Step(now.Add(100 * time.Millisecond))
	if telegraph.Position != (Vec{1200, 0}) || telegraph.Direction != (Vec{1, 0}) {
		t.Fatalf("telegraph tracked its owner: %#v", telegraph)
	}
	if telegraph.state(now.Add(100*time.Millisecond)) != protocol.TelegraphPending {
		t.Fatal("telegraph left pending before the windup elapsed")
	}

	activation := now.Add(ability.Windup())
	w.Step(activation)
	if ownedProjectiles(w, mage.ID) != 1 || telegraph.state(activation) != protocol.TelegraphActive {
		t.Fatalf("activation produced projectiles=%d state=%d", ownedProjectiles(w, mage.ID), telegraph.state(activation))
	}
	for _, projectile := range w.projectiles {
		if projectile.OwnerID == mage.ID && (projectile.Velocity.X <= 0 || projectile.Velocity.Y != 0) {
			t.Fatalf("projectile did not follow locked direction: %#v", projectile.Velocity)
		}
	}

	resolvedAt := telegraph.ActiveUntil
	w.Step(resolvedAt)
	if telegraph.state(resolvedAt) != protocol.TelegraphResolved {
		t.Fatalf("state at resolution = %d", telegraph.state(resolvedAt))
	}
	w.Step(telegraph.ExpiresAt)
	if len(w.telegraphs) != 0 {
		t.Fatal("resolved telegraph did not expire")
	}
}

func TestDeathCancelsPendingTelegraphWithResolutionFlash(t *testing.T) {
	w, now := testWorld()
	mage := addTestPlayer(w, "mage", model.Mage, Vec{1200, 0}, now)
	fire(w, mage, 1, now)
	telegraph := w.telegraphs[sortedTelegraphIDs(w.telegraphs)[0]]
	deathAt := now.Add(100 * time.Millisecond)
	w.damage(mage, w.tuning.MaxHealth, "attacker", deathAt)
	if telegraph.state(deathAt) != protocol.TelegraphResolved || !telegraph.Delivered {
		t.Fatalf("cancelled telegraph = %#v", telegraph)
	}
	w.Step(now.Add(equippedAbility(w, mage).Windup()))
	if ownedProjectiles(w, mage.ID) != 0 {
		t.Fatal("a dead caster's pending action delivered")
	}
}

func TestSnapshotCarriesTelegraphAndExpandedPlayerState(t *testing.T) {
	w, now := testWorld()
	viewer := addTestPlayer(w, "viewer", model.Gunslinger, Vec{1200, 0}, now)
	mage := addTestPlayer(w, "mage", model.Mage, Vec{1250, 0}, now)
	mage.SquadID = "squad-a"
	mage.LingerUntil = now.Add(time.Second)
	mage.Effects = []ActiveEffect{{EffectID: "slow-test", ExpiresAt: now.Add(time.Second)}}
	w.startTelegraph(mage.ID, "fire", mage.Position, Vec{1, 0}, equippedAbility(w, mage), now)

	snapshot := w.SnapshotFor(viewer.ID, now.Add(100*time.Millisecond), protocol.ServerSnapshot)
	var player, warning *protocol.Entity
	for index := range snapshot.Entities {
		entity := &snapshot.Entities[index]
		switch entity.ID {
		case mage.ID:
			player = entity
		case "t-0":
			warning = entity
		}
	}
	if player == nil || player.Type != protocol.EntityPlayer || player.Element != "fire" || player.SquadID != "squad-a" || player.Allegiance != protocol.AllegianceHostile || !player.Lingering {
		t.Fatalf("expanded player = %#v", player)
	}
	if len(player.EffectIDs) != 1 || player.EffectIDs[0] != "slow-test" {
		t.Fatalf("effect IDs = %#v", player.EffectIDs)
	}
	if warning == nil || warning.Type != protocol.EntityTelegraph || warning.TelegraphShape != "line" || warning.TelegraphState != protocol.TelegraphPending || warning.Element != "fire" || warning.AbilityID != "fire-bolt-cast" {
		t.Fatalf("telegraph entity = %#v", warning)
	}
	if warning.TelegraphProgress <= 0 || warning.TelegraphProgress >= 1 || warning.Length <= 0 || warning.Width <= 0 {
		t.Fatalf("telegraph geometry/progress = %#v", warning)
	}
}
