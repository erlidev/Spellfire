package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"spellfire/server/internal/model"
)

type SQLite struct{ db *sql.DB }

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

func (s *SQLite) migrate() error {
	_, err := s.db.Exec(`
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
CREATE INDEX IF NOT EXISTS characters_account_idx ON characters(account_id);`)
	return err
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

func (s *SQLite) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (s *SQLite) Characters(ctx context.Context, accountID string) ([]model.Character, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,account_id,name,class,level,xp FROM characters WHERE account_id=? ORDER BY name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	characters := make([]model.Character, 0)
	for rows.Next() {
		var c model.Character
		if err := rows.Scan(&c.ID, &c.AccountID, &c.Name, &c.Class, &c.Level, &c.XP); err != nil {
			return nil, err
		}
		characters = append(characters, c)
	}
	return characters, rows.Err()
}

func (s *SQLite) CreateCharacter(ctx context.Context, c model.Character) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO characters(id,account_id,name,class,level,xp) VALUES(?,?,?,?,?,?)`, c.ID, c.AccountID, c.Name, c.Class, c.Level, c.XP)
	return classify(err)
}

func (s *SQLite) Character(ctx context.Context, accountID, id string) (model.Character, error) {
	var c model.Character
	err := s.db.QueryRowContext(ctx, `SELECT id,account_id,name,class,level,xp FROM characters WHERE account_id=? AND id=?`, accountID, id).Scan(&c.ID, &c.AccountID, &c.Name, &c.Class, &c.Level, &c.XP)
	return c, classify(err)
}

func (s *SQLite) Close() error { return s.db.Close() }

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
	return fmt.Errorf("store: %w", err)
}
