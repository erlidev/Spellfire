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
	AccountBySession(context.Context, string, time.Time) (model.Account, error)
	DeleteSession(context.Context, string) error
	Characters(context.Context, string) ([]model.Character, error)
	CreateCharacter(context.Context, model.Character) error
	Character(context.Context, string, string) (model.Character, error)
	// SaveCharacterState persists the world state a character keeps across a
	// disconnect: last position, carried materials, and unlocked outposts.
	SaveCharacterState(context.Context, string, model.CharacterState) error
	// SaveCharacterProgress persists the permanent character axis: level, XP,
	// and the unlock ledger. It is a deliberate commit, separate from the
	// incidental world state above.
	SaveCharacterProgress(context.Context, string, model.Progress) error
	// CraftedItems and CreateCraftedItem store owned items as blueprint and
	// component references. Nothing here records a computed stat.
	CraftedItems(context.Context, string) ([]model.CraftedItem, error)
	CreateCraftedItem(context.Context, model.CraftedItem) error
	Close() error
}
