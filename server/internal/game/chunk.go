package game

import (
	"fmt"
	"math"
	"time"

	"spellfire/server/internal/tuning"
)

// The world is chunked because at radius 45,000 it cannot be resident. A chunk
// is one spatial-index cell of terrain, materialised from (world seed, chunk
// coordinate) the moment a body comes near it and dropped again when none is.
// Nothing about a chunk is stored: the same coordinate always produces the same
// items, so residency is a cache rather than state.
//
// Two rules keep the lifecycle from breaking contracts the rest of the
// simulation already relies on:
//
//   - A chunk holding an item the world has changed — damaged, or in its
//     graceful-removal fade — is pinned. Entity.Delete's fade and the rewind
//     window's Entity.presentAt history both assume an item outlives its own
//     removal, and regenerating a chunk would hand back a tree at full health.
//   - Destruction leaves a scar: the felled site is remembered by ID and never
//     regenerated, so a chunk that is evicted and comes back is the world the
//     players left rather than the world the seed describes.
//
// Runtime-authored terrain — authored fixtures, Mage walls, developer spawns —
// is never chunked at all. It lives in the resident set, which is the simplest
// way to satisfy the same contracts: a wall cannot be evicted out from under
// its own lifetime because eviction never sees it.

// worldChunk is one materialised cell of generated terrain.
type worldChunk struct {
	coord gridCell
	items []*Entity
}

// chunkLoadReach and chunkKeepReach bracket residency around a body: a chunk is
// materialised once a body is within the first and dropped only past the second,
// so a body walking a chunk edge does not thrash the generator.
//
// The load reach has to cover the furthest a viewer can ever see — the AOI
// half-extent widened by the largest scope in the tables — plus a margin, or a
// scoped player would look at ground that had not been generated. The keep reach
// adds a whole chunk of hysteresis on top of that.
func (w *World) chunkLoadReach() float64 {
	return w.tuning.AOIRadius + w.maxScopeBonus() + w.chunkSize/2
}

func (w *World) chunkKeepReach() float64 { return w.chunkLoadReach() + w.chunkSize }

// maxScopeBonus is the widest view any weapon in the tables can buy.
func (w *World) maxScopeBonus() float64 {
	bonus := 0.0
	for _, weapon := range w.tuning.Tables.Weapons {
		if weapon.Scope != nil {
			bonus = math.Max(bonus, weapon.Scope.ViewBonus)
		}
	}
	return bonus
}

// updateResidency materialises the chunks bodies have come near and drops the
// ones nobody is near any more. It runs once per tick: the cost is proportional
// to the number of bodies, never to the size of the world.
func (w *World) updateResidency() {
	if w.chunksFrozen {
		return
	}
	// Lingering and dead bodies count: a body still in the world still needs the
	// ground under it, and the snapshot its owner reconnects to is built from it.
	for _, id := range sortedPlayerIDs(w.players) {
		w.loadChunksAround(w.players[id].Position)
	}
	if len(w.chunks) == 0 {
		return
	}
	keep := w.chunkKeepReach()
	for _, coord := range sortedCells(w.chunks) {
		chunk := w.chunks[coord]
		if w.chunkPinned(chunk) || w.chunkNeeded(coord, keep) {
			continue
		}
		w.evictChunk(chunk)
	}
}

// loadChunksAround materialises every chunk within load reach of a point. It is
// also the seam a join uses before it tests whether a saved position is still
// standable: the ground has to exist before it can be asked about.
func (w *World) loadChunksAround(at Vec) {
	if w.chunksFrozen {
		return
	}
	reach := w.chunkLoadReach()
	lower, upper := w.terrain.cellAt(Vec{at.X - reach, at.Y - reach}), w.terrain.cellAt(Vec{at.X + reach, at.Y + reach})
	for y := lower.Y; y <= upper.Y; y++ {
		for x := lower.X; x <= upper.X; x++ {
			w.loadChunk(gridCell{x, y})
		}
	}
}

func (w *World) loadChunk(coord gridCell) {
	if _, resident := w.chunks[coord]; resident {
		return
	}
	chunk := &worldChunk{coord: coord, items: w.generateChunk(coord)}
	w.chunks[coord] = chunk
	for _, item := range chunk.items {
		w.terrain.insert(item)
	}
}

func (w *World) evictChunk(chunk *worldChunk) {
	for _, item := range chunk.items {
		w.terrain.remove(item)
	}
	delete(w.chunks, chunk.coord)
}

// chunkNeeded reports whether any body is close enough to keep a chunk.
func (w *World) chunkNeeded(coord gridCell, reach float64) bool {
	min := Vec{float64(coord.X) * w.chunkSize, float64(coord.Y) * w.chunkSize}
	max := Vec{min.X + w.chunkSize, min.Y + w.chunkSize}
	for _, p := range w.players {
		nearest := Vec{math.Max(min.X, math.Min(p.Position.X, max.X)), math.Max(min.Y, math.Min(p.Position.Y, max.Y))}
		if delta := nearest.Sub(p.Position); math.Abs(delta.X) <= reach && math.Abs(delta.Y) <= reach {
			return true
		}
	}
	return false
}

// chunkPinned reports whether a chunk holds state the seed cannot reproduce. A
// damaged item is the whole reason: dropping the chunk and regenerating it
// would repair it, and the player who spent a magazine on it would find it
// whole. A fading item is pinned for the same reason its fade exists.
func (w *World) chunkPinned(chunk *worldChunk) bool {
	for _, item := range chunk.items {
		if item.Deleting || item.Health != item.MaxHealth {
			return true
		}
	}
	return false
}

// generateChunk materialises one chunk's terrain from the world seed and the
// chunk's own coordinate — nothing else. Two consequences follow, and both are
// load-bearing: a chunk is identical however many times it is evicted and
// reloaded, and two chunks generated in either order never disagree about the
// item between them.
//
// Placement is a jittered lattice rather than rejection sampling. Rejection
// sampling needs to see everything already placed, which a chunk cannot: its
// neighbour may not exist yet. A lattice with bounded jitter needs to see
// nothing at all — the jitter is clamped so that two sites, in the same chunk or
// across a chunk edge, can never come closer than the declared spacing.
func (w *World) generateChunk(coord gridCell) []*Entity {
	terrain := w.tuning.Tables.World.Terrain
	definition, ok := w.tuning.Tables.Entities[terrain.Entity]
	if !ok || terrain.Cell <= 0 || terrain.Fill <= 0 {
		return nil
	}
	base := newEntity("", terrain.Entity, Vec{}, definition, EntityOverrides{})
	baseRadius := base.circleRadius()
	// The site may wander this far from its lattice point and still leave
	// Spacing between it and anything a neighbouring site could hold.
	jitter := math.Max(0, (terrain.Cell-terrain.Spacing-2*(baseRadius+terrain.RadiusSpread))/2)
	inner := w.tuning.SafeRadius + terrain.InnerMargin
	outer := w.tuning.WorldRadius - terrain.OuterMargin
	perChunk := int64(math.Round(w.chunkSize / terrain.Cell))
	if perChunk < 1 {
		perChunk = 1
	}
	items := make([]*Entity, 0, perChunk*perChunk)
	for sy := int64(coord.Y) * perChunk; sy < int64(coord.Y+1)*perChunk; sy++ {
		for sx := int64(coord.X) * perChunk; sx < int64(coord.X+1)*perChunk; sx++ {
			draw := newSiteStream(terrain.Seed, sx, sy)
			if draw.next() >= terrain.Fill {
				continue
			}
			position := Vec{
				(float64(sx)+0.5)*terrain.Cell + (draw.next()*2-1)*jitter,
				(float64(sy)+0.5)*terrain.Cell + (draw.next()*2-1)*jitter,
			}
			// Terrain starts InnerMargin outside safety and stops OuterMargin
			// short of the rim, so neither the hub nor the world edge is walled in.
			if distance := math.Sqrt(position.LengthSq()); distance < inner || distance > outer {
				continue
			}
			radius := baseRadius + draw.next()*terrain.RadiusSpread
			id := fmt.Sprintf("%s-%d:%d", terrain.Entity, sx, sy)
			// A felled site stays felled: without the scar an evicted chunk would
			// hand back everything the players had cleared.
			if w.scars[id] {
				continue
			}
			// Authored fixtures are the one thing generation defers to, and they
			// are read from the table rather than from the world, so deferring to
			// them stays independent of what is currently resident.
			if w.fixtureOverlaps(position, radius+terrain.Spacing) {
				continue
			}
			objects := collisionObjectsFromTuning(definition.CollisionObjects)
			if len(objects) > 0 {
				objects[0].Radius = radius
			}
			entity := newEntity(id, terrain.Entity, position, definition, EntityOverrides{CollisionObjects: &objects})
			items = append(items, &entity)
		}
	}
	return items
}

// fixtureOverlaps tests a candidate against the authored fixtures. The table is
// the source rather than the world, so a chunk generated before or after a
// fixture's neighbours makes the same decision.
func (w *World) fixtureOverlaps(position Vec, radius float64) bool {
	for _, fixture := range w.tuning.Tables.World.Fixtures {
		definition, ok := w.tuning.Tables.Entities[fixture.Entity]
		if !ok {
			continue
		}
		at := Vec{fixture.Position[0], fixture.Position[1]}
		reach := radius + entityExtent(definition)
		if at.Sub(position).LengthSq() < reach*reach {
			return true
		}
	}
	return false
}

// entityExtent is how far an archetype's geometry reaches from its position.
func entityExtent(definition tuning.EntityDefinition) float64 {
	shape := newEntity("", "", Vec{}, definition, EntityOverrides{})
	return shape.boundingRadius()
}

// siteStream is the per-site deterministic draw sequence. It is seeded from the
// world seed and the site's lattice coordinates alone, so a site's item is the
// same whichever chunk pass produced it and whatever else the world holds.
type siteStream struct{ state uint64 }

func newSiteStream(seed uint64, x, y int64) *siteStream {
	state := seed ^ (uint64(x)+0x9e3779b97f4a7c15)*0xbf58476d1ce4e5b9
	state = (state ^ (state >> 29)) ^ (uint64(y)+0x94d049bb133111eb)*0xc2b2ae3d27d4eb4f
	return &siteStream{state: state}
}

func (s *siteStream) next() float64 {
	s.state += 0x9e3779b97f4a7c15
	value := s.state
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	value ^= value >> 31
	return float64(value>>11) / float64(uint64(1)<<53)
}

// residentItems materialises the authored fixtures. They are placed once at
// construction and never evicted: singular geography is not something a seed
// reproduces, and there is far too little of it to be worth chunking.
func (w *World) placeFixtures() {
	for _, fixture := range w.tuning.Tables.World.Fixtures {
		definition, ok := w.tuning.Tables.Entities[fixture.Entity]
		if !ok {
			continue
		}
		entity := newEntity(fixture.ID, fixture.Entity, Vec{fixture.Position[0], fixture.Position[1]}, definition, EntityOverrides{})
		w.addResidentItem(&entity)
	}
}

// addResidentItem puts a runtime-authored entity — a Mage wall, a developer
// spawn — into the never-evicted set and the spatial index.
func (w *World) addResidentItem(entity *Entity) {
	w.resident[entity.ID] = entity
	w.terrain.insert(entity)
}

// deleteWorldItem starts an item's graceful removal and records it for the
// reaper. A generated item also leaves a scar, so the chunk it belonged to may
// be evicted later without the world growing its trees back.
func (w *World) deleteWorldItem(item *Entity, now time.Time) {
	if item.Deleting {
		return
	}
	item.Delete(now)
	w.deleting = append(w.deleting, item)
	if _, authored := w.resident[item.ID]; !authored {
		w.scars[item.ID] = true
	}
}

// reapWorldItems collects the items whose fade has finished. It sweeps the
// pending list rather than the world, so the cost follows what was actually
// destroyed instead of how much terrain is resident.
func (w *World) reapWorldItems(now time.Time) {
	kept := w.deleting[:0]
	for _, item := range w.deleting {
		if !item.deleteComplete(now) {
			kept = append(kept, item)
			continue
		}
		w.removeWorldItem(item)
	}
	w.deleting = kept
}

// removeWorldItem takes an item out of the index and out of whatever holds it.
func (w *World) removeWorldItem(item *Entity) {
	w.terrain.remove(item)
	delete(w.resident, item.ID)
	if chunk := w.chunks[w.chunkOf(item.Position)]; chunk != nil {
		for index, candidate := range chunk.items {
			if candidate == item {
				chunk.items = append(chunk.items[:index], chunk.items[index+1:]...)
				break
			}
		}
	}
}

func (w *World) chunkOf(at Vec) gridCell { return w.terrain.cellAt(at) }

// terrainItem finds one piece of terrain by ID. Authored geography answers
// directly; generated terrain falls back to a sweep of what is resident, which
// is the developer inspector's path rather than a simulation one.
func (w *World) terrainItem(id string) (*Entity, bool) {
	if item := w.resident[id]; item != nil {
		return item, true
	}
	var found *Entity
	w.terrain.all(func(item *Entity) bool {
		if item.ID == id {
			found = item
		}
		return found == nil
	})
	return found, found != nil
}

// setWorldItems replaces every piece of terrain with an authored set and takes
// the chunk generator out of the picture. It is the test and tooling seam:
// nothing in the simulation replaces the world wholesale.
func (w *World) setWorldItems(items ...*Entity) {
	w.chunks = make(map[gridCell]*worldChunk)
	w.scars = make(map[string]bool)
	w.resident = make(map[string]*Entity)
	w.deleting = nil
	w.terrain = newSpatialGrid[*Entity](w.chunkSize)
	w.chunksFrozen = true
	for _, item := range items {
		item.indexed = false
		w.addResidentItem(item)
	}
}
