package game

import (
	"container/heap"
	"math"
	"sort"
	"testing"
)

// The traversal target is the reason Phase 3.2 exists: the world must be a
// five-minute journey on foot even though a straight radial line is under three.
// These tests measure that over the live macro structure rather than trusting the
// parameters, building a walkable grid from the belt predicate and running a
// shortest-path search across it.

// walkGrid is a coarse sampling of where a body may stand. A cell is walkable if
// it is inside the world and outside every ridge belt (or inside a pass).
type walkGrid struct {
	step   float64
	half   int // cells from the centre to the rim on each axis
	radius float64
	walk   []bool
}

func buildWalkGrid(w *World, step float64) *walkGrid {
	radius := w.tuning.WorldRadius
	half := int(math.Ceil(radius/step)) + 1
	side := 2*half + 1
	g := &walkGrid{step: step, half: half, radius: radius, walk: make([]bool, side*side)}
	for j := -half; j <= half; j++ {
		for i := -half; i <= half; i++ {
			pos := Vec{float64(i) * step, float64(j) * step}
			if pos.LengthSq() <= radius*radius && w.walkableAt(pos) {
				g.walk[g.index(i, j)] = true
			}
		}
	}
	return g
}

func (g *walkGrid) side() int          { return 2*g.half + 1 }
func (g *walkGrid) index(i, j int) int { return (j+g.half)*g.side() + (i + g.half) }
func (g *walkGrid) walkable(i, j int) bool {
	if i < -g.half || i > g.half || j < -g.half || j > g.half {
		return false
	}
	return g.walk[g.index(i, j)]
}
func (g *walkGrid) at(x, y float64) (int, int) {
	return int(math.Round(x / g.step)), int(math.Round(y / g.step))
}

// dijkstra returns the shortest walkable distance from the origin cell to every
// cell, in world units, with unreached cells left at +Inf.
func (g *walkGrid) dijkstra() []float64 {
	side := g.side()
	dist := make([]float64, side*side)
	for i := range dist {
		dist[i] = math.Inf(1)
	}
	start := g.index(0, 0)
	dist[start] = 0
	pq := &cellHeap{{node: start, cost: 0}}
	neighbours := [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	for pq.Len() > 0 {
		top := heap.Pop(pq).(cellItem)
		if top.cost > dist[top.node] {
			continue
		}
		ci, cj := top.node%side-g.half, top.node/side-g.half
		for _, d := range neighbours {
			ni, nj := ci+d[0], cj+d[1]
			if !g.walkable(ni, nj) {
				continue
			}
			step := g.step
			if d[0] != 0 && d[1] != 0 {
				step *= math.Sqrt2
			}
			next := g.index(ni, nj)
			if nd := top.cost + step; nd < dist[next] {
				dist[next] = nd
				heap.Push(pq, cellItem{node: next, cost: nd})
			}
		}
	}
	return dist
}

type cellItem struct {
	node int
	cost float64
}
type cellHeap []cellItem

func (h cellHeap) Len() int           { return len(h) }
func (h cellHeap) Less(a, b int) bool { return h[a].cost < h[b].cost }
func (h cellHeap) Swap(a, b int)      { h[a], h[b] = h[b], h[a] }
func (h *cellHeap) Push(x any)        { *h = append(*h, x.(cellItem)) }
func (h *cellHeap) Pop() any {
	old := *h
	item := old[len(old)-1]
	*h = old[:len(old)-1]
	return item
}

// The executable form of the traversal target: the shortest on-foot path from
// the hub to the rim, sampled across many bearings, has a median of at least
// five minutes at base speed. A straight radial line at 260 u/s is under three.
func TestOnFootJourneyToTheRimIsAtLeastFiveMinutes(t *testing.T) {
	w, _ := scaleWorld(t)
	const step = 150.0
	grid := buildWalkGrid(w, step)
	dist := grid.dijkstra()

	speed := w.tuning.PlayerSpeed
	target := grid.radius - w.tuning.Tables.World.Terrain.OuterMargin - 2*step
	journeys := make([]float64, 0, 72)
	for a := 0; a < 72; a++ {
		angle := float64(a) / 72 * 2 * math.Pi
		i, j := grid.at(math.Cos(angle)*target, math.Sin(angle)*target)
		best := math.Inf(1)
		// The rim cell itself may fall on a barrier or just outside the disc;
		// take the nearest reached cell in a small neighbourhood.
		for dj := -1; dj <= 1; dj++ {
			for di := -1; di <= 1; di++ {
				if d := dist[grid.index(i+di, j+dj)]; d < best {
					best = d
				}
			}
		}
		if math.IsInf(best, 1) {
			t.Fatalf("the rim at bearing %.0f° is not reachable on foot at all", angle*180/math.Pi)
		}
		journeys = append(journeys, best/speed)
	}
	sort.Float64s(journeys)
	median := journeys[len(journeys)/2]
	t.Logf("on-foot rim journeys: min %.0fs, median %.0fs, max %.0fs (straight line ~%.0fs)",
		journeys[0], median, journeys[len(journeys)-1], target/speed)
	if median < 300 {
		t.Fatalf("median on-foot journey to the rim is %.0fs, under the 300s (5 min) target", median)
	}
}

// The other half of the target: the length is bought with terrain, not a wall
// around the map, so no straight radial line is clear from the hub to the rim.
func TestNoStraightRadialCorridorReachesTheRim(t *testing.T) {
	w, _ := scaleWorld(t)
	inner := w.tuning.Tables.World.SpawnRadius
	outer := w.tuning.WorldRadius - w.tuning.Tables.World.Terrain.OuterMargin
	for a := 0; a < 720; a++ {
		angle := float64(a) / 720 * 2 * math.Pi
		blocked := false
		for r := inner; r <= outer; r += 40 {
			if !w.walkableAt(Vec{math.Cos(angle) * r, math.Sin(angle) * r}) {
				blocked = true
				break
			}
		}
		if !blocked {
			t.Fatalf("a straight radial line at %.2f° is clear from hub to rim; the belts are not funnelling travel", angle*180/math.Pi)
		}
	}
}

// Macro structure that funnels travel must never seal it: the walkable space is
// one connected region, so no biome, spawn point, or (future) outpost is walled
// off behind a ring with no pass. Every walkable cell must be reachable from the
// hub.
func TestGeneratedTerrainNeverSealsAWalkableRegion(t *testing.T) {
	w, _ := scaleWorld(t)
	const step = 150.0
	grid := buildWalkGrid(w, step)
	dist := grid.dijkstra()

	// Every cell the field calls walkable must actually be reached; an unreached
	// walkable cell is an enclosed pocket. A one-cell margin from the rim avoids
	// counting slivers the coarse grid clips against the circular boundary.
	stranded, total := 0, 0
	rim := grid.radius - step
	for j := -grid.half; j <= grid.half; j++ {
		for i := -grid.half; i <= grid.half; i++ {
			pos := Vec{float64(i) * step, float64(j) * step}
			if pos.LengthSq() > rim*rim || !grid.walkable(i, j) {
				continue
			}
			total++
			if math.IsInf(dist[grid.index(i, j)], 1) {
				stranded++
			}
		}
	}
	t.Logf("connectivity: %d of %d walkable cells reachable from the hub", total-stranded, total)
	if stranded > 0 {
		t.Fatalf("%d of %d walkable cells are sealed off from the hub", stranded, total)
	}
}

// Every biome region must be reachable from the hub, since aligned materials are
// gated to their region and a walled-off biome would hard-lock a build the world
// field went to lengths to keep reachable. Sampled across the disc, the biome at
// every reachable-adjacent sample is itself reachable.
func TestEveryBiomeRegionIsReachableFromTheHub(t *testing.T) {
	w, _ := scaleWorld(t)
	const step = 150.0
	grid := buildWalkGrid(w, step)
	dist := grid.dijkstra()

	seen := map[string]bool{}
	reached := map[string]bool{}
	for j := -grid.half; j <= grid.half; j += 2 {
		for i := -grid.half; i <= grid.half; i += 2 {
			if !grid.walkable(i, j) {
				continue
			}
			pos := Vec{float64(i) * step, float64(j) * step}
			biome := w.tuning.Field.BiomeAt(pos.X, pos.Y).ID
			if biome == "" {
				continue
			}
			seen[biome] = true
			if !math.IsInf(dist[grid.index(i, j)], 1) {
				reached[biome] = true
			}
		}
	}
	for biome := range seen {
		if !reached[biome] {
			t.Fatalf("biome %q has walkable ground but none of it is reachable from the hub", biome)
		}
	}
	if len(reached) < len(w.tuning.Tables.Biomes) {
		t.Fatalf("only %d of %d biomes were sampled as reachable", len(reached), len(w.tuning.Tables.Biomes))
	}
}
