package game

import (
	"fmt"
	"math"
	"sort"
	"time"

	"spellfire/server/internal/tuning"
)

// The stone wall is the only spell that authors terrain. It reuses the common
// entity and collision substrate the trees and the fixed fixture already run
// on: its segments are ordinary world items, so they block movement and
// projectiles, take damage, and are destroyed by exactly the code that destroys
// a tree. What the wall adds is a lifetime, an owner, and the placement rules
// that keep it from being used as a cage.
//
// Line of sight reads this same world-item geometry, so the wall blocks sight
// for exactly as long as it blocks movement and rounds.

// wallGroup is one caster's standing wall: the segments it raised and when they
// come down. One group per caster is the design's whole limit — raising a
// second wall drops the first.
type wallGroup struct {
	OwnerID   string
	Segments  []*Entity
	ExpiresAt time.Time
}

// wallSegments is where a wall's segments would stand: a row laid perpendicular
// to the direction the cast committed to, centred on the placed point. It is
// separated from raising so the gate below can answer "may this be placed"
// without putting anything into the world.
func wallSegments(at, direction Vec, wall tuning.Wall) []Vec {
	across := Vec{-direction.Y, direction.X}.Normalized()
	if across.LengthSq() == 0 {
		across = Vec{0, 1}
	}
	positions := make([]Vec, 0, wall.Segments)
	offset := -float64(wall.Segments-1) / 2
	for index := 0; index < wall.Segments; index++ {
		positions = append(positions, at.Add(across.Mul((offset+float64(index))*wall.Spacing)))
	}
	return positions
}

// placeable reports whether a cast that authors terrain may go ahead. It is
// checked before the cost is charged rather than at delivery, so a refused wall
// costs nothing and reads as a rule instead of a lost cast.
//
// Two rules: a wall may not be raised inside a safe zone, and it may not be
// raised on top of an actor. The first keeps safety from becoming an offensive
// tool; the second keeps a wall from boxing a player in. Both come straight
// from the invariants.
func (w *World) placeable(ability tuning.Ability, at, direction Vec) bool {
	if ability.Wall == nil {
		return true
	}
	definition, ok := w.tuning.Tables.Entities[ability.Wall.Kind]
	if !ok {
		return false
	}
	if at.LengthSq() <= w.tuning.SafeRadius*w.tuning.SafeRadius {
		return false
	}
	shape := newEntity("", ability.Wall.Kind, Vec{}, definition, EntityOverrides{})
	extent := shape.boundingRadius()
	for _, position := range wallSegments(at, direction, *ability.Wall) {
		if position.LengthSq() > w.tuning.WorldRadius*w.tuning.WorldRadius {
			return false
		}
		// The index pads a query by the widest body it holds, so asking for the
		// wall's own extent still visits everyone who could be standing in it.
		occupied := false
		w.bodies.near(position, extent, func(body *Player) bool {
			reach := extent + body.circleRadius()
			occupied = body.Alive && body.Position.Sub(position).LengthSq() < reach*reach
			return !occupied
		})
		if occupied {
			return false
		}
	}
	return true
}

// raiseWall materialises one caster's wall, dropping whatever it had standing.
// The segments are ordinary world items, so nothing downstream — collision,
// snapshots, projectile damage, destruction — needs a case for them.
func (w *World) raiseWall(ownerID string, at, direction Vec, wall tuning.Wall, now time.Time) {
	definition, ok := w.tuning.Tables.Entities[wall.Kind]
	if !ok {
		return
	}
	w.dropWall(ownerID, now)
	group := &wallGroup{OwnerID: ownerID, ExpiresAt: now.Add(wall.Duration())}
	for index, position := range wallSegments(at, direction, wall) {
		// A segment placed inside existing cover is skipped rather than refused:
		// the wall is still worth raising, and one segment overlapping a tree is
		// not a reason to waste the cast.
		if w.collides(position, 1) {
			continue
		}
		entity := newEntity(fmt.Sprintf("wall-%d-%d", w.nextWall, index), wall.Kind, position, definition, EntityOverrides{})
		entity.SpawnedAt = now
		group.Segments = append(group.Segments, &entity)
		w.addResidentItem(&entity)
	}
	w.nextWall++
	if len(group.Segments) == 0 {
		return
	}
	w.walls[ownerID] = group
}

// dropWall takes a caster's standing wall down through the shared graceful
// removal, so it fades from every client rather than blinking out.
func (w *World) dropWall(ownerID string, now time.Time) {
	group := w.walls[ownerID]
	if group == nil {
		return
	}
	for _, segment := range group.Segments {
		w.deleteWorldItem(segment, now)
	}
	delete(w.walls, ownerID)
}

// stepWalls retires expired walls and forgets the ones the world has already
// destroyed, so a caster whose wall was shot down may raise another.
func (w *World) stepWalls(now time.Time) {
	for _, ownerID := range sortedWallOwners(w.walls) {
		group := w.walls[ownerID]
		if !now.Before(group.ExpiresAt) {
			w.dropWall(ownerID, now)
			continue
		}
		standing := false
		for _, segment := range group.Segments {
			standing = standing || segment.Alive
		}
		if !standing {
			delete(w.walls, ownerID)
		}
	}
}

// Walls exposes standing player-authored terrain for tests and tooling.
func (w *World) Walls(ownerID string) []Entity {
	group := w.walls[ownerID]
	if group == nil {
		return nil
	}
	segments := make([]Entity, 0, len(group.Segments))
	for _, segment := range group.Segments {
		segments = append(segments, *segment)
	}
	return segments
}

func sortedWallOwners(walls map[string]*wallGroup) []string {
	owners := make([]string, 0, len(walls))
	for owner := range walls {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	return owners
}

// wallSpan is how wide a wall of this shape stands.
func wallSpan(wall tuning.Wall) float64 {
	return math.Max(0, float64(wall.Segments-1)) * wall.Spacing
}
