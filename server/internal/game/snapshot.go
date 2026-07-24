package game

import (
	"fmt"
	"math"
	"sort"
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
	message.Cooldowns = playerCooldowns(viewer, now)
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
	// A flashbang takes vision whole, smoke and solid terrain both cast shadows,
	// and a body inside smoke sees only a small circle around itself. All are
	// enforced here rather than drawn over on the client: what a player cannot
	// see, a client is never sent. The occluder set is collected once for this
	// viewer's send rather than rescanning the world per candidate entity.
	blind := w.blinded(viewer)
	// Occluders are collected over the interest square rather than the world:
	// nothing outside it can stand between the viewer and something inside it.
	occ := w.collectOccluders(viewer.Position, viewDistance)
	// A target is hidden when no part of its silhouette has a clear line. The
	// viewer's own entities are the one exemption from smoke: a cloud that hid a
	// body's own rounds would read as a wall it could not shoot through, so those
	// are tested against terrain alone.
	hidden := func(at Vec, ownerID string, extent float64) bool {
		if outsideView(at, 0) || blind {
			return true
		}
		if ownerID == playerID {
			return !occ.visibleTerrain(viewer.Position, at, extent)
		}
		return !occ.visible(viewer.Position, at, extent)
	}
	// Every family is drawn from the spatial index over the interest square, so
	// a snapshot costs what is near the viewer rather than what the world holds.
	// Results are ordered by ID because index buckets are a map and a snapshot's
	// entity order must not depend on one.
	for _, p := range interest(w.bodies, viewer.Position, viewDistance) {
		if p.ID != playerID && hidden(p.Position, p.ID, p.circleRadius()) {
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
			Shield: float32(w.guardHealth(p)), MaxShield: float32(w.guardDurability(p)),
			Invulnerable: p.exitInvulnerable(now), Mounted: p.Mounted(),
		})
	}
	// Rideables — a Gunslinger's vehicle or a Mage's mount — are ordinary bodies
	// for line of sight: one behind a wall is hidden exactly as a player is.
	for _, r := range interest(w.rideGrid, viewer.Position, viewDistance) {
		if !r.Alive && !r.Deleting {
			continue
		}
		if hidden(r.Position, r.OwnerID, r.circleRadius()) {
			continue
		}
		message.Entities = append(message.Entities, protocol.Entity{
			Type: protocol.EntityMount, ID: r.ID, ClassName: r.Kind, OwnerID: r.OwnerID,
			X: float32(r.Position.X), Y: float32(r.Position.Y), VX: float32(r.Velocity.X), VY: float32(r.Velocity.Y),
			Health: float32(r.Health), MaxHealth: float32(r.MaxHealth), Alive: r.Alive,
			Mass: float32(r.Mass), Radius: float32(r.circleRadius()),
			Allegiance: ownerAllegiance(viewer, w.players[r.OwnerID]),
			Deleting:   r.Deleting, DeleteProgress: float32(r.deleteProgress(now)),
		})
	}
	for _, p := range interest(w.shots, viewer.Position, viewDistance) {
		if hidden(p.Position, p.OwnerID, p.circleRadius()) {
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
	for _, telegraph := range interest(w.warnings, viewer.Position, viewDistance) {
		// A telegraph is ground geometry rather than a body: it is hidden only
		// when the point it is anchored at is inside a cloud, since the shape it
		// warns about reaches well outside its own origin.
		if hidden(telegraph.Position, telegraph.OwnerID, 0) {
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
	for _, deployable := range interest(w.fieldGrid, viewer.Position, viewDistance) {
		// Fields are unaffected by line of sight: a smoke cloud is exactly what
		// explains why everything behind it went missing, and an area effect —
		// firestorm, blizzard, a cinder patch — is ground the player is entitled
		// to see and play around even through cover. Only distance and blindness
		// take a field off the wire.
		if blind || outsideView(deployable.Position, deployable.Field.Radius) {
			continue
		}
		message.Entities = append(message.Entities, protocol.Entity{
			Type: protocol.EntityDeployable, ID: deployable.ID, ClassName: deployable.Kind, OwnerID: deployable.OwnerID,
			X: float32(deployable.Position.X), Y: float32(deployable.Position.Y),
			Radius: float32(deployable.Field.Radius), Alive: deployable.Alive, Mass: float32(deployable.Mass),
			Element:    deployable.Element,
			Allegiance: ownerAllegiance(viewer, w.players[deployable.OwnerID]),
			Deleting:   deployable.Deleting, DeleteProgress: float32(deployable.deleteProgress(now)),
		})
	}
	// Terrain is deliberately outside the concealment rule: static cover blinking
	// in and out as a cloud drifts would desynchronise the client's own
	// collision prediction, and the cloud drawn over it already hides it.
	for _, item := range interest(w.terrain, viewer.Position, viewDistance) {
		if !item.Alive && !item.Deleting {
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

// playerCooldowns reports the viewer's own running lockouts as time left. It is
// the authoritative answer to "why did that button do nothing": the client
// cannot derive it, because whether a use was charged at all depends on mana,
// ammunition, and placement rules it only sees a snapshot late. Expired entries
// are simply omitted; the map is bounded by the action bar, so nothing has to
// sweep it.
func playerCooldowns(p *Player, now time.Time) []protocol.Cooldown {
	if len(p.Cooldowns) == 0 {
		return nil
	}
	ids := make([]string, 0, len(p.Cooldowns))
	for id := range p.Cooldowns {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	cooldowns := make([]protocol.Cooldown, 0, len(ids))
	for _, id := range ids {
		if left := p.Cooldowns[id].Sub(now); left > 0 {
			cooldowns = append(cooldowns, protocol.Cooldown{AbilityID: id, RemainingMS: uint32(left.Milliseconds() + 1)})
		}
	}
	if len(cooldowns) == 0 {
		return nil
	}
	return cooldowns
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
	w.bodies.update(p)
	w.recordHistory(p, now)
	return true
}

// WorldItems lists the terrain currently resident, in ID order. It is a tooling
// and test seam: nothing in the simulation wants every item in the world.
func (w *World) WorldItems() []Entity {
	items := make([]Entity, 0, w.terrain.len())
	w.terrain.all(func(item *Entity) bool {
		copy := *item
		copy.CollisionObjects = append([]CollisionObject(nil), item.CollisionObjects...)
		items = append(items, copy)
		return true
	})
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

// interest collects one family's members inside the viewer's square, ordered by
// ID. The square is the camera's, so a corner never loses state.
func interest[T gridMember](grid *spatialGrid[T], at Vec, reach float64) []T {
	found := make([]T, 0, 32)
	grid.near(at, reach, func(member T) bool {
		found = append(found, member)
		return true
	})
	sort.Slice(found, func(i, j int) bool { return found[i].indexEntity().ID < found[j].indexEntity().ID })
	return found
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
