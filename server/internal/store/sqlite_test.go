package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"spellfire/server/internal/model"
)

func TestSQLiteAccountSessionAndCharacterLifecycle(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	a := model.Account{ID: "a1", Email: "PLAYER@example.com", PasswordHash: []byte("hash")}
	if err := s.CreateAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAccount(ctx, model.Account{ID: "a2", Email: "player@example.com", PasswordHash: []byte("hash")}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate email error = %v", err)
	}
	loaded, err := s.AccountByEmail(ctx, "player@EXAMPLE.com")
	if err != nil || loaded.ID != a.ID {
		t.Fatalf("account = %#v, %v", loaded, err)
	}
	if err := s.CreateSession(ctx, "active", a.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.AccountIDBySession(ctx, "active", time.Now()); err != nil || got != a.ID {
		t.Fatalf("session = %q, %v", got, err)
	}
	if err := s.CreateSession(ctx, "expired", a.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountIDBySession(ctx, "expired", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired error = %v", err)
	}
	c := model.Character{ID: "c1", AccountID: a.ID, Name: "Ember Fox", Class: model.Mage, Level: 1}
	if err := s.CreateCharacter(ctx, c); err != nil {
		t.Fatal(err)
	}
	characters, err := s.Characters(ctx, a.ID)
	if err != nil || len(characters) != 1 || characters[0].Class != model.Mage {
		t.Fatalf("characters = %#v, %v", characters, err)
	}
	if _, err := s.Character(ctx, "another-account", c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account read error = %v", err)
	}
	if err := s.DeleteSession(ctx, "active"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountIDBySession(ctx, "active", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted session error = %v", err)
	}
}

func TestSQLiteEnforcesCharacterClass(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateAccount(ctx, model.Account{ID: "a", Email: "a@example.com", PasswordHash: []byte("hash")}); err != nil {
		t.Fatal(err)
	}
	err = s.CreateCharacter(ctx, model.Character{ID: "c", AccountID: "a", Name: "Broken One", Class: model.Class("paladin"), Level: 1})
	if err == nil {
		t.Fatal("invalid class was accepted")
	}
}
