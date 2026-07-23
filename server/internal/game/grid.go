package game

import (
	"math"
	"sort"
)

// The uniform spatial grid is the world's one broad-phase answer. Every query
// that used to walk a flat slice — movement collision, projectile sweeps,
// snapshot interest, occluder collection, and automatic target acquisition —
// reaches it instead, so a world large enough to hold tens of thousands of
// colliders costs each of those queries only what is actually near the asker.
//
// One grid module, one cell size, five buckets: terrain, players, projectiles,
// telegraphs, and fields each get their own cell map so a query never has to
// filter a mixed bucket by type. Visibility is deliberately not a second
// answer: collectOccluders draws from the same terrain and field buckets the
// collision and snapshot paths do.
//
// Cell size is the world's chunk size (≈ the AOI half-extent), which is what
// makes chunk residency and index buckets the same coordinate space.

// gridCell is an integer cell coordinate. Cells are addressed rather than
// stored in a dense array so an empty region of a 45,000-unit world costs
// nothing.
type gridCell struct{ X, Y int32 }

// gridMember is anything the index holds. Every world family embeds Entity, so
// the promoted method satisfies this without a per-family adapter.
type gridMember interface {
	comparable
	indexEntity() *Entity
}

func (e *Entity) indexEntity() *Entity { return e }

// indexExtent is how far a member reaches past its own position. Collision
// geometry answers it for everything materialised; a field, which is an area
// rather than a body, declares QueryExtent instead.
func indexExtent(e *Entity) float64 {
	if e.QueryExtent > 0 {
		return e.QueryExtent
	}
	return e.boundingRadius()
}

type spatialGrid[T gridMember] struct {
	size float64
	// extent is the largest reach any member has had. Members are bucketed by
	// their position alone — never smeared across every cell they overlap — so a
	// query widens its cell range by this instead, which keeps membership
	// single-valued and iteration duplicate-free.
	extent float64
	cells  map[gridCell][]T
}

func newSpatialGrid[T gridMember](size float64) *spatialGrid[T] {
	if size <= 0 {
		size = 1
	}
	return &spatialGrid[T]{size: size, cells: make(map[gridCell][]T)}
}

func (g *spatialGrid[T]) cellAt(position Vec) gridCell {
	return gridCell{int32(math.Floor(position.X / g.size)), int32(math.Floor(position.Y / g.size))}
}

func (g *spatialGrid[T]) insert(member T) {
	entity := member.indexEntity()
	if entity.indexed {
		g.update(member)
		return
	}
	cell := g.cellAt(entity.Position)
	g.cells[cell] = append(g.cells[cell], member)
	entity.cell, entity.indexed = cell, true
	g.extent = math.Max(g.extent, indexExtent(entity))
}

func (g *spatialGrid[T]) remove(member T) {
	entity := member.indexEntity()
	if !entity.indexed {
		return
	}
	bucket := g.cells[entity.cell]
	for index, candidate := range bucket {
		if candidate == member {
			// Order-preserving removal: a bucket's order is what makes a query's
			// results reproducible, and the buckets are short enough that the copy
			// is cheaper than the determinism it would cost to swap.
			g.cells[entity.cell] = append(bucket[:index], bucket[index+1:]...)
			break
		}
	}
	if len(g.cells[entity.cell]) == 0 {
		delete(g.cells, entity.cell)
	}
	entity.indexed = false
}

// update re-buckets a member that has moved. A body crossing a cell boundary is
// rare at this cell size — a running player needs seconds — so movement costs a
// comparison per tick and nothing else.
func (g *spatialGrid[T]) update(member T) {
	entity := member.indexEntity()
	if !entity.indexed {
		g.insert(member)
		return
	}
	if cell := g.cellAt(entity.Position); cell != entity.cell {
		g.remove(member)
		g.insert(member)
	}
}

// moved re-buckets a member only if the index already holds it. It is what a
// movement path wants: a projectile being fast-forwarded through the rewind
// window is not in the world yet, and may never be, so moving it must not be
// what puts it there.
func (g *spatialGrid[T]) moved(member T) {
	if member.indexEntity().indexed {
		g.update(member)
	}
}

// each visits every member whose cell can reach the rectangle, in a fixed cell
// order so two identical queries always visit in the same sequence. Visiting
// stops when visit reports false.
func (g *spatialGrid[T]) each(min, max Vec, visit func(T) bool) {
	if len(g.cells) == 0 {
		return
	}
	min, max = Vec{min.X - g.extent, min.Y - g.extent}, Vec{max.X + g.extent, max.Y + g.extent}
	lower, upper := g.cellAt(min), g.cellAt(max)
	for y := lower.Y; y <= upper.Y; y++ {
		for x := lower.X; x <= upper.X; x++ {
			for _, member := range g.cells[gridCell{x, y}] {
				if !visit(member) {
					return
				}
			}
		}
	}
}

// near visits everything within reach of a point.
func (g *spatialGrid[T]) near(at Vec, reach float64, visit func(T) bool) {
	g.each(Vec{at.X - reach, at.Y - reach}, Vec{at.X + reach, at.Y + reach}, visit)
}

// along visits everything within pad of a segment, using the segment's bounding
// box. A swept query is rare enough — one per projectile per tick — that the
// box is worth more than a walked supercover would save.
func (g *spatialGrid[T]) along(from, to Vec, pad float64, visit func(T) bool) {
	min := Vec{math.Min(from.X, to.X) - pad, math.Min(from.Y, to.Y) - pad}
	max := Vec{math.Max(from.X, to.X) + pad, math.Max(from.Y, to.Y) + pad}
	g.each(min, max, visit)
}

// all visits every member the index holds. It is for lifecycle sweeps and
// tooling, never for a per-viewer or per-projectile path.
func (g *spatialGrid[T]) all(visit func(T) bool) {
	for _, cell := range sortedCells(g.cells) {
		for _, member := range g.cells[cell] {
			if !visit(member) {
				return
			}
		}
	}
}

func (g *spatialGrid[T]) len() int {
	total := 0
	for _, bucket := range g.cells {
		total += len(bucket)
	}
	return total
}

// Membership is deliberately funnelled through these helpers rather than left
// to the maps: every family lives in its own keyed store *and* in the index, and
// the two must never disagree about what is in the world.

func (w *World) addProjectile(projectile *Projectile) {
	w.projectiles[projectile.ID] = projectile
	w.shots.insert(projectile)
}

func (w *World) removeProjectile(id string) {
	if projectile := w.projectiles[id]; projectile != nil {
		w.shots.remove(projectile)
		delete(w.projectiles, id)
	}
}

func (w *World) addTelegraph(telegraph *Telegraph) {
	w.telegraphs[telegraph.ID] = telegraph
	w.warnings.insert(telegraph)
}

func (w *World) removeTelegraph(id string) {
	if telegraph := w.telegraphs[id]; telegraph != nil {
		w.warnings.remove(telegraph)
		delete(w.telegraphs, id)
	}
}

func (w *World) addDeployable(deployable *Deployable) {
	w.deployables[deployable.ID] = deployable
	w.fieldGrid.insert(deployable)
}

func (w *World) removeDeployable(id string) {
	if deployable := w.deployables[id]; deployable != nil {
		w.fieldGrid.remove(deployable)
		delete(w.deployables, id)
	}
}

// sortedCells orders cells so a full sweep is reproducible even though the
// backing map is not.
func sortedCells[V any](cells map[gridCell]V) []gridCell {
	ordered := make([]gridCell, 0, len(cells))
	for cell := range cells {
		ordered = append(ordered, cell)
	}
	sort.Slice(ordered, func(i, j int) bool { return cellBefore(ordered[i], ordered[j]) })
	return ordered
}

func cellBefore(a, b gridCell) bool {
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.X < b.X
}
