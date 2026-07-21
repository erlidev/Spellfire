package game

import (
	"fmt"
	"math"
	"sort"
	"time"

	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

// Telegraph is one authoritative world warning. Its geometry is locked when
// the action commits, so a caster cannot rotate or drag an already-readable
// danger area over a player during the windup.
type Telegraph struct {
	Entity
	OwnerID, AbilityID, Element string
	Direction                   Vec
	Shape                       string
	Radius, Length, Width       float64
	AngleDegrees                float64
	StartedAt, PendingUntil     time.Time
	ActiveUntil, ExpiresAt      time.Time
	Delivered                   bool
	ability                     tuning.Ability
}

func (t *Telegraph) state(at time.Time) uint64 {
	if at.Before(t.PendingUntil) {
		return protocol.TelegraphPending
	}
	if at.Before(t.ActiveUntil) {
		return protocol.TelegraphActive
	}
	return protocol.TelegraphResolved
}

func (t *Telegraph) progress(at time.Time) float64 {
	var start, end time.Time
	switch t.state(at) {
	case protocol.TelegraphPending:
		start, end = t.StartedAt, t.PendingUntil
	case protocol.TelegraphActive:
		start, end = t.PendingUntil, t.ActiveUntil
	default:
		start, end = t.ActiveUntil, t.ExpiresAt
	}
	if span := end.Sub(start); span > 0 {
		return math.Max(0, math.Min(1, float64(at.Sub(start))/float64(span)))
	}
	return 1
}

// startTelegraph is owner-agnostic by construction. Player spells use it now;
// Sentries, bosses, and deployables pass their own owner, element, origin, and
// direction through the same entry point when those entity loops land.
func (w *World) startTelegraph(ownerID, element string, origin, direction Vec, ability tuning.Ability, now time.Time) *Telegraph {
	row := ability.Telegraph
	if row == nil || ability.WindupMS <= 0 {
		return nil
	}
	direction = direction.Normalized()
	telegraph := &Telegraph{
		Entity:  newEntity(fmt.Sprintf("t-%d", w.nextTelegraph), "telegraph", origin, w.tuning.Tables.Entities["telegraph"], EntityOverrides{}),
		OwnerID: ownerID, AbilityID: ability.ID, Element: element,
		Direction: direction, Shape: row.Shape,
		Radius: row.Radius, Length: row.Length, Width: row.Width, AngleDegrees: row.AngleDegrees,
		StartedAt: now, PendingUntil: now.Add(ability.Windup()), ability: ability,
	}
	telegraph.ActiveUntil = telegraph.PendingUntil.Add(row.ActiveDuration())
	telegraph.ExpiresAt = telegraph.ActiveUntil.Add(row.ResolvedDuration())
	w.nextTelegraph++
	w.telegraphs[telegraph.ID] = telegraph
	return telegraph
}

func (w *World) stepTelegraphs(now time.Time) {
	for _, id := range sortedTelegraphIDs(w.telegraphs) {
		telegraph := w.telegraphs[id]
		if telegraph.Deleting {
			continue
		}
		if !telegraph.Delivered && !now.Before(telegraph.PendingUntil) {
			telegraph.Delivered = true
			// Owner lifecycle code cancels pending warnings on death. Delivery
			// itself does not inspect the player map, so a Sentry or boss uses
			// this same emitter rather than growing an owner-specific branch.
			w.deliverAt(telegraph.OwnerID, telegraph.Position, telegraph.Direction, telegraph.ability, now, telegraph.Element)
		}
		if !now.Before(telegraph.ExpiresAt) {
			delete(w.telegraphs, id)
		}
	}
}

// cancelTelegraphs turns an interrupted pending cast directly into its
// resolved flash. The warning never silently disappears, but it cannot deliver
// after its owner dies.
func (w *World) cancelTelegraphs(ownerID string, now time.Time) {
	for _, telegraph := range w.telegraphs {
		if telegraph.OwnerID != ownerID || telegraph.Delivered {
			continue
		}
		telegraph.Delivered = true
		telegraph.PendingUntil, telegraph.ActiveUntil = now, now
		telegraph.ExpiresAt = now.Add(telegraph.ability.Telegraph.ResolvedDuration())
	}
}

func sortedTelegraphIDs(telegraphs map[string]*Telegraph) []string {
	ids := make([]string, 0, len(telegraphs))
	for id := range telegraphs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
