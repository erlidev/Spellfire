package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"spellfire/server/internal/model"
)

type SQLite struct{ db *sql.DB }

// migrations run in order and are never edited once shipped: index+1 is the
// PRAGMA user_version each one leaves behind, and a database only ever moves
// forward through the ones it has not seen. Changing the schema means appending
// an entry, never rewriting an earlier one.
var migrations = []string{
	// 1 — accounts, sessions, and characters.
	`
CREATE TABLE IF NOT EXISTS accounts (
  id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, password_hash BLOB NOT NULL, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY, account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS characters (
  id TEXT PRIMARY KEY, account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name TEXT NOT NULL, class TEXT NOT NULL CHECK(class IN ('gunslinger','mage')),
  level INTEGER NOT NULL DEFAULT 1, xp INTEGER NOT NULL DEFAULT 0,
  schema_version INTEGER NOT NULL DEFAULT 1, UNIQUE(account_id, name)
);
CREATE INDEX IF NOT EXISTS sessions_account_idx ON sessions(account_id);
CREATE INDEX IF NOT EXISTS characters_account_idx ON characters(account_id);`,

	// 2 — persisted world state. Position is nullable because "never placed"
	// and "placed at the origin" are different states. Crafted items live in
	// their own table so the Phase 2 loadout can reference one by ID.
	`
ALTER TABLE characters ADD COLUMN pos_x REAL;
ALTER TABLE characters ADD COLUMN pos_y REAL;
ALTER TABLE characters ADD COLUMN materials TEXT NOT NULL DEFAULT '{}';
ALTER TABLE characters ADD COLUMN outposts TEXT NOT NULL DEFAULT '[]';
CREATE TABLE IF NOT EXISTS crafted_items (
  id TEXT PRIMARY KEY, character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
  blueprint TEXT NOT NULL, components TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS crafted_items_character_idx ON crafted_items(character_id);`,

	// 3 — when the saved position was written. Nullable: a row migrated from
	// version 2 has a position of unknown age, which is treated as expired.
	`ALTER TABLE characters ADD COLUMN last_seen_at INTEGER;`,
}

const characterColumns = `id,account_id,name,class,level,xp,schema_version,pos_x,pos_y,materials,outposts,last_seen_at`

func OpenSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &SQLite{db: db}
	if _, err = db.Exec(`PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// migrate reads the database's own schema version and applies every forward
// migration it has not seen. A database written by a newer build is refused
// rather than downgraded.
func (s *SQLite) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("store: database is at schema version %d, newer than this build's %d; run the newer binary", version, len(migrations))
	}
	for index := version; index < len(migrations); index++ {
		if err := s.applyMigration(index); err != nil {
			return fmt.Errorf("store: migration %d: %w", index+1, err)
		}
	}
	return nil
}

// applyMigration runs one migration and its version bump in a single
// transaction, so an interrupted upgrade never leaves a half-migrated schema
// recorded as complete.
func (s *SQLite) applyMigration(index int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(migrations[index]); err != nil {
		return err
	}
	// PRAGMA does not accept a bound parameter; the value is a loop index.
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, index+1)); err != nil {
		return err
	}
	return tx.Commit()
}

// SchemaVersion reports the migration the database has been brought up to.
func (s *SQLite) SchemaVersion() (int, error) {
	var version int
	err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version)
	return version, err
}

func (s *SQLite) CreateAccount(ctx context.Context, a model.Account) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO accounts(id,email,password_hash,created_at) VALUES(?,?,?,?)`, a.ID, strings.ToLower(a.Email), a.PasswordHash, time.Now().Unix())
	return classify(err)
}

func (s *SQLite) AccountByEmail(ctx context.Context, email string) (model.Account, error) {
	var a model.Account
	err := s.db.QueryRowContext(ctx, `SELECT id,email,password_hash FROM accounts WHERE email=?`, strings.ToLower(email)).Scan(&a.ID, &a.Email, &a.PasswordHash)
	return a, classify(err)
}

func (s *SQLite) CreateSession(ctx context.Context, tokenHash, accountID string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash,account_id,expires_at) VALUES(?,?,?)`, tokenHash, accountID, expires.Unix())
	return classify(err)
}

func (s *SQLite) AccountIDBySession(ctx context.Context, tokenHash string, now time.Time) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT account_id FROM sessions WHERE token_hash=? AND expires_at>?`, tokenHash, now.Unix()).Scan(&id)
	return id, classify(err)
}

func (s *SQLite) AccountBySession(ctx context.Context, tokenHash string, now time.Time) (model.Account, error) {
	var account model.Account
	err := s.db.QueryRowContext(ctx, `
SELECT accounts.id,accounts.email
FROM sessions JOIN accounts ON accounts.id=sessions.account_id
WHERE sessions.token_hash=? AND sessions.expires_at>?`, tokenHash, now.Unix()).
		Scan(&account.ID, &account.Email)
	return account, classify(err)
}

func (s *SQLite) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (s *SQLite) Characters(ctx context.Context, accountID string) ([]model.Character, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+characterColumns+` FROM characters WHERE account_id=? ORDER BY name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	characters := make([]model.Character, 0)
	for rows.Next() {
		c, err := scanCharacter(rows.Scan)
		if err != nil {
			return nil, err
		}
		characters = append(characters, c)
	}
	return characters, rows.Err()
}

func (s *SQLite) CreateCharacter(ctx context.Context, c model.Character) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO characters(id,account_id,name,class,level,xp,schema_version,materials,outposts) VALUES(?,?,?,?,?,?,?,'{}','[]')`,
		c.ID, c.AccountID, c.Name, c.Class, c.Level, c.XP, model.CharacterSchemaVersion)
	return classify(err)
}

func (s *SQLite) Character(ctx context.Context, accountID, id string) (model.Character, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+characterColumns+` FROM characters WHERE account_id=? AND id=?`, accountID, id)
	c, err := scanCharacter(row.Scan)
	return c, classify(err)
}

// SaveCharacterState writes back the world state a character keeps across a
// disconnect and stamps the record at the current shape, which is what completes
// a forward record migration on disk.
func (s *SQLite) SaveCharacterState(ctx context.Context, id string, state model.CharacterState) error {
	materials, err := json.Marshal(carriedMaterials(state.Materials))
	if err != nil {
		return fmt.Errorf("store: encode carried materials: %w", err)
	}
	outposts, err := json.Marshal(unlockedOutposts(state.Outposts))
	if err != nil {
		return fmt.Errorf("store: encode unlocked outposts: %w", err)
	}
	var x, y, lastSeen any
	if state.Placed {
		x, y = state.Position.X, state.Position.Y
	}
	if !state.LastSeen.IsZero() {
		lastSeen = state.LastSeen.Unix()
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE characters SET pos_x=?,pos_y=?,materials=?,outposts=?,last_seen_at=?,schema_version=? WHERE id=?`,
		x, y, string(materials), string(outposts), lastSeen, model.CharacterSchemaVersion, id)
	if err != nil {
		return classify(err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) CraftedItems(ctx context.Context, characterID string) ([]model.CraftedItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,character_id,blueprint,components FROM crafted_items WHERE character_id=? ORDER BY id`, characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.CraftedItem, 0)
	for rows.Next() {
		var item model.CraftedItem
		var components string
		if err := rows.Scan(&item.ID, &item.CharacterID, &item.Blueprint, &components); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(components), &item.Components); err != nil {
			return nil, fmt.Errorf("store: crafted item %s: decode components: %w", item.ID, err)
		}
		if item.Components == nil {
			item.Components = map[string]string{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLite) CreateCraftedItem(ctx context.Context, item model.CraftedItem) error {
	components, err := json.Marshal(nonEmpty(item.Components))
	if err != nil {
		return fmt.Errorf("store: encode components: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO crafted_items(id,character_id,blueprint,components) VALUES(?,?,?,?)`,
		item.ID, item.CharacterID, item.Blueprint, string(components))
	return classify(err)
}

func (s *SQLite) Close() error { return s.db.Close() }

// scanCharacter reads one row and brings it forward to the current record
// shape, so nothing above the store ever sees an older version.
func scanCharacter(scan func(...any) error) (model.Character, error) {
	var c model.Character
	var x, y sql.NullFloat64
	var lastSeen sql.NullInt64
	var materials, outposts string
	if err := scan(&c.ID, &c.AccountID, &c.Name, &c.Class, &c.Level, &c.XP, &c.SchemaVersion, &x, &y, &materials, &outposts, &lastSeen); err != nil {
		return c, err
	}
	c.State.Placed = x.Valid && y.Valid
	c.State.Position = model.Point{X: x.Float64, Y: y.Float64}
	if lastSeen.Valid {
		c.State.LastSeen = time.Unix(lastSeen.Int64, 0)
	}
	if err := json.Unmarshal([]byte(materials), &c.State.Materials); err != nil {
		return c, fmt.Errorf("store: character %s: decode carried materials: %w", c.ID, err)
	}
	if err := json.Unmarshal([]byte(outposts), &c.State.Outposts); err != nil {
		return c, fmt.Errorf("store: character %s: decode unlocked outposts: %w", c.ID, err)
	}
	return c.Migrate()
}

// carriedMaterials drops empty and negative counts so a save never records a
// stack the character does not hold.
func carriedMaterials(materials map[string]int) map[string]int {
	carried := make(map[string]int, len(materials))
	for id, count := range materials {
		if count > 0 {
			carried[id] = count
		}
	}
	return carried
}

// unlockedOutposts normalises the unlock list to a sorted, deduplicated set so
// the stored blob is stable regardless of discovery order.
func unlockedOutposts(outposts []string) []string {
	seen := map[string]bool{}
	unlocked := make([]string, 0, len(outposts))
	for _, id := range outposts {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unlocked = append(unlocked, id)
	}
	sort.Strings(unlocked)
	return unlocked
}

func nonEmpty(components map[string]string) map[string]string {
	if components == nil {
		return map[string]string{}
	}
	return components
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrConflict
	}
	if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		return ErrNotFound
	}
	return fmt.Errorf("store: %w", err)
}
