package game

import (
	"math"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

func (w *World) SnapshotFor(playerID string, now time.Time, kind uint64) protocol.ServerEnvelope {
	message := protocol.ServerEnvelope{Kind: kind, ServerTick: w.tick, ServerTimeMS: uint64(now.UnixMilli()), PlayerID: playerID}
	viewer := w.players[playerID]
	if viewer == nil {
		message.Error = "player is not in the world"
		return message
	}
	radiusSq := w.tuning.AOIRadius * w.tuning.AOIRadius
	for _, id := range sortedPlayerIDs(w.players) {
		p := w.players[id]
		if id != playerID && p.Position.Sub(viewer.Position).LengthSq() > radiusSq {
			continue
		}
		resource := p.Mana
		if p.Class == model.Gunslinger {
			resource = float64(p.Ammo)
		}
		message.Entities = append(message.Entities, protocol.Entity{
			Type: protocol.EntityPlayer, ID: p.ID, Name: p.Name, ClassName: string(p.Class),
			X: float32(p.Position.X), Y: float32(p.Position.Y), VX: float32(p.Velocity.X), VY: float32(p.Velocity.Y),
			AimX: float32(p.Aim.X), AimY: float32(p.Aim.Y), Health: float32(p.Health), MaxHealth: float32(w.tuning.MaxHealth),
			Mana: float32(resource), AcknowledgedInput: p.Acknowledged, Alive: p.Alive,
			Element: w.playerElement(p), SquadID: p.SquadID, Allegiance: playerAllegiance(viewer, p),
			Lingering: p.Lingering(), EffectIDs: activeEffectIDs(p.Effects),
		})
	}
	for _, id := range sortedProjectileIDs(w.projectiles) {
		p := w.projectiles[id]
		if p.Position.Sub(viewer.Position).LengthSq() > radiusSq {
			continue
		}
		message.Entities = append(message.Entities, protocol.Entity{
			Type: protocol.EntityProjectile, ID: p.ID, ClassName: p.Kind,
			X: float32(p.Position.X), Y: float32(p.Position.Y), VX: float32(p.Velocity.X), VY: float32(p.Velocity.Y),
			OwnerID: p.OwnerID, Element: p.Element, Allegiance: ownerAllegiance(viewer, w.players[p.OwnerID]), Alive: true,
		})
	}
	for _, id := range sortedTelegraphIDs(w.telegraphs) {
		telegraph := w.telegraphs[id]
		if telegraph.Position.Sub(viewer.Position).LengthSq() > radiusSq {
			continue
		}
		message.Entities = append(message.Entities, protocol.Entity{
			Type: protocol.EntityTelegraph, ID: telegraph.ID, OwnerID: telegraph.OwnerID, AbilityID: telegraph.AbilityID,
			X: float32(telegraph.Position.X), Y: float32(telegraph.Position.Y),
			AimX: float32(telegraph.Direction.X), AimY: float32(telegraph.Direction.Y),
			Element: telegraph.Element, Allegiance: ownerAllegiance(viewer, w.players[telegraph.OwnerID]),
			TelegraphState: telegraph.state(now), TelegraphShape: telegraph.Shape,
			Radius: float32(telegraph.Radius), Length: float32(telegraph.Length), Width: float32(telegraph.Width),
			AngleDegrees: float32(telegraph.AngleDegrees), TelegraphProgress: float32(telegraph.progress(now)), Alive: true,
		})
	}
	for _, c := range w.colliders {
		if c.Position.Sub(viewer.Position).LengthSq() <= math.Pow(w.tuning.AOIRadius+c.Radius, 2) {
			message.Colliders = append(message.Colliders, protocol.Collider{ID: c.ID, X: float32(c.Position.X), Y: float32(c.Position.Y), Radius: float32(c.Radius), Kind: c.Kind})
		}
	}
	return message
}

func playerAllegiance(viewer, target *Player) uint64 {
	if viewer == nil || target == nil {
		return protocol.AllegianceNeutral
	}
	if viewer.ID == target.ID {
		return protocol.AllegianceSelf
	}
	if viewer.SquadID != "" && viewer.SquadID == target.SquadID {
		return protocol.AllegianceSquad
	}
	return protocol.AllegianceHostile
}

func ownerAllegiance(viewer, owner *Player) uint64 {
	if owner == nil {
		return protocol.AllegianceNeutral
	}
	return playerAllegiance(viewer, owner)
}

func activeEffectIDs(effects []ActiveEffect) []string {
	ids := make([]string, 0, len(effects))
	for _, effect := range effects {
		ids = append(ids, effect.EffectID)
	}
	return ids
}

func (w *World) PlayerState(id string) (Player, bool) {
	p := w.players[id]
	if p == nil {
		return Player{}, false
	}
	return *p, true
}

func (w *World) SetPlayerPosition(id string, position Vec, now time.Time) bool {
	p := w.players[id]
	if p == nil {
		return false
	}
	p.Position = position
	w.recordHistory(p, now)
	return true
}

func (w *World) Colliders() []Collider { return append([]Collider(nil), w.colliders...) }

// Contributions returns the current target life's effective-damage ledger,
// ordered by credit priority (most damage, then earliest contributor).
func (w *World) Contributions(targetID string) []DamageContribution {
	return w.combat.targetContributions(targetID)
}

// LastKill returns the immutable lethal event for the target's current dead
// life. Drop ownership can consume it without depending on the last hit.
func (w *World) LastKill(targetID string) (CombatEvent, bool) {
	return w.combat.lastKill(targetID)
}

// CombatEventsAfter exposes the bounded append-only stream to future drop and
// boss systems. Callers retain the last Sequence they processed as their cursor.
func (w *World) CombatEventsAfter(sequence uint64) []CombatEvent {
	return w.combat.eventsAfter(sequence)
}

func sortedProjectileIDs(projectiles map[string]*Projectile) []string {
	ids := make([]string, 0, len(projectiles))
	for id := range projectiles {
		ids = append(ids, id)
	}
	// Projectile identifiers are monotonic and lexical ordering is deterministic enough for snapshots.
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}
