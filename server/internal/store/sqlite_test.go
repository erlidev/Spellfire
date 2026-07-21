package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
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
	if got, err := s.AccountBySession(ctx, "active", time.Now()); err != nil || got.ID != a.ID || got.Email != "player@example.com" {
		t.Fatalf("session account = %#v, %v", got, err)
	}
	if err := s.CreateSession(ctx, "expired", a.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountIDBySession(ctx, "expired", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired error = %v", err)
	}
	if _, err := s.AccountBySession(ctx, "expired", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired account error = %v", err)
	}
	c := model.Character{ID: "c1", AccountID: a.ID, Name: "Ember Fox", Class: model.Mage, Progress: model.Progress{Level: 1}}
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
	err = s.CreateCharacter(ctx, model.Character{ID: "c", AccountID: "a", Name: "Broken One", Class: model.Class("paladin"), Progress: model.Progress{Level: 1}})
	if err == nil {
		t.Fatal("invalid class was accepted")
	}
}

// A pre-1.2 database has no user_version and no world-state columns. Opening it
// with this build must migrate it forward in place, without touching the rows.
func TestSQLiteMigratesAnExistingDatabaseForward(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(migrations[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO accounts(id,email,password_hash,created_at) VALUES('a','a@example.com',x'00',0);
INSERT INTO characters(id,account_id,name,class,level,xp) VALUES('c','a','Vance','gunslinger',3,120)`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if version, err := s.SchemaVersion(); err != nil || version != len(migrations) {
		t.Fatalf("schema version = %d, %v; want %d", version, err, len(migrations))
	}
	c, err := s.Character(context.Background(), "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if c.Level != 3 || c.XP != 120 {
		t.Fatalf("migration lost progression: %#v", c)
	}
	// The v1 record carried no world state, so the migrated record must offer
	// the empty one rather than a position at the origin.
	if c.SchemaVersion != model.CharacterSchemaVersion || c.State.Placed || len(c.State.Materials) != 0 || len(c.State.Outposts) != 0 {
		t.Fatalf("record migration = %#v", c)
	}
	// A record that predates equipped slots migrates to an empty loadout, which
	// resolves to the class default on the next join rather than to nothing.
	if !c.State.Loadout.Empty() {
		t.Fatalf("migrated loadout = %#v, want the empty one", c.State.Loadout)
	}
	if !c.State.LastSeen.IsZero() {
		t.Fatalf("last seen = %v, want the unstamped zero that expires", c.State.LastSeen)
	}
	// A record that predates the unlock ledger migrates to an empty one, which
	// is rolled into a starter kit the first time the character is used rather
	// than leaving it unable to equip anything.
	if len(c.Unlocks) != 0 {
		t.Fatalf("migrated ledger = %#v, want the empty one", c.Unlocks)
	}
}

// The permanent axis is written through its own statement, so a world-state
// save from a body that entered before a grant cannot roll the grant back.
func TestSQLitePersistsProgressIndependentlyOfWorldState(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "progress.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	if err := s.CreateAccount(ctx, model.Account{ID: "a", Email: "a@example.com", PasswordHash: []byte("hash")}); err != nil {
		t.Fatal(err)
	}
	created := model.Character{
		ID: "c", AccountID: "a", Name: "Ilse", Class: model.Mage,
		Progress: model.Progress{Level: 1, Unlocks: []string{"starter-staff", "fire-bolt", "starter-staff"}},
	}
	if err := s.CreateCharacter(ctx, created); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Character(ctx, "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	// The stored ledger is a sorted set regardless of the order it was granted.
	if !reflect.DeepEqual(loaded.Unlocks, []string{"fire-bolt", "starter-staff"}) {
		t.Fatalf("created ledger = %#v", loaded.Unlocks)
	}

	if err := s.SaveCharacterProgress(ctx, "c", model.Progress{Level: 4, XP: 37, Unlocks: []string{"fire-bolt", "starter-staff", "fire-2"}}); err != nil {
		t.Fatal(err)
	}
	// A world-state save written afterwards touches no progression column.
	if err := s.SaveCharacterState(ctx, "c", model.CharacterState{Placed: true, LastSeen: time.Now()}); err != nil {
		t.Fatal(err)
	}
	loaded, err = s.Character(ctx, "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Level != 4 || loaded.XP != 37 {
		t.Fatalf("progression = level %d, xp %d", loaded.Level, loaded.XP)
	}
	if !reflect.DeepEqual(loaded.Unlocks, []string{"fire-2", "fire-bolt", "starter-staff"}) {
		t.Fatalf("saved ledger = %#v", loaded.Unlocks)
	}
	if err := s.SaveCharacterProgress(ctx, "missing", model.Progress{Level: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("saving progress for an unknown character = %v", err)
	}
}

func TestSQLiteRefusesADatabaseFromANewerBuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	future, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := future.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations)+1)); err != nil {
		t.Fatal(err)
	}
	if err := future.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSQLite(path); err == nil || !strings.Contains(err.Error(), "newer than this build") {
		t.Fatalf("open error = %v", err)
	}
}

func TestSQLitePersistsCharacterStateAndCraftedItems(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	if err := s.CreateAccount(ctx, model.Account{ID: "a", Email: "a@example.com", PasswordHash: []byte("hash")}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(ctx, model.Character{ID: "c", AccountID: "a", Name: "Ilse", Class: model.Mage, Progress: model.Progress{Level: 1}}); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.Character(ctx, "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.SchemaVersion != model.CharacterSchemaVersion || fresh.State.Placed {
		t.Fatalf("new character = %#v", fresh)
	}

	lastSeen := time.Now().Truncate(time.Second)
	state := model.CharacterState{
		Position: model.Point{X: -1420.5, Y: 880.25}, Placed: true, LastSeen: lastSeen,
		Materials: map[string]int{"iron": 7, "spent": 0}, Outposts: []string{"ridge", "ford", "ridge"},
		Loadout: model.Loadout{Weapon: "starter-staff", Spells: []string{"", "fire-bolt", ""}, Version: 4},
	}
	if err := s.SaveCharacterState(ctx, "c", state); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Character(ctx, "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.State.Placed || loaded.State.Position != state.Position {
		t.Fatalf("position = %#v", loaded.State)
	}
	// The stamp is what decides whether the position is still honoured.
	if !loaded.State.LastSeen.Equal(lastSeen) {
		t.Fatalf("last seen = %v, want %v", loaded.State.LastSeen, lastSeen)
	}
	// A zero count is not a held stack, and unlocks normalise to a sorted set.
	if !reflect.DeepEqual(loaded.State.Materials, map[string]int{"iron": 7}) {
		t.Fatalf("materials = %#v", loaded.State.Materials)
	}
	if !reflect.DeepEqual(loaded.State.Outposts, []string{"ford", "ridge"}) {
		t.Fatalf("outposts = %#v", loaded.State.Outposts)
	}
	// The equipped set round-trips as references by slot, empty slots included:
	// a slot's index is its binding, so losing one would rebind the rest.
	if !reflect.DeepEqual(loaded.State.Loadout, state.Loadout) {
		t.Fatalf("loadout = %#v, want %#v", loaded.State.Loadout, state.Loadout)
	}
	if err := s.SaveCharacterState(ctx, "missing", state); !errors.Is(err, ErrNotFound) {
		t.Fatalf("save for an unknown character = %v", err)
	}

	item := model.CraftedItem{ID: "i1", CharacterID: "c", Weapon: "starter-staff", Components: map[string]string{"core": "core_ember", "focus": "focus_wide"}}
	if err := s.CreateCraftedItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCraftedItem(ctx, model.CraftedItem{ID: "i2", CharacterID: "ghost", Weapon: "starter-staff"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("crafted item for an unknown character = %v", err)
	}
	items, err := s.CraftedItems(ctx, "c")
	if err != nil || len(items) != 1 {
		t.Fatalf("crafted items = %#v, %v", items, err)
	}
	if !reflect.DeepEqual(items[0], item) {
		t.Fatalf("crafted item = %#v", items[0])
	}
	// The persisted item is references only: its stored columns hold the
	// blueprint and component IDs and nothing derived from them.
	var columns int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('crafted_items')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 4 {
		t.Fatalf("crafted_items has %d columns; a stat snapshot has crept into the record", columns)
	}
}
