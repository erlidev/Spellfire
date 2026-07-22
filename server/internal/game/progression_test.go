package game

import (
	"testing"
	"time"

	"spellfire/server/internal/loadout"
	"spellfire/server/internal/model"
	"spellfire/server/internal/tuning"
)

// XP goes to whoever the combat log credits — most damage dealt — not to
// whoever landed the last hit, so the same rule that owns a kill owns its
// progression.
func TestKillXPFollowsDamageCreditRatherThanTheLastHit(t *testing.T) {
	w, now := testWorld()
	heavy := addTestPlayer(w, "heavy", model.Gunslinger, Vec{}, now)
	finisher := addTestPlayer(w, "finisher", model.Gunslinger, Vec{}, now)
	target := addTestPlayer(w, "target", model.Mage, Vec{}, now)

	// Joining is itself a grant — the starter kit — so the marks are cleared
	// first and what remains is what the kill produced.
	w.DrainProgress()
	w.damage(target, 80, heavy.ID, now)
	w.damage(target, target.Health, finisher.ID, now)
	if target.Alive {
		t.Fatal("the target survived a lethal hit")
	}
	award := w.tuning.Tables.Progression.Award(tuning.SourcePlayerKill)
	if heavy.XP != award {
		t.Fatalf("credited killer XP = %d, want the %d a player kill is worth", heavy.XP, award)
	}
	if finisher.XP != 0 {
		t.Fatalf("the last hit earned %d XP; credit is by damage dealt", finisher.XP)
	}
	if !heavy.ProgressDirty {
		t.Fatal("an XP award left nothing for the engine to persist")
	}
}

// Levelling is the only thing that grants content, and what it grants must
// widen the equippable set — access, never a bigger number.
func TestLevellingGrantsUnlocksAndWidensTheEquippableSet(t *testing.T) {
	w, now := testWorld()
	killer := addTestPlayer(w, "killer", model.Mage, Vec{}, now)
	// A fresh Mage owns only its drawn kit, so the level-2 grant is observable.
	before := len(loadout.Equippable(w.tuning.Tables, model.Mage, w.inventory(killer), loadout.KindSpell))
	cost := w.tuning.Tables.Progression.XPToNext(1)
	award := w.tuning.Tables.Progression.Award(tuning.SourcePlayerKill)
	for earned := 0; earned <= cost; earned += award {
		target := addTestPlayer(w, "victim", model.Gunslinger, Vec{}, now)
		w.damage(target, target.Health, killer.ID, now)
		w.RemovePlayer(target.ID)
	}
	if killer.Level < 2 {
		t.Fatalf("level = %d after banking more than the %d XP level 1 costs", killer.Level, cost)
	}
	granted := w.tuning.Tables.UnlocksThrough(killer.Level)
	for _, id := range granted {
		if !killer.Unlocks.Has(id) {
			t.Fatalf("level %d did not grant %q", killer.Level, id)
		}
	}
	if after := len(loadout.Equippable(w.tuning.Tables, model.Mage, w.inventory(killer), loadout.KindSpell)); after < before {
		t.Fatalf("equippable spells shrank from %d to %d on levelling", before, after)
	}
}

// Developer fixtures are not an economy: farming an admin-spawned body must not
// move a real character's permanent progression.
func TestAdminFixturesAwardNoProgression(t *testing.T) {
	w, now := testWorld()
	killer := addTestPlayer(w, "killer", model.Gunslinger, Vec{}, now)
	fixture := addTestPlayer(w, "fixture", model.Gunslinger, Vec{}, now)
	fixture.AdminSpawned = true
	w.DrainProgress()
	w.damage(fixture, fixture.Health, killer.ID, now)
	if killer.XP != 0 || killer.ProgressDirty {
		t.Fatalf("killing a developer fixture earned %d XP", killer.XP)
	}
}

// A grant is a deliberate commit: it is persisted as soon as the engine drains
// it, not left to the autosave clock, and it is drained exactly once.
func TestTheEngineDrainsAndPersistsProgressionChanges(t *testing.T) {
	store := newRecorder()
	engine := NewEngine(DefaultTuning(), store)
	character := placed("p", 100, 0)
	if _, err := engine.Join(character, time.Now()); err != nil {
		t.Fatalf("join: %v", err)
	}
	engine.mu.Lock()
	// A record with no ledger is given a starter kit at join, which is itself a
	// grant the record does not yet hold.
	pending := engine.drainProgressLocked()
	engine.mu.Unlock()
	if len(pending) != 1 || pending[0].progress == nil {
		t.Fatalf("join produced %d progression writes, want the starter kit grant", len(pending))
	}
	if len(pending[0].progress.Unlocks) == 0 {
		t.Fatal("the persisted grant carries no unlocks")
	}
	engine.write(pending[0])
	progress, ok := store.savedProgress("p")
	if !ok {
		t.Fatal("the progression grant was never persisted")
	}
	if progress.Level != 1 || len(progress.Unlocks) == 0 {
		t.Fatalf("persisted progress = %+v", progress)
	}
	// A drained mark is cleared, so an unchanged body is not rewritten on every
	// tick for the rest of its session.
	engine.mu.Lock()
	again := engine.drainProgressLocked()
	engine.mu.Unlock()
	if len(again) != 0 {
		t.Fatalf("an unchanged body was drained again: %d writes", len(again))
	}
	// World state and progression are separate statements: the progression
	// write must not carry a state write with it.
	if _, wrote := store.saved("p"); wrote {
		t.Fatal("a progression write also wrote world state")
	}
}

// The developer level grant is the only way above the opening kit until mob XP
// lands, so it has to grant exactly what the level unlocks, stay inside the
// bound the table declares, and never take an unlock back.
func TestGrantProgressUnlocksWhatTheLevelHoldsAndIsBounded(t *testing.T) {
	w, now := testWorld()
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{}, now)
	table := w.tuning.Tables.Progression

	progress, err := w.GrantProgress(p.ID, table.MaxLevel)
	if err != nil {
		t.Fatalf("a grant at the cap was refused: %v", err)
	}
	if progress.Level != table.MaxLevel || !p.ProgressDirty {
		t.Fatalf("level %d, dirty %v: the grant did not reach the persistence path", progress.Level, p.ProgressDirty)
	}
	for _, id := range w.tuning.Tables.UnlocksThrough(table.MaxLevel) {
		if !p.Unlocks.Has(id) {
			t.Fatalf("the cap did not grant %q", id)
		}
	}
	owned := p.Unlocks.Len()
	if _, err := w.GrantProgress(p.ID, 1); err != nil {
		t.Fatalf("dropping back to level 1 was refused: %v", err)
	}
	if p.Unlocks.Len() != owned {
		t.Fatalf("dropping a level confiscated %d unlocks; the ledger is permanent", owned-p.Unlocks.Len())
	}
	if _, err := w.GrantProgress(p.ID, table.MaxLevel+1); err == nil {
		t.Fatal("a level past the table's own cap was accepted")
	}
	if _, err := w.GrantProgress("nobody", 2); err == nil {
		t.Fatal("a grant to a body that is not in the world was accepted")
	}
}
