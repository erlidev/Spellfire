// Package progression owns the character axis: the permanent unlock ledger,
// what XP is worth, and what a level grants. It holds no world state, so the
// simulation, the store, and character creation agree on one answer without
// either owning the rule.
//
// The ledger is flat and permanent by design — gunslinger.md records unlocks as
// a flat ledger earned through level or discovery — and nothing here ever
// removes an entry. Retiring content maps an entry onto its replacement; it
// never confiscates one.
package progression

import (
	"math/rand"
	"sort"

	"spellfire/server/internal/model"
	"spellfire/server/internal/tuning"
)

// Ledger is the set of content a character permanently owns. It is a value:
// granting returns a new ledger rather than mutating a shared one, so a copy
// handed to the world can never write back through the character record.
type Ledger struct {
	owned map[string]bool
	ids   []string
}

// New builds a ledger from persisted IDs, dropping blanks and duplicates.
func New(ids []string) Ledger {
	ledger := Ledger{owned: make(map[string]bool, len(ids)), ids: make([]string, 0, len(ids))}
	for _, id := range ids {
		if id == "" || ledger.owned[id] {
			continue
		}
		ledger.owned[id] = true
		ledger.ids = append(ledger.ids, id)
	}
	sort.Strings(ledger.ids)
	return ledger
}

// Has reports ownership. An empty ID is never owned, which is what makes an
// empty slot legal without a special case at every call site.
func (l Ledger) Has(id string) bool { return id != "" && l.owned[id] }

// IDs is the ledger in stable order, safe for the caller to keep.
func (l Ledger) IDs() []string { return append([]string(nil), l.ids...) }

func (l Ledger) Len() int { return len(l.ids) }

// With returns the ledger extended by whatever it does not already hold, and
// the entries that were actually new.
func (l Ledger) With(ids ...string) (Ledger, []string) {
	added := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || l.owned[id] {
			continue
		}
		added = append(added, id)
	}
	if len(added) == 0 {
		return l, nil
	}
	sort.Strings(added)
	return New(append(l.IDs(), added...)), added
}

// StarterKit is the draw a character receives on creation: one weapon from the
// basic set of its class, plus a random selection of the low-tier content its
// slot kind uses. The draw is seeded from the character's own ID, so it is
// stable — the same character re-rolled by a migration gets the same kit — while
// two characters get different opening tools.
//
// Nothing drawn is exclusive: every row in the pool also carries an
// unlock_level, so a bad draw is a starting flavour rather than a permanent gap.
func StarterKit(tables *tuning.Tables, class model.Class, characterID string) []string {
	draw := rand.New(rand.NewSource(int64(hash(characterID + ":" + string(class)))))
	kit := make([]string, 0, tables.Progression.StarterKit.Unlocks+1)
	if weapons := tables.StarterWeapons(string(class)); len(weapons) > 0 {
		kit = append(kit, weapons[draw.Intn(len(weapons))])
	}
	pool := tables.StarterSpells()
	if class == model.Gunslinger {
		pool = tables.StarterGadgets(string(class))
	}
	// Fisher-Yates over a copy: the pool is drawn without replacement, so a
	// small pool yields all of itself rather than repeating one row.
	pool = append([]string(nil), pool...)
	draw.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if count := tables.Progression.StarterKit.Unlocks; count < len(pool) {
		pool = pool[:count]
	}
	return New(append(kit, pool...)).IDs()
}

// Sync carries a saved ledger onto today's content and onto the level the
// character has already reached. Retired entries follow their retirement, an
// entry this build has never heard of is dropped, and everything the level
// grants is added — which is what lets content be added at a level a character
// is already past. A character with no ledger at all is one created before the
// ledger existed, or one that has just been created, and is given its kit.
//
// It reports whether the result differs from what was saved, so the caller can
// persist a ledger that changed without writing one that did not.
func Sync(tables *tuning.Tables, class model.Class, characterID string, level int, saved []string) (Ledger, bool) {
	resolved := make([]string, 0, len(saved))
	for _, id := range saved {
		resolution, ok := tables.ResolveUnlock(id)
		if !ok || resolution.ID == "" {
			// A refunded retirement leaves no content to own. The refund itself
			// is owed against the material ledger, not this one.
			continue
		}
		resolved = append(resolved, resolution.ID)
	}
	if len(resolved) == 0 {
		resolved = StarterKit(tables, class, characterID)
	}
	ledger, added := New(resolved).With(tables.UnlocksThrough(level)...)
	return ledger, len(added) > 0 || !sameIDs(saved, ledger.ids)
}

// Advance adds XP and reports the level and remaining XP it leaves the
// character at, along with everything the levels crossed granted. Surplus XP
// carries into the next level rather than being discarded, and the cap absorbs
// the rest: XP past the cap buys nothing, which is the compressed power band
// working as intended.
func Advance(tables *tuning.Tables, level, xp, award int) (int, int, []string) {
	table := tables.Progression
	if level < 1 {
		level = 1
	}
	if award > 0 {
		xp += award
	}
	granted := make([]string, 0)
	for {
		cost := table.XPToNext(level)
		if cost <= 0 {
			// At the cap XP stops accumulating, so a capped character's record
			// does not drift upward forever with a number nothing reads.
			return level, 0, granted
		}
		if xp < cost {
			return level, xp, granted
		}
		xp -= cost
		level++
		granted = append(granted, unlocksAt(tables, level)...)
	}
}

// unlocksAt is what arriving at exactly this level grants.
func unlocksAt(tables *tuning.Tables, level int) []string {
	previous := New(tables.UnlocksThrough(level - 1))
	granted := make([]string, 0)
	for _, id := range tables.UnlocksThrough(level) {
		if !previous.Has(id) {
			granted = append(granted, id)
		}
	}
	return granted
}

// Progress is the persisted character axis: level, XP toward the next level,
// and the ledger as stored IDs.
func Progress(level, xp int, ledger Ledger) model.Progress {
	return model.Progress{Level: level, XP: xp, Unlocks: ledger.IDs()}
}

func sameIDs(saved []string, current []string) bool {
	if len(saved) != len(current) {
		return false
	}
	for index, id := range current {
		if saved[index] != id {
			return false
		}
	}
	return true
}

// hash is FNV-1a over the seed string, so a character's draw depends only on
// its own ID and never on process start or map order.
func hash(value string) uint64 {
	var h uint64 = 1469598103934665603
	for i := range value {
		h ^= uint64(value[i])
		h *= 1099511628211
	}
	return h
}
