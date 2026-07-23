package game

import (
	"math"
	"strings"
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

// scaleWorld is the shipped world at its real size: 45,000 units, chunked
// terrain, nothing overridden. The compact arena testWorld() builds is the wrong
// instrument for anything about scale.
func scaleWorld(t *testing.T) (*World, time.Time) {
	t.Helper()
	return NewWorld(DefaultTuning()), time.Unix(1_700_000_000, 0)
}

// A world this size cannot be resident, so it must not be: an untouched world
// holds its authored fixtures and nothing else, and terrain arrives around the
// bodies that come near it.
func TestWorldIsNeverFullyResident(t *testing.T) {
	w, now := scaleWorld(t)
	if items := w.WorldItems(); len(items) != len(w.tuning.Tables.World.Fixtures) {
		t.Fatalf("an empty world holds %d items, want only its %d fixtures", len(items), len(w.tuning.Tables.World.Fixtures))
	}
	addTestPlayer(w, "p", model.Gunslinger, Vec{20_000, 0}, now)
	w.Step(now)
	near, far := 0, 0
	for _, item := range w.WorldItems() {
		if math.Abs(item.Position.X-20_000) <= w.chunkKeepReach() && math.Abs(item.Position.Y) <= w.chunkKeepReach() {
			near++
			continue
		}
		if item.Kind == "tree" {
			far++
		}
	}
	if near == 0 {
		t.Fatal("no terrain materialised around a body in the Frontier")
	}
	if far > 0 {
		t.Fatalf("%d generated items are resident nowhere near a body", far)
	}
}

// The seed is the whole world: a chunk that has been evicted and comes back must
// be the chunk that left, or nothing downstream — collision prediction, cover,
// the map a player learned — can be trusted.
func TestChunkGenerationIsDeterministicAndOrderIndependent(t *testing.T) {
	w, _ := scaleWorld(t)
	other, _ := scaleWorld(t)
	coords := []gridCell{{4, 3}, {-7, 12}, {0, -2}, {25, 25}}
	for _, coord := range coords {
		first, second := w.generateChunk(coord), w.generateChunk(coord)
		if len(first) != len(second) {
			t.Fatalf("chunk %v generated %d items and then %d", coord, len(first), len(second))
		}
		for index := range first {
			if first[index].ID != second[index].ID || first[index].Position != second[index].Position {
				t.Fatalf("chunk %v item %d differs between generations: %#v vs %#v", coord, index, first[index], second[index])
			}
		}
	}
	// The same chunks, generated in the opposite order in a different world, are
	// still the same chunks: generation never reads what is already resident.
	for index := len(coords) - 1; index >= 0; index-- {
		mine, theirs := w.generateChunk(coords[index]), other.generateChunk(coords[index])
		if len(mine) != len(theirs) {
			t.Fatalf("chunk %v depends on generation order: %d vs %d items", coords[index], len(mine), len(theirs))
		}
	}
}

// The jittered lattice is what replaces rejection sampling, and this is the
// property it buys: two scatter items can never overlap, in the same chunk or
// across a chunk edge, without either chunk having to see the other. Belt items
// are the deliberate exception — a ridge is meant to be a solid overlapping mass
// — so they are excluded from the no-overlap check.
func TestGeneratedTerrainNeverOverlapsAcrossChunkEdges(t *testing.T) {
	w, _ := scaleWorld(t)
	spacing := w.tuning.Tables.World.Terrain.Spacing
	items := make([]*Entity, 0, 512)
	for y := int32(-6); y <= 6; y++ {
		for x := int32(3); x <= 14; x++ {
			for _, item := range w.generateChunk(gridCell{x, y}) {
				if !strings.HasPrefix(item.ID, "belt-") {
					items = append(items, item)
				}
			}
		}
	}
	if len(items) < 50 {
		t.Fatalf("the sampled region generated only %d items; the density test is meaningless", len(items))
	}
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			gap := math.Sqrt(items[i].Position.Sub(items[j].Position).LengthSq()) - items[i].circleRadius() - items[j].circleRadius()
			if gap < spacing-1e-9 {
				t.Fatalf("%s and %s are %g apart, closer than the %g spacing", items[i].ID, items[j].ID, gap, spacing)
			}
		}
	}
}

// Eviction is a cache decision and must never be a world decision. A chunk
// holding damage cannot be dropped, because regenerating it would repair it —
// and a felled tree must stay felled once its chunk does come and go.
func TestChunkEvictionPinsDamageAndRemembersDestruction(t *testing.T) {
	w, now := scaleWorld(t)
	p := addTestPlayer(w, "p", model.Gunslinger, Vec{6_000, 0}, now)
	w.Step(now)
	var damaged, doomed *Entity
	for _, item := range w.WorldItems() {
		if item.Kind != "tree" {
			continue
		}
		live := w.resolveItem(t, item.ID)
		if damaged == nil {
			damaged = live
			continue
		}
		if doomed == nil && live != damaged {
			doomed = live
			break
		}
	}
	if damaged == nil || doomed == nil {
		t.Fatal("the body's surroundings hold too little terrain to test eviction")
	}
	damagedChunk, doomedID := w.chunkOf(damaged.Position), doomed.ID
	damaged.TakeDamage(100)
	w.deleteWorldItem(doomed, now)

	// The body walks far enough that nothing here is needed any more.
	w.SetPlayerPosition(p.ID, Vec{30_000, 0}, now)
	for i := 1; i <= 60; i++ {
		w.Step(now.Add(time.Duration(i) * time.Second))
	}
	if _, resident := w.chunks[damagedChunk]; !resident {
		t.Fatal("a chunk holding a damaged item was evicted; reloading it would repair the damage")
	}
	if !w.scars[doomedID] {
		t.Fatalf("%s was destroyed but left no scar", doomedID)
	}

	// Coming back must not restore what was cleared.
	w.SetPlayerPosition(p.ID, Vec{6_000, 0}, now)
	w.Step(now.Add(2 * time.Minute))
	for _, item := range w.WorldItems() {
		if item.ID == doomedID {
			t.Fatalf("%s grew back when its chunk was reloaded", doomedID)
		}
	}
	if live := w.resolveItem(t, damaged.ID); live.Health != damaged.MaxHealth-100 {
		t.Fatalf("damaged item came back at %g health, want %g", live.Health, damaged.MaxHealth-100)
	}
}

// resolveItem finds a live terrain entity by ID, failing the test when it is not
// in the world.
func (w *World) resolveItem(t *testing.T, id string) *Entity {
	t.Helper()
	item, ok := w.terrainItem(id)
	if !ok {
		t.Fatalf("%s is not resident", id)
	}
	return item
}

// The index is an optimisation, so it has to answer exactly what the scan it
// replaced did. This holds it against a brute-force pass over everything
// resident, for collision and for snapshot interest alike.
func TestSpatialIndexAgreesWithABruteForceScan(t *testing.T) {
	w, now := scaleWorld(t)
	addTestPlayer(w, "p", model.Gunslinger, Vec{12_000, 3_000}, now)
	w.Step(now)
	items := w.WorldItems()
	if len(items) < 20 {
		t.Fatalf("only %d items resident; the comparison is meaningless", len(items))
	}
	radius := w.tuning.PlayerRadius
	for _, item := range items {
		for _, probe := range []Vec{item.Position, item.Position.Add(Vec{item.boundingRadius() + radius + 1, 0})} {
			want := false
			for index := range items {
				if items[index].intersectsCircle(probe, radius) {
					want = true
					break
				}
			}
			if got := w.collides(probe, radius); got != want {
				t.Fatalf("collides(%v) = %v, brute force says %v", probe, got, want)
			}
		}
	}
	viewer := Vec{12_000, 3_000}
	reach := w.tuning.AOIRadius
	want := 0
	for _, item := range items {
		extent := item.boundingRadius()
		if math.Abs(item.Position.X-viewer.X) <= reach+extent && math.Abs(item.Position.Y-viewer.Y) <= reach+extent {
			want++
		}
	}
	snapshot := w.SnapshotFor("p", now, protocol.ServerSnapshot)
	got := 0
	for _, entity := range snapshot.Entities {
		if entity.Type == protocol.EntityWorldItem {
			got++
		}
	}
	if got != want {
		t.Fatalf("snapshot carried %d world items, brute force says %d", got, want)
	}
}

// The index and the keyed stores are two views of one world, and they must
// never disagree. The case that made this worth asserting is a round resolved
// inside the rewind window: it is fast-forwarded before it is inserted, so
// moving it must not be what puts it in the index — and if it never enters the
// world it must not be left there either.
func TestIndexMembershipMatchesTheWorld(t *testing.T) {
	w, now := testWorld()
	shooter := carrying(t, w, addTestPlayer(w, "shooter", model.Gunslinger, Vec{1200, 0}, now), "starter-rifle")
	// The body is far enough that the round crosses several fast-forward
	// substeps before it lands, and close enough that it lands inside the rewind
	// window: it therefore moves repeatedly and is then thrown away, which is
	// exactly the sequence that must leave nothing behind in the index.
	target := addTestPlayer(w, "target", model.Mage, Vec{1320, 0}, now)
	for tick := 1; tick <= 120; tick++ {
		at := now.Add(time.Duration(tick) * time.Second / 60)
		w.ApplyInput(shooter.ID, protocol.Input{Sequence: uint32(tick), Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(at.Add(-w.tuning.MaxRewind).UnixMilli())})
		w.Step(at)
		if got, want := w.shots.len(), len(w.projectiles); got != want {
			t.Fatalf("tick %d: index holds %d projectiles, the world holds %d", tick, got, want)
		}
		if got, want := w.bodies.len(), len(w.players); got != want {
			t.Fatalf("tick %d: index holds %d bodies, the world holds %d", tick, got, want)
		}
		if got, want := w.warnings.len(), len(w.telegraphs); got != want {
			t.Fatalf("tick %d: index holds %d telegraphs, the world holds %d", tick, got, want)
		}
		if got, want := w.fieldGrid.len(), len(w.deployables); got != want {
			t.Fatalf("tick %d: index holds %d fields, the world holds %d", tick, got, want)
		}
	}
	if target.Health == target.MaxHealth {
		t.Fatal("the rewound shots never landed; the invariant was not actually exercised")
	}
}

// Every radius-derived constant has to survive the new scale together: the spawn
// ring must sit inside the hub, terrain must start outside safety and stop short
// of the rim, and the fixtures must still be in the world.
func TestRadiusDerivedConstantsHoldAtTheNewScale(t *testing.T) {
	w, _ := scaleWorld(t)
	table := w.tuning.Tables.World
	if table.SpawnRadius >= w.tuning.SafeRadius {
		t.Fatalf("spawn ring %g is not inside the %g safe radius", table.SpawnRadius, w.tuning.SafeRadius)
	}
	if w.chunkSize < w.tuning.AOIRadius {
		t.Fatalf("chunk size %g is smaller than the %g AOI half-extent; a viewer would see past resident ground", w.chunkSize, w.tuning.AOIRadius)
	}
	inner := w.tuning.SafeRadius + table.Terrain.InnerMargin
	outer := w.tuning.WorldRadius - table.Terrain.OuterMargin
	for _, coord := range []gridCell{{0, 0}, {1, 0}, {20, 20}, {-37, 0}} {
		for _, item := range w.generateChunk(coord) {
			distance := math.Sqrt(item.Position.LengthSq())
			if distance < inner || distance > outer {
				t.Fatalf("%s generated at %g, outside the [%g, %g] terrain band", item.ID, distance, inner, outer)
			}
		}
	}
	for _, fixture := range table.Fixtures {
		if math.Hypot(fixture.Position[0], fixture.Position[1]) > w.tuning.WorldRadius {
			t.Fatalf("fixture %q is outside the world", fixture.ID)
		}
	}
}

// The load test the scale substrate owes: 50 and 100 concurrent bodies against
// the real terrain density, measured through the production encoder and held to
// the same 64 KiB per-snapshot guardrail the protocol suite enforces.
func TestConcurrentLoadHoldsTheSnapshotBudget(t *testing.T) {
	const budget = 64 * 1024
	for _, count := range []int{50, 100} {
		w, now := scaleWorld(t)
		// Bodies are packed into one battle-density cluster rather than spread
		// over the world: a snapshot is only ever as large as the busiest view.
		for index := 0; index < count; index++ {
			angle := float64(index) * 2.399963
			reach := 400 * math.Sqrt(float64(index)/float64(count))
			addTestPlayer(w, playerID(index), model.Gunslinger, Vec{9_500 + math.Cos(angle)*reach, math.Sin(angle) * reach}, now)
		}
		for tick := 1; tick <= 30; tick++ {
			at := now.Add(time.Duration(tick) * time.Second / 60)
			for index := 0; index < count; index++ {
				w.ApplyInput(playerID(index), protocol.Input{Sequence: uint32(tick), Buttons: ButtonRight | ButtonFire, AimX: 1, ClientTimeMS: uint64(at.UnixMilli())})
			}
			w.Step(at)
		}
		largest, total := 0, 0
		for index := 0; index < count; index++ {
			size := len(protocol.EncodeServer(w.SnapshotFor(playerID(index), now, protocol.ServerSnapshot)))
			total += size
			if size > largest {
				largest = size
			}
		}
		t.Logf("%d bodies: %d resident items, largest snapshot %d bytes, mean %d bytes (%d bytes/s per client at 20 Hz)",
			count, w.terrain.len(), largest, total/count, largest*w.tuning.SendRate)
		if largest > budget {
			t.Fatalf("%d bodies produced a %d-byte snapshot, over the %d-byte budget", count, largest, budget)
		}
	}
}

// The other half of the load picture: a population spread across the Frontier
// is what actually exercises residency, and the world must stay a small fraction
// of itself however far apart the bodies are.
func TestSpreadPopulationKeepsResidencyBounded(t *testing.T) {
	w, now := scaleWorld(t)
	const count = 100
	for index := 0; index < count; index++ {
		angle := float64(index) * 2.399963
		reach := 10_000 + 15_000*float64(index)/float64(count)
		addTestPlayer(w, playerID(index), model.Gunslinger, Vec{math.Cos(angle) * reach, math.Sin(angle) * reach}, now)
	}
	started := time.Now()
	for tick := 1; tick <= 60; tick++ {
		w.Step(now.Add(time.Duration(tick) * time.Second / 60))
	}
	elapsed := time.Since(started) / 60
	chunks, items := len(w.chunks), w.terrain.len()
	// One chunk's worth of sites is the density the tables declare; the whole
	// world is that times its area. The default biome's scatter fills sum to the
	// chance a site carries anything, which is the density estimate the log wants.
	defaultFill := 0.0
	for _, scatter := range w.tuning.Tables.World.Terrain.Default.Scatter {
		defaultFill += scatter.Fill
	}
	perChunk := math.Pow(w.chunkSize/w.tuning.Tables.World.Terrain.Cell, 2) * defaultFill
	worldChunks := math.Pi * math.Pow(w.tuning.WorldRadius/w.chunkSize, 2)
	// What residency actually promises is that the cost follows the players
	// rather than the map: each body keeps the chunks inside its keep reach and
	// nothing else, so the bound is per-body and independent of world size.
	perBody := math.Pow(2*math.Ceil(w.chunkKeepReach()/w.chunkSize)+1, 2)
	t.Logf("%d spread bodies: %d chunks (bound %.0f) and %d items resident, of roughly %.0f chunks and %.0f items in the whole world, %v per tick",
		count, chunks, float64(count)*perBody, items, worldChunks, perChunk*worldChunks, elapsed)
	if float64(chunks) > float64(count)*perBody {
		t.Fatalf("%d chunks resident for %d bodies, past the %.0f each body can keep", chunks, count, perBody)
	}
	if float64(chunks) >= worldChunks {
		t.Fatalf("%d chunks resident of about %.0f; the world is effectively fully loaded", chunks, worldChunks)
	}
}

func playerID(index int) string {
	return "load-" + string(rune('a'+index/26)) + string(rune('a'+index%26))
}
