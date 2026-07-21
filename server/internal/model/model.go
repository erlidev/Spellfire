package model

import (
	"fmt"
	"time"
)

type Class string

const (
	Gunslinger Class = "gunslinger"
	Mage       Class = "mage"
)

func (c Class) Valid() bool { return c == Gunslinger || c == Mage }

// CharacterSchemaVersion is the record shape this build writes. A row stored at
// a lower version is carried forward by Migrate before any caller sees it, so
// the rest of the server only ever handles the current shape.
//
//	1  name, class, level, and xp
//	2  adds saved world position, carried materials, and unlocked outposts
//	3  adds the last-seen stamp that decides whether the position still holds
const CharacterSchemaVersion = 3

type Account struct {
	ID           string
	Email        string
	PasswordHash []byte
}

type Point struct{ X, Y float64 }

// CharacterState is the world state a character keeps across a disconnect. It
// holds references and counts only — never derived combat values — so a tuning
// edit retunes a returning character without touching the record.
type CharacterState struct {
	// Position is meaningful only when Placed is set. An unplaced character
	// enters the world at the hub spawn.
	Position Point
	Placed   bool
	// LastSeen is when the position was written. A zero stamp means the record
	// predates the stamp, which counts as expired: the position could be
	// arbitrarily old and there is no way to tell.
	LastSeen  time.Time
	Materials map[string]int // carried raw material ID → count
	Outposts  []string       // unlocked outpost IDs, sorted
}

type Character struct {
	ID            string         `json:"id"`
	AccountID     string         `json:"-"`
	Name          string         `json:"name"`
	Class         Class          `json:"class"`
	Level         int            `json:"level"`
	XP            int            `json:"xp"`
	SchemaVersion int            `json:"-"`
	State         CharacterState `json:"-"`
}

// Migrate carries a record read at an older schema version forward to
// CharacterSchemaVersion. Steps are sequential and additive: each fills what the
// newer shape expects and none discards a field. A record from a newer build is
// an error rather than a silent downgrade, because writing it back would
// truncate whatever that build stored.
func (c Character) Migrate() (Character, error) {
	if c.SchemaVersion > CharacterSchemaVersion {
		return c, fmt.Errorf("model: character %s is at record schema version %d, newer than this build's %d", c.ID, c.SchemaVersion, CharacterSchemaVersion)
	}
	for c.SchemaVersion < CharacterSchemaVersion {
		switch c.SchemaVersion {
		case 1:
			// v1 predates persisted world state. The character has no saved
			// position, so it enters at the hub spawn, and carries nothing.
			c.State = CharacterState{}
		case 2:
			// v2 saved a position but not when. An undatable position cannot
			// be trusted to be recent, so it is left unstamped and expires.
			c.State.LastSeen = time.Time{}
		}
		c.SchemaVersion++
	}
	if c.State.Materials == nil {
		c.State.Materials = map[string]int{}
	}
	return c, nil
}

// CraftedItem is a persisted crafted weapon. It stores the blueprint and the
// component filling each slot — references into the tuning tables — and never a
// stat snapshot, so editing a balance row retunes every existing item in place
// with no character migration.
type CraftedItem struct {
	ID          string
	CharacterID string
	Blueprint   string            // components.blueprints row ID
	Components  map[string]string // blueprint slot → components.components row ID
}
