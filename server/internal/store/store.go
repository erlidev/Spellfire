package store

import (
	"context"
	"errors"
	"time"

	"spellfire/server/internal/model"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Store interface {
	CreateAccount(context.Context, model.Account) error
	AccountByEmail(context.Context, string) (model.Account, error)
	CreateSession(context.Context, string, string, time.Time) error
	AccountIDBySession(context.Context, string, time.Time) (string, error)
	DeleteSession(context.Context, string) error
	Characters(context.Context, string) ([]model.Character, error)
	CreateCharacter(context.Context, model.Character) error
	Character(context.Context, string, string) (model.Character, error)
	Close() error
}
