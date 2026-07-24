package game

import (
	"fmt"
	"sort"
	"time"

	"spellfire/server/internal/crafting"
	"spellfire/server/internal/tuning"
)

// Rideables are the crafted-to-spawn transport entities: a Gunslinger's vehicle
// (a motorcycle) or a Mage's summoned mount (a horse). Both are the same code
// path, data-driven from rideables.json — what differs is the class that builds
// one, its cost, and its speed. Crafting one spawns the entity beside the player
// rather than adding anything to inventory, and there is one per owner.
//
// Riding is server-authoritative movement: a mounted body drives its ride at the
// ride's speed and cannot dash, fire, or use. It is transport only. Incoming
// damage routes to the ride, and a ride destroyed forces the dismount, so the
// exposure a long journey carries is never removed — only shortened.
type Rideable struct {
	Entity
	OwnerID string
	// RiderID is the body currently astride it, equal to OwnerID while ridden and
	// empty while it stands idle.
	RiderID   string
	Class     string
	RecipeID  string
	RideSpeed float64
}

func (w *World) addRideable(r *Rideable) {
	w.rideables[r.ID] = r
	w.rideGrid.insert(r)
}

func (w *World) removeRideable(id string) {
	if r := w.rideables[id]; r != nil {
		w.rideGrid.remove(r)
		delete(w.rideables, id)
	}
}

// ownerRideable returns the one rideable an owner has in the world, or nil.
func (w *World) ownerRideable(ownerID string) *Rideable {
	for _, r := range w.rideables {
		if r.OwnerID == ownerID && !r.Deleting {
			return r
		}
	}
	return nil
}

// CraftRideable builds one rideable and places it in the world beside the
// player. It runs the same gates a craft does — a crafting service where it
// stands, a live recipe for the class, and the materials actually carried — and
// like ammunition it leaves no inventory item. There is one ride per owner, so a
// second craft replaces the first.
func (w *World) CraftRideable(id, recipeID string, now time.Time) (map[string]int, error) {
	p := w.players[id]
	if p == nil {
		return nil, ErrCraftingUnavailable
	}
	if !p.Alive || p.Lingering() || p.AdminSpawned || p.Mounted() {
		return nil, ErrCraftingUnavailable
	}
	if !w.serviceAt(p.Position, "crafting") {
		return nil, ErrCraftingLocked
	}
	recipe, err := crafting.ValidateRideable(w.tuning.Tables, p.Class, recipeID)
	if err != nil {
		return nil, err
	}
	if short := crafting.Shortfall(recipe.Cost, p.Materials); len(short) > 0 {
		return nil, w.shortfallError(short)
	}
	crafting.Spend(p.Materials, recipe.Cost)
	w.spawnRideable(p, recipe, now)
	return p.CarriedMaterials(), nil
}

// spawnRideable materialises one ride beside the player, replacing any the owner
// already has. It is placed a short way to the side so it does not sit exactly
// on top of the body that summoned it.
func (w *World) spawnRideable(p *Player, recipe tuning.Rideable, now time.Time) {
	if existing := w.ownerRideable(p.ID); existing != nil {
		if rider := w.players[existing.RiderID]; rider != nil {
			rider.MountID = ""
		}
		w.removeRideable(existing.ID)
	}
	definition := w.tuning.Tables.Entities[recipe.Entity]
	radius := p.circleRadius() + 8
	for _, object := range definition.CollisionObjects {
		if object.Radius > 0 {
			radius = p.circleRadius() + object.Radius + 8
		}
	}
	at := p.Position.Add(perp(p.Aim).Mul(radius))
	if w.collides(at, definition.CollisionObjects[0].Radius) {
		at = p.Position
	}
	r := &Rideable{
		Entity:  newEntity(fmt.Sprintf("r-%d", w.nextRideable), recipe.Entity, at, definition, EntityOverrides{}),
		OwnerID: p.ID, Class: recipe.Class, RecipeID: recipe.ID, RideSpeed: recipe.RideSpeed,
	}
	r.SpawnedAt = now
	w.nextRideable++
	w.addRideable(r)
}

// tryMount puts the player on an owned ride standing within reach, unless it is
// too soon after combat. It reports whether the body actually mounted.
func (w *World) tryMount(p *Player, now time.Time) bool {
	if p.Mounted() || now.Before(p.LastCombat.Add(w.tuning.MountLockout)) {
		return false
	}
	r := w.ownerRideable(p.ID)
	if r == nil || !r.Alive || r.RiderID != "" {
		return false
	}
	reach := p.circleRadius() + r.circleRadius() + 40
	if p.Position.Sub(r.Position).LengthSq() > reach*reach {
		return false
	}
	p.MountID = r.ID
	r.RiderID = p.ID
	p.Position, p.Velocity, p.DashTicksLeft = r.Position, Vec{}, 0
	p.Scoped, p.Guarding = false, false
	w.bodies.update(p)
	return true
}

// rideStep advances a mounted body for one tick: the interact button dismounts,
// and otherwise the body drives its ride.
func (w *World) rideStep(p *Player, move Vec, interact bool, now time.Time, dt float64) {
	// A stun suppresses everything the body does, dismounting included, exactly
	// as it suppresses movement and every use.
	if interact && !w.stunned(p) {
		w.dismount(p, now)
		return
	}
	w.driveMount(p, move, now, dt)
}

// driveMount moves the ride under player input and keeps the rider synced to it.
// Statuses still apply: a slow scales the ride's speed, and a root or stun stops
// it, so control effects are not shaken off by being mounted.
func (w *World) driveMount(p *Player, move Vec, now time.Time, dt float64) {
	r := w.rideables[p.MountID]
	if r == nil || !r.Alive {
		w.dismount(p, now)
		return
	}
	speed := w.tuning.PlayerSpeed * p.SpeedMultiplier * r.RideSpeed * w.movementScale(p)
	if w.stunned(p) || w.rooted(p) {
		speed = 0
	}
	velocity := move.Mul(speed)
	r.Position = w.moveCircle(r.Position, velocity.Mul(dt), r.circleRadius())
	r.Velocity = velocity
	w.rideGrid.update(r)
	p.Position, p.Velocity = r.Position, velocity
	p.Scoped, p.Guarding, p.DashTicksLeft = false, false, 0
	w.bodies.update(p)
}

// dismount puts a rider back on foot where its ride stands. The ride stays in the
// world, idle, so the body can mount it again.
func (w *World) dismount(p *Player, now time.Time) {
	if p.MountID == "" {
		return
	}
	if r := w.rideables[p.MountID]; r != nil {
		r.RiderID, r.Velocity = "", Vec{}
		w.rideGrid.update(r)
		p.Position = r.Position
	}
	p.MountID, p.Velocity = "", Vec{}
	w.bodies.update(p)
}

// damageRideable routes a hit meant for a mounted body onto its ride, and forces
// the dismount when the ride is destroyed. Shields and armor do not apply — a
// ride has raw health — and both parties are marked in combat.
func (w *World) damageRideable(rider *Player, amount float64, sourceID string, at time.Time) {
	r := w.rideables[rider.MountID]
	if r == nil {
		w.dismount(rider, at)
		return
	}
	_, destroyed := r.TakeDamage(amount)
	rider.LastCombat = at
	if source := w.players[sourceID]; source != nil {
		source.LastCombat = at
	}
	if destroyed {
		w.destroyRideable(r, at)
	}
}

// destroyRideable ends a ride: the rider is put back on foot where it stood, and
// the entity fades through the shared graceful removal.
func (w *World) destroyRideable(r *Rideable, now time.Time) {
	if rider := w.players[r.RiderID]; rider != nil {
		rider.MountID, rider.Velocity = "", Vec{}
		rider.Position = r.Position
		w.bodies.update(rider)
	}
	r.RiderID = ""
	r.Delete(now)
	w.rideGrid.update(r)
}

// reapRideableOf removes an owner's ride when they leave the world. A ride is
// transient — it is never persisted and never outlives its owner's session.
func (w *World) reapRideableOf(ownerID string) {
	if r := w.ownerRideable(ownerID); r != nil {
		if rider := w.players[r.RiderID]; rider != nil {
			rider.MountID = ""
		}
		w.removeRideable(r.ID)
	}
}

// Rideables exposes the rides for tests and tooling, in ID order.
func (w *World) Rideables() []Rideable {
	fields := make([]Rideable, 0, len(w.rideables))
	for _, id := range sortedRideableIDs(w.rideables) {
		fields = append(fields, *w.rideables[id])
	}
	return fields
}

func sortedRideableIDs(rideables map[string]*Rideable) []string {
	ids := make([]string, 0, len(rideables))
	for id := range rideables {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// perp returns a unit vector perpendicular to v, or a default when v is zero, so
// a summoned ride lands beside the body rather than under it.
func perp(v Vec) Vec {
	n := v.Normalized()
	if n.LengthSq() == 0 {
		return Vec{0, 1}
	}
	return Vec{-n.Y, n.X}
}
