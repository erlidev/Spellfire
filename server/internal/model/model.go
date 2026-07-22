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
//	4  adds the equipped loadout: weapon, gadget slots, and spell slots
//	5  adds the permanent unlock ledger
const CharacterSchemaVersion = 5

type Account struct {
	ID           string
	Email        string
	PasswordHash []byte
}

type Point struct{ X, Y float64 }

// Loadout is the equipped set: content IDs by slot, never their stats. The
// slices are positional and may hold empty strings for empty slots, because a
// slot's index is what the action bar binds to a key.
//
// Version records the content revision the set was last validated at. When it
// no longer matches the manifest, a balance patch has landed and the character
// is owed the global respec — the set is re-resolved and re-validated on the
// next join rather than silently carrying an arrangement a patch invalidated.
type Loadout struct {
	Weapon    string   `json:"weapon"`
	Gadgets   []string `json:"gadgets"`
	Spells    []string `json:"spells"`
	Keystones []string `json:"keystones"`
	Version   int      `json:"version"`
}

// Empty reports a record that has never had a loadout written — a character
// created before it chose one, which resolves to the class default.
func (l Loadout) Empty() bool {
	return l.Weapon == "" && len(l.Gadgets) == 0 && len(l.Spells) == 0 && len(l.Keystones) == 0
}

// Clone copies the slices so a stored loadout cannot be mutated through a
// reference the world handed out.
func (l Loadout) Clone() Loadout {
	l.Gadgets = append([]string(nil), l.Gadgets...)
	l.Spells = append([]string(nil), l.Spells...)
	l.Keystones = append([]string(nil), l.Keystones...)
	return l
}

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
	// Loadout is the equipped set. An empty one is not an error: it resolves to
	// the class default the first time the character enters the world.
	Loadout Loadout
}

// Progress is the permanent character axis: the level reached, the XP banked
// toward the next one, and the flat unlock ledger. It stores IDs and counts
// only, so a balance patch retunes what a character owns without touching the
// record. It is embedded in Character, and its fields marshal flat.
type Progress struct {
	Level int `json:"level"`
	XP    int `json:"xp"`
	// Unlocks is every permanently owned weapon, spell, and gadget ID, sorted.
	// It is never shortened: content is retired onto a replacement or a refund,
	// never confiscated.
	Unlocks []string `json:"unlocks"`
}

// Clone copies the ledger so a stored progression cannot be mutated through a
// reference the world handed out.
func (p Progress) Clone() Progress {
	p.Unlocks = append([]string(nil), p.Unlocks...)
	return p
}

type Character struct {
	ID        string `json:"id"`
	AccountID string `json:"-"`
	Name      string `json:"name"`
	Class     Class  `json:"class"`
	Progress
	SchemaVersion int            `json:"-"`
	State         CharacterState `json:"-"`
	// Items are the crafted instances the character owns. They live in their own
	// table rather than on the record, so they are loaded alongside it at join
	// rather than carried through the character's schema version.
	Items []CraftedItem `json:"-"`
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
		case 3:
			// v3 predates equipped slots. An empty loadout resolves to the
			// class default on the next join, which is exactly what the
			// character was already fighting with.
			c.State.Loadout = Loadout{}
		case 4:
			// v4 predates the unlock ledger. An empty ledger is rolled into a
			// starter kit and topped up with everything the level already
			// grants, which needs the tuning tables and so happens where the
			// character is used rather than here.
			c.Unlocks = nil
		}
		c.SchemaVersion++
	}
	if c.State.Materials == nil {
		c.State.Materials = map[string]int{}
	}
	return c, nil
}

// CraftedItem is a persisted crafted weapon. It stores the weapon category it
// is an instance of and the component filling each slot — references into the
// tuning tables — and never a stat snapshot, so editing a balance row retunes
// every existing item in place with no character migration. The blueprint is not
// stored because it is the weapon row's, and two copies of one fact drift.
//
// An equipped weapon slot may name either a weapon row, which is the stock
// configuration, or one of these instances.
type CraftedItem struct {
	ID          string
	CharacterID string
	Weapon      string            // weapons row ID
	Components  map[string]string // blueprint slot → components.components row ID
}

// Clone copies the component map so a stored item cannot be mutated through a
// reference the world handed out.
func (i CraftedItem) Clone() CraftedItem {
	components := make(map[string]string, len(i.Components))
	for slot, component := range i.Components {
		components[slot] = component
	}
	i.Components = components
	return i
}
