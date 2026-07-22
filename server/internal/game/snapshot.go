package game

import (
	"fmt"
	"math"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

func (w *World) SnapshotFor(playerID string, now time.Time, kind uint64) protocol.ServerEnvelope {
	message := protocol.ServerEnvelope{Kind: kind, ServerTick: w.tick, ServerTimeMS: uint64(now.UnixMilli()), PlayerID: playerID}
	viewer := w.players[playerID]
	if viewer == nil {
		message.Error = "player is not in the world"
		return message
	}
	viewDistance := w.tuning.AOIRadius
	if viewer.ViewDistance > 0 {
		viewDistance = viewer.ViewDistance
	}
	// A scope is the one camera exception: it trades peripheral vision on the
	// client for reach, and the server has to send what that reach can see.
	if viewer.Scoped {
		if scope := w.scope(viewer); scope != nil {
			viewDistance += scope.ViewBonus
		}
	}
	// The camera is rectangular. Treating the configured reach as a radius cut
	// off both visible corners, so the interest area is the full square whose
	// half-width and half-height are the maximum view distance.
	outsideView := func(at Vec, extent float64) bool {
		delta := at.Sub(viewer.Position)
		reach := viewDistance + extent
		return math.Abs(delta.X) > reach || math.Abs(delta.Y) > reach
	}
	// A flashbang takes vision whole, and a smoke cloud takes it along one line.
	// Both are enforced here rather than drawn over on the client: what a player
	// cannot see, a client is never sent.
	blind := w.blinded(viewer)
	hidden := func(at Vec) bool {
		return outsideView(at, 0) || blind || w.occluded(viewer.Position, at)
	}
	for _, id := range sortedPlayerIDs(w.players) {
		p := w.players[id]
		if id != playerID && hidden(p.Position) {
			continue
		}
		resource := p.Mana
		if p.Class == model.Gunslinger {
			resource = float64(p.Ammo)
			// A weapon that spends crafted ammunition has no magazine to meter,
			// so the resource it reports is what it is actually carrying.
			if ability, ok := w.ability(p); ok && ability.Cost.Kind == tuning.CostMaterial {
				resource = float64(p.Materials[ability.Cost.Material])
			}
		}
		message.Entities = append(message.Entities, protocol.Entity{
			Type: protocol.EntityPlayer, ID: p.ID, Name: p.Name, ClassName: string(p.Class),
			X: float32(p.Position.X), Y: float32(p.Position.Y), VX: float32(p.Velocity.X), VY: float32(p.Velocity.Y),
			AimX: float32(p.Aim.X), AimY: float32(p.Aim.Y), Health: float32(p.Health), MaxHealth: float32(p.MaxHealth),
			Mana: float32(resource), AcknowledgedInput: p.Acknowledged, Alive: p.Alive,
			Element: w.playerElement(p), SquadID: p.SquadID, Allegiance: playerAllegiance(viewer, p),
			Lingering: p.Lingering(), EffectIDs: activeEffectIDs(p.Effects),
			Mass: float32(p.Mass), Radius: float32(p.circleRadius()),
			Deleting: p.Deleting, DeleteProgress: float32(p.deleteProgress(now)),
			Scoped: p.Scoped, Guarding: p.Guarding,
			RecoilDegrees: float32(w.recoilDegrees(p, now)), Shots: p.Fired,
		})
	}
	for _, id := range sortedProjectileIDs(w.projectiles) {
		p := w.projectiles[id]
		if hidden(p.Position) {
			continue
		}
		message.Entities = append(message.Entities, protocol.Entity{
			Type: protocol.EntityProjectile, ID: p.ID, ClassName: p.Kind,
			X: float32(p.Position.X), Y: float32(p.Position.Y), VX: float32(p.Velocity.X), VY: float32(p.Velocity.Y),
			Health: float32(p.Health), MaxHealth: float32(p.MaxHealth), OwnerID: p.OwnerID, Element: p.Element,
			Allegiance: ownerAllegiance(viewer, w.players[p.OwnerID]), Alive: p.Alive, Mass: float32(p.Mass),
			Deleting: p.Deleting, DeleteProgress: float32(p.deleteProgress(now)),
		})
	}
	for _, id := range sortedTelegraphIDs(w.telegraphs) {
		telegraph := w.telegraphs[id]
		if hidden(telegraph.Position) {
			continue
		}
		message.Entities = append(message.Entities, protocol.Entity{
			Type: protocol.EntityTelegraph, ID: telegraph.ID, OwnerID: telegraph.OwnerID, AbilityID: telegraph.AbilityID,
			X: float32(telegraph.Position.X), Y: float32(telegraph.Position.Y),
			AimX: float32(telegraph.Direction.X), AimY: float32(telegraph.Direction.Y),
			Element: telegraph.Element, Allegiance: ownerAllegiance(viewer, w.players[telegraph.OwnerID]),
			TelegraphState: telegraph.state(now), TelegraphShape: telegraph.Shape,
			Radius: float32(telegraph.Radius), Length: float32(telegraph.Length), Width: float32(telegraph.Width),
			AngleDegrees: float32(telegraph.AngleDegrees), TelegraphProgress: float32(telegraph.progress(now)),
			Health: float32(telegraph.Health), MaxHealth: float32(telegraph.MaxHealth), Alive: telegraph.Alive,
			Mass: float32(telegraph.Mass), Deleting: telegraph.Deleting, DeleteProgress: float32(telegraph.deleteProgress(now)),
		})
	}
	for _, id := range sortedDeployableIDs(w.deployables) {
		deployable := w.deployables[id]
		// A cloud is never hidden by a cloud: what is standing in the world is
		// exactly what explains why everything behind it went missing.
		if blind || outsideView(deployable.Position, deployable.Field.Radius) {
			continue
		}
		message.Entities = append(message.Entities, protocol.Entity{
			Type: protocol.EntityDeployable, ID: deployable.ID, ClassName: deployable.Kind, OwnerID: deployable.OwnerID,
			X: float32(deployable.Position.X), Y: float32(deployable.Position.Y),
			Radius: float32(deployable.Field.Radius), Alive: deployable.Alive, Mass: float32(deployable.Mass),
			Allegiance: ownerAllegiance(viewer, w.players[deployable.OwnerID]),
			Deleting:   deployable.Deleting, DeleteProgress: float32(deployable.deleteProgress(now)),
		})
	}
	// Terrain is deliberately outside the occlusion rule: static cover blinking
	// in and out as a cloud drifts would desynchronise the client's own
	// collision prediction, and the cloud drawn over it already hides it.
	for _, item := range w.worldItems {
		if item == nil || (!item.Alive && !item.Deleting) {
			continue
		}
		extent := item.boundingRadius()
		if outsideView(item.Position, extent) {
			continue
		}
		entity := protocol.Entity{
			Type: protocol.EntityWorldItem, ID: item.ID, ClassName: item.Kind,
			X: float32(item.Position.X), Y: float32(item.Position.Y), VX: float32(item.Velocity.X), VY: float32(item.Velocity.Y),
			Health: float32(item.Health), MaxHealth: float32(item.MaxHealth), Alive: item.Alive, Mass: float32(item.Mass), Allegiance: protocol.AllegianceNeutral,
			Deleting: item.Deleting, DeleteProgress: float32(item.deleteProgress(now)),
		}
		if len(item.CollisionObjects) > 0 {
			primary := item.CollisionObjects[0]
			entity.Radius, entity.Length, entity.Width = float32(primary.Radius), float32(primary.HalfWidth*2), float32(primary.HalfHeight*2)
		}
		message.Entities = append(message.Entities, entity)
		if item.Deleting {
			continue
		}
		for index, object := range item.CollisionObjects {
			shape := "circle"
			if object.Type == CollisionBox {
				shape = "box"
			}
			position := item.Position.Add(object.Offset)
			message.Colliders = append(message.Colliders, protocol.Collider{
				ID: fmt.Sprintf("%s:%d", item.ID, index), EntityID: item.ID, Kind: item.Kind, Shape: shape,
				X: float32(position.X), Y: float32(position.Y), Radius: float32(object.Radius), Width: float32(object.HalfWidth * 2), Height: float32(object.HalfHeight * 2),
			})
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

func (w *World) WorldItems() []Entity {
	items := make([]Entity, 0, len(w.worldItems))
	for _, item := range w.worldItems {
		if item == nil {
			continue
		}
		copy := *item
		copy.CollisionObjects = append([]CollisionObject(nil), item.CollisionObjects...)
		items = append(items, copy)
	}
	return items
}

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
