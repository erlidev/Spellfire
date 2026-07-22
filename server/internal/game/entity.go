package game

import (
	"math"
	"time"

	"spellfire/server/internal/tuning"
)

// CollisionType is a compact runtime geometry tag. The numeric representation
// keeps hot collision loops data-oriented and can move unchanged into a future
// ECS component store.
type CollisionType uint8

const (
	CollisionCircle CollisionType = iota + 1
	CollisionBox
)

// CollisionObject is one entity-local collision primitive. Entities may carry
// multiple objects without requiring a new runtime type or collision interface.
type CollisionObject struct {
	Type                  CollisionType
	Offset                Vec
	Radius                float64
	HalfWidth, HalfHeight float64
}

// Entity is the common mutable state embedded by every materialized world
// object. It intentionally contains data, not polymorphic behavior: a future
// ECS can split these fields into components without unpicking an interface
// hierarchy.
type Entity struct {
	ID, Kind           string
	DefinitionID       string
	Position, Velocity Vec
	Mass               float64
	Health, MaxHealth  float64
	Alive              bool
	OccludesVision     bool
	VisibleInShadow    bool
	CollisionObjects   []CollisionObject
	AdminSpawned       bool
	Deleting           bool
	DeleteStarted      time.Time
	DeleteEnds         time.Time
	// SpawnedAt is when a runtime-authored entity came into being, and is zero
	// for everything the world started with. Together with DeleteStarted it is
	// the entity's lifetime, which is what a rewound shot has to be tested
	// against: a round rewound to before a wall was raised must pass through it,
	// and one rewound to while it stood must be stopped by it.
	SpawnedAt time.Time
}

const (
	entityDeleteFade      = 350 * time.Millisecond
	entityDeleteReapDelay = 100 * time.Millisecond
)

// Deletable is the lifecycle seam shared by every entity family. Entity's
// embedded method satisfies it today; an ECS can implement the same operation
// by marking lifecycle components without changing callers.
type Deletable interface{ Delete(time.Time) }

var (
	_ Deletable = (*Entity)(nil)
	_ Deletable = (*Player)(nil)
	_ Deletable = (*Projectile)(nil)
	_ Deletable = (*Telegraph)(nil)
)

// EntityOverrides supplies typed per-instance values. Pointer fields preserve
// the distinction between "not overridden" and a legitimate zero. Replacing
// collision geometry is explicit because an empty slice is a useful override.
type EntityOverrides struct {
	Mass, Health, MaxHealth *float64
	CollisionObjects        *[]CollisionObject
}

func newEntity(id, kind string, position Vec, definition tuning.EntityDefinition, overrides EntityOverrides) Entity {
	entity := Entity{
		ID: id, Kind: kind, DefinitionID: kind, Position: position, Mass: definition.Mass,
		Health: definition.MaxHealth, MaxHealth: definition.MaxHealth, Alive: true,
		OccludesVision: definition.OccludesVision, VisibleInShadow: definition.VisibleInShadow,
		CollisionObjects: collisionObjectsFromTuning(definition.CollisionObjects),
	}
	if overrides.MaxHealth != nil && overrides.Health == nil {
		initialHealth := *overrides.MaxHealth
		overrides.Health = &initialHealth
	}
	entity.ApplyOverrides(overrides)
	return entity
}

// Delete starts an idempotent graceful removal. It immediately leaves physics
// and gameplay, remains in snapshots for the fade window, then becomes
// eligible for collection by its owning world store.
func (e *Entity) Delete(now time.Time) {
	if e.Deleting {
		return
	}
	e.Deleting, e.Alive, e.Health = true, false, 0
	e.Velocity = Vec{}
	e.DeleteStarted, e.DeleteEnds = now, now.Add(entityDeleteFade)
}

func (e *Entity) deleteProgress(now time.Time) float64 {
	if !e.Deleting {
		return 0
	}
	return math.Max(0, math.Min(1, float64(now.Sub(e.DeleteStarted))/float64(entityDeleteFade)))
}

func (e *Entity) deleteComplete(now time.Time) bool {
	return e.Deleting && !now.Before(e.DeleteEnds.Add(entityDeleteReapDelay))
}

// presentAt reports whether the entity's geometry stood in the world at a past
// moment: raised before it, and neither destroyed nor expired until after. It
// is what keeps lag compensation honest about player-authored terrain, whose
// lifetime is shorter than the rewind window is wide.
func (e *Entity) presentAt(at time.Time) bool {
	if !e.SpawnedAt.IsZero() && at.Before(e.SpawnedAt) {
		return false
	}
	return !e.Deleting || at.Before(e.DeleteStarted)
}

func (e *Entity) cancelDelete() {
	e.Deleting, e.DeleteStarted, e.DeleteEnds = false, time.Time{}, time.Time{}
}

func (w *World) newProjectileEntity(id string, position, velocity Vec, radius float64) Entity {
	definition := w.tuning.Tables.Entities["projectile"]
	objects := collisionObjectsFromTuning(definition.CollisionObjects)
	for index := range objects {
		if objects[index].Type == CollisionCircle {
			objects[index].Radius = radius
		}
	}
	entity := newEntity(id, "projectile", position, definition, EntityOverrides{CollisionObjects: &objects})
	entity.Velocity = velocity
	return entity
}

// ApplyOverrides changes only explicitly supplied runtime values. Callers may
// use it at construction or later for temporary effects and scripted state.
func (e *Entity) ApplyOverrides(overrides EntityOverrides) {
	if overrides.Mass != nil {
		e.Mass = *overrides.Mass
	}
	if overrides.MaxHealth != nil {
		e.MaxHealth = *overrides.MaxHealth
	}
	if overrides.Health != nil {
		e.Health = *overrides.Health
		e.Alive = e.Health != 0
	}
	if overrides.CollisionObjects != nil {
		e.CollisionObjects = append([]CollisionObject(nil), (*overrides.CollisionObjects)...)
	}
}

// TakeDamage applies damage to any destroyable entity. Negative health is the
// data-level sentinel for an undestroyable entity, so no type-specific branch
// is needed for walls or future permanent fixtures.
func (e *Entity) TakeDamage(amount float64) (applied float64, destroyed bool) {
	if !e.Alive || e.Health < 0 || amount <= 0 {
		return 0, false
	}
	applied = math.Min(e.Health, amount)
	e.Health = math.Max(0, e.Health-applied)
	if e.Health == 0 {
		e.Alive = false
	}
	return applied, !e.Alive
}

func (e *Entity) restoreHealth() {
	e.Health, e.Alive = e.MaxHealth, true
}

func (e *Entity) boundingRadius() float64 {
	radius := 0.0
	for _, object := range e.CollisionObjects {
		extent := object.Offset.LengthSq()
		switch object.Type {
		case CollisionCircle:
			extent = math.Sqrt(extent) + object.Radius
		case CollisionBox:
			extent = math.Sqrt(extent) + math.Hypot(object.HalfWidth, object.HalfHeight)
		}
		radius = math.Max(radius, extent)
	}
	return radius
}

func (e *Entity) circleRadius() float64 {
	for _, object := range e.CollisionObjects {
		if object.Type == CollisionCircle {
			return object.Radius
		}
	}
	return 0
}

func collisionObjectsFromTuning(objects []tuning.CollisionObject) []CollisionObject {
	result := make([]CollisionObject, 0, len(objects))
	for _, object := range objects {
		converted := CollisionObject{Offset: Vec{object.OffsetX, object.OffsetY}, Radius: object.Radius, HalfWidth: object.Width / 2, HalfHeight: object.Height / 2}
		switch object.Type {
		case "circle":
			converted.Type = CollisionCircle
		case "box":
			converted.Type = CollisionBox
		}
		result = append(result, converted)
	}
	return result
}

func (e *Entity) intersectsCircle(position Vec, radius float64) bool {
	if !e.Alive {
		return false
	}
	for _, object := range e.CollisionObjects {
		center := e.Position.Add(object.Offset)
		switch object.Type {
		case CollisionCircle:
			limit := radius + object.Radius
			if position.Sub(center).LengthSq() < limit*limit {
				return true
			}
		case CollisionBox:
			dx := math.Max(math.Abs(position.X-center.X)-object.HalfWidth, 0)
			dy := math.Max(math.Abs(position.Y-center.Y)-object.HalfHeight, 0)
			if dx*dx+dy*dy < radius*radius {
				return true
			}
		}
	}
	return false
}

func (e *Entity) intersectsSegment(from, to Vec, radius float64) bool {
	return e.Alive && e.overlapsSegment(from, to, radius)
}

// blockedSegmentAt is intersectsSegment as it stood at a past moment. Lag
// compensation resolves a shot against the terrain of the claimed time, so a
// round rewound to before a wall went up passes through it, and one rewound to
// while a tree still stood is stopped by it even though it has since fallen.
func (e *Entity) blockedSegmentAt(from, to Vec, radius float64, at time.Time) bool {
	return e.presentAt(at) && e.overlapsSegment(from, to, radius)
}

func (e *Entity) overlapsSegment(from, to Vec, radius float64) bool {
	for _, object := range e.CollisionObjects {
		center := e.Position.Add(object.Offset)
		switch object.Type {
		case CollisionCircle:
			if segmentCircle(from, to, center, radius+object.Radius) {
				return true
			}
		case CollisionBox:
			if segmentCircleBox(from, to, center, object.HalfWidth, object.HalfHeight, radius) {
				return true
			}
		}
	}
	return false
}

// segmentCircleBox tests a swept circle against an axis-aligned box exactly:
// the Minkowski sum is two expanded rectangles plus a radius around each
// corner, rather than the over-large square produced by expanding both axes.
func segmentCircleBox(from, to, center Vec, halfWidth, halfHeight, radius float64) bool {
	if segmentBox(from, to, center, halfWidth+radius, halfHeight) || segmentBox(from, to, center, halfWidth, halfHeight+radius) {
		return true
	}
	for _, corner := range []Vec{{-halfWidth, -halfHeight}, {-halfWidth, halfHeight}, {halfWidth, -halfHeight}, {halfWidth, halfHeight}} {
		if segmentCircle(from, to, center.Add(corner), radius) {
			return true
		}
	}
	return false
}

func segmentBox(from, to, center Vec, halfWidth, halfHeight float64) bool {
	from, to = from.Sub(center), to.Sub(center)
	delta := to.Sub(from)
	tMin, tMax := 0.0, 1.0
	for _, axis := range [][4]float64{{from.X, delta.X, -halfWidth, halfWidth}, {from.Y, delta.Y, -halfHeight, halfHeight}} {
		origin, direction, low, high := axis[0], axis[1], axis[2], axis[3]
		if math.Abs(direction) < 1e-12 {
			if origin < low || origin > high {
				return false
			}
			continue
		}
		a, b := (low-origin)/direction, (high-origin)/direction
		if a > b {
			a, b = b, a
		}
		tMin, tMax = math.Max(tMin, a), math.Min(tMax, b)
		if tMin > tMax {
			return false
		}
	}
	return true
}
