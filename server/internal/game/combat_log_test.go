package game

import (
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/tuning"
)

func TestDamageLedgerCreditsMostEffectiveDamageNotLastHit(t *testing.T) {
	w, now := testWorld()
	target := addTestPlayer(w, "target", model.Mage, Vec{1200, 0}, now)

	w.damage(target, 60, "first", now)
	w.damage(target, 30, "second", now.Add(time.Millisecond))
	w.damage(target, 50, "last-hit", now.Add(2*time.Millisecond))

	kill, ok := w.LastKill(target.ID)
	if !ok {
		t.Fatal("lethal damage produced no kill event")
	}
	if kill.CreditID != "first" || kill.SourceID != "last-hit" {
		t.Fatalf("kill = %#v, want first credited and last-hit recorded", kill)
	}
	want := []DamageContribution{{SourceID: "first", Amount: 60}, {SourceID: "second", Amount: 30}, {SourceID: "last-hit", Amount: 10}}
	if len(kill.Contributions) != len(want) {
		t.Fatalf("contributions = %#v", kill.Contributions)
	}
	for index := range want {
		if kill.Contributions[index] != want[index] {
			t.Fatalf("contribution %d = %#v, want %#v", index, kill.Contributions[index], want[index])
		}
	}
}

func TestDamageLedgerExcludesShieldAbsorptionAndOverkill(t *testing.T) {
	w, now := testWorld()
	target := addTestPlayer(w, "target", model.Mage, Vec{}, now)
	target.Effects = []ActiveEffect{{EffectID: "test-shield", Absorb: 25, ExpiresAt: now.Add(time.Second)}}
	// Tests may install rows directly because the shipped effects table is
	// intentionally empty until content phases author settled magnitudes.
	w.tuning.Tables.Effects["test-shield"] = tuning.Effect{ID: "test-shield", Name: "Test shield", Kind: "shield"}
	defer delete(w.tuning.Tables.Effects, "test-shield")

	w.damage(target, 50, "attacker", now)
	if got := w.Contributions(target.ID); len(got) != 1 || got[0].Amount != 25 {
		t.Fatalf("shielded contribution = %#v, want 25 effective damage", got)
	}
	w.damage(target, 500, "attacker", now.Add(time.Millisecond))
	kill, _ := w.LastKill(target.ID)
	if kill.Contributions[0].Amount != w.tuning.MaxHealth {
		t.Fatalf("overkill contribution = %g, want max health %g", kill.Contributions[0].Amount, w.tuning.MaxHealth)
	}
}

func TestContributionTieGoesToEarliestContributor(t *testing.T) {
	w, now := testWorld()
	target := addTestPlayer(w, "target", model.Mage, Vec{}, now)
	w.damage(target, 50, "early", now)
	w.damage(target, 50, "later", now.Add(time.Millisecond))
	kill, _ := w.LastKill(target.ID)
	if kill.CreditID != "early" {
		t.Fatalf("tie credit = %q, want earliest contributor", kill.CreditID)
	}
}

func TestCombatLogCursorAndRespawnLifecycle(t *testing.T) {
	w, now := testWorld()
	target := addTestPlayer(w, "target", model.Mage, Vec{}, now)
	w.damage(target, w.tuning.MaxHealth, "source", now)
	events := w.CombatEventsAfter(0)
	if len(events) != 2 || events[0].Kind != CombatDamage || events[1].Kind != CombatKill {
		t.Fatalf("events = %#v", events)
	}
	if got := w.CombatEventsAfter(events[0].Sequence); len(got) != 1 || got[0].Kind != CombatKill {
		t.Fatalf("cursor result = %#v", got)
	}
	// Returned events are defensive copies: a downstream consumer cannot
	// corrupt the ownership decision another consumer sees.
	events[1].Contributions[0].Amount = -1
	again, _ := w.LastKill(target.ID)
	if again.Contributions[0].Amount <= 0 {
		t.Fatal("combat-log caller mutated retained kill event")
	}
	if !w.Respawn(target.ID, now.Add(time.Second)) {
		t.Fatal("respawn rejected")
	}
	if len(w.Contributions(target.ID)) != 0 {
		t.Fatal("contribution ledger crossed a life boundary")
	}
	if _, ok := w.LastKill(target.ID); ok {
		t.Fatal("last kill crossed a life boundary")
	}
	if len(w.CombatEventsAfter(0)) != 2 {
		t.Fatal("respawn discarded immutable combat events")
	}
}
