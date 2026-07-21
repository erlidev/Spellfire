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
			Type: 1, ID: p.ID, Name: p.Name, ClassName: string(p.Class),
			X: float32(p.Position.X), Y: float32(p.Position.Y), VX: float32(p.Velocity.X), VY: float32(p.Velocity.Y),
			AimX: float32(p.Aim.X), AimY: float32(p.Aim.Y), Health: float32(p.Health), MaxHealth: float32(w.tuning.MaxHealth),
			Mana: float32(resource), AcknowledgedInput: p.Acknowledged, Alive: p.Alive,
		})
	}
	for _, id := range sortedProjectileIDs(w.projectiles) {
		p := w.projectiles[id]
		if p.Position.Sub(viewer.Position).LengthSq() > radiusSq {
			continue
		}
		message.Entities = append(message.Entities, protocol.Entity{Type: 2, ID: p.ID, ClassName: p.Kind, X: float32(p.Position.X), Y: float32(p.Position.Y), VX: float32(p.Velocity.X), VY: float32(p.Velocity.Y), OwnerID: p.OwnerID, Alive: true})
	}
	for _, c := range w.colliders {
		if c.Position.Sub(viewer.Position).LengthSq() <= math.Pow(w.tuning.AOIRadius+c.Radius, 2) {
			message.Colliders = append(message.Colliders, protocol.Collider{ID: c.ID, X: float32(c.Position.X), Y: float32(c.Position.Y), Radius: float32(c.Radius), Kind: c.Kind})
		}
	}
	return message
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
