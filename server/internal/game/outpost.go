package game

import (
	"sort"
	"time"

	"spellfire/server/internal/tuning"
)

// Outposts are the discoverable safe fixtures that overlay the radial world
// field. The field still answers danger, biome, and grade by radius, but safety
// is no longer a radius comparison against the origin: every outpost carries a
// no-PvP bubble and a service set, and the zone vocabulary resolves against the
// nearest one. Keeping this an overlay leaves the bit-identical Go/TS field and
// its coverage check untouched — a bubble is a simulation rule, not geography.

// orderedOutposts is the outpost table as one ID-ordered slice, built once when
// the world is created. Protection is asked on every hostile hit test and on
// every body every tick, and terrain generation asks it per candidate site, so
// none of those may pay for a map iteration and a sort.
func orderedOutposts(rows map[string]tuning.Outpost) []tuning.Outpost {
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ordered := make([]tuning.Outpost, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, rows[id])
	}
	return ordered
}

// outpostAt returns the outpost whose no-PvP bubble contains a position, or nil.
// Outposts sit far enough apart that a position is in at most one; when tuning
// ever overlaps two, the lowest ID wins so the answer is deterministic.
func (w *World) outpostAt(pos Vec) *tuning.Outpost {
	for index := range w.outposts {
		outpost := &w.outposts[index]
		at := Vec{outpost.Position[0], outpost.Position[1]}
		if pos.Sub(at).LengthSq() <= outpost.SafeRadius*outpost.SafeRadius {
			return outpost
		}
	}
	return nil
}

// inOutpostBubble reports whether a position sits inside any outpost's no-PvP
// radius.
func (w *World) inOutpostBubble(pos Vec) bool { return w.outpostAt(pos) != nil }

// serviceAt reports whether a specific service is available where a body stands:
// the central hub offers all of them, and an outpost offers whatever its row
// declares. Everywhere else offers nothing.
func (w *World) serviceAt(pos Vec, service string) bool {
	if w.tuning.Field.Safe(pos.X, pos.Y) {
		return true
	}
	if outpost := w.outpostAt(pos); outpost != nil {
		return outpost.Offers(service)
	}
	return false
}

// stepDiscovery unlocks an outpost the body has reached for the first time. It
// awards the discovery XP source and marks the character for an immediate state
// save, so the unlock persists on the same pass rather than waiting for the next
// autosave — the way a loadout commit does. A developer fixture discovers
// nothing, since it has no character row to persist.
func (w *World) stepDiscovery(p *Player) {
	if p.AdminSpawned {
		return
	}
	for _, outpost := range w.outposts {
		if containsID(p.Outposts, outpost.ID) {
			continue
		}
		at := Vec{outpost.Position[0], outpost.Position[1]}
		if p.Position.Sub(at).LengthSq() > outpost.DiscoveryRadius*outpost.DiscoveryRadius {
			continue
		}
		p.Outposts = append(p.Outposts, outpost.ID)
		sort.Strings(p.Outposts)
		w.awardXP(p, tuning.SourceDiscovery)
		w.stateDirty[p.ID] = true
	}
}

// containsID reports membership in a small sorted/unsorted slice of IDs.
func containsID(ids []string, id string) bool {
	for _, existing := range ids {
		if existing == id {
			return true
		}
	}
	return false
}

// DrainDirtyState reports the characters whose persisted world state changed
// since the last drain — an outpost unlock — and clears the marks. The engine
// saves each immediately; the world never writes.
func (w *World) DrainDirtyState() []string {
	if len(w.stateDirty) == 0 {
		return nil
	}
	ids := make([]string, 0, len(w.stateDirty))
	for id := range w.stateDirty {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	w.stateDirty = make(map[string]bool)
	return ids
}

// insideOutpost reports whether a candidate of the given radius falls inside any
// outpost's safe footprint. Terrain generation defers to it the way it defers to
// authored fixtures, so no scatter and no ridge belt materialises inside an
// outpost — the ground there is always open and standable.
func (w *World) insideOutpost(position Vec, radius float64) bool {
	for _, outpost := range w.outposts {
		at := Vec{outpost.Position[0], outpost.Position[1]}
		reach := radius + outpost.SafeRadius
		if at.Sub(position).LengthSq() < reach*reach {
			return true
		}
	}
	return false
}

// updateExitInvuln grants the brief invulnerability that leaving a no-PvP bubble
// gives, and tracks whether the body is protected this tick. The grant fires on
// the protected→unprotected edge, so standing in safety never refreshes it and
// walking back in never re-arms it.
func (w *World) updateExitInvuln(p *Player, now time.Time) {
	protectedNow := w.Protected(p.Position)
	if p.WasProtected && !protectedNow && w.tuning.ExitInvuln > 0 {
		p.ExitInvulnUntil = now.Add(w.tuning.ExitInvuln)
	}
	p.WasProtected = protectedNow
}

// nearestUnlockedOutpost is where a death or a stale-position recall sends a
// character: the unlocked outpost nearest to where it fell, or the central hub
// when that is closer or nothing is unlocked. The chosen fixture must be
// standable in the world as it is now.
func (w *World) nearestUnlockedOutpost(id string, unlocked []string, from Vec) Vec {
	best := w.hubSpawn(id)
	nearest := best.Sub(from).LengthSq()
	for _, outpostID := range unlocked {
		outpost, ok := w.tuning.Tables.Outposts[outpostID]
		if !ok {
			continue
		}
		position := Vec{outpost.Position[0], outpost.Position[1]}
		w.loadChunksAround(position)
		if distance := position.Sub(from).LengthSq(); distance < nearest && w.standable(position) {
			best, nearest = position, distance
		}
	}
	return best
}
