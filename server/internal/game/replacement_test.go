package game

import (
	"errors"
	"testing"
	"time"

	"spellfire/server/internal/model"
)

func TestReplacementKicksOldConnection(t *testing.T) {
	engine := NewEngine(DefaultTuning(), nil)
	character := model.Character{ID: "same-character", Name: "Hero", Class: model.Gunslinger}
	old, _ := engine.Join(character, time.Now())
	_, _ = engine.Join(character, time.Now())
	select {
	case <-old.Kick:
	case <-time.After(time.Second):
		t.Fatal("replacement did not kick the old connection")
	}
}

func accountCharacter(accountID, id string) model.Character {
	return model.Character{ID: id, AccountID: accountID, Name: id, Class: model.Gunslinger}
}

// One account, one body: a second character cannot be brought into the world
// beside the first.
func TestSecondCharacterOnTheSameAccountIsRefused(t *testing.T) {
	engine := NewEngine(DefaultTuning(), nil)
	now := time.Now()
	if _, err := engine.Join(accountCharacter("account", "first"), now); err != nil {
		t.Fatalf("first join: %v", err)
	}
	if _, err := engine.Join(accountCharacter("account", "second"), now); !errors.Is(err, ErrAccountInWorld) {
		t.Fatalf("second join error = %v, want ErrAccountInWorld", err)
	}
	if engine.Present("second") {
		t.Fatal("the refused character was added to the world")
	}
	// A different account is unaffected, and the first character's own
	// reconnect still replaces its connection rather than being refused.
	if _, err := engine.Join(accountCharacter("other", "third"), now); err != nil {
		t.Fatalf("join on another account: %v", err)
	}
	if _, err := engine.Join(accountCharacter("account", "first"), now); err != nil {
		t.Fatalf("reconnect of the same character: %v", err)
	}
}

// The refusal outlasts the connection: a lingering body still occupies the
// account, so switching characters cannot be used to pull a body out of a
// fight. Once the logout window closes, the account is free.
func TestLingeringBodyStillOccupiesTheAccount(t *testing.T) {
	engine := NewEngine(DefaultTuning(), nil)
	now := time.Now()
	client, err := engine.Join(accountCharacter("account", "first"), now)
	if err != nil {
		t.Fatalf("first join: %v", err)
	}
	engine.Leave(client)
	if _, err := engine.Join(accountCharacter("account", "second"), now); !errors.Is(err, ErrAccountInWorld) {
		t.Fatalf("join beside a lingering body = %v, want ErrAccountInWorld", err)
	}

	closed := now.Add(DefaultTuning().LogoutLinger + time.Second)
	engine.mu.Lock()
	engine.reapLingeringLocked(closed)
	engine.mu.Unlock()
	if _, err := engine.Join(accountCharacter("account", "second"), closed); err != nil {
		t.Fatalf("join after the logout window closed: %v", err)
	}
}
