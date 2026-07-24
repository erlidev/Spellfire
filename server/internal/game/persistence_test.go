package game

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

// recorder stands in for the store: the engine's contract is what it writes and
// when, not how SQLite stores it.
type recorder struct {
	mu       sync.Mutex
	writes   map[string]model.CharacterState
	progress map[string]model.Progress
	items    []model.CraftedItem
	fail     error
}

func newRecorder() *recorder {
	return &recorder{writes: map[string]model.CharacterState{}, progress: map[string]model.Progress{}}
}

func (r *recorder) CreateCraftedItem(_ context.Context, item model.CraftedItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.items = append(r.items, item)
	return nil
}

// savedItems is the crafted items the engine has written, copied so a test
// never reads the recorder's slice while the writer goroutine appends to it.
func (r *recorder) savedItems() []model.CraftedItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]model.CraftedItem(nil), r.items...)
}

func (r *recorder) SaveCharacterProgress(_ context.Context, id string, progress model.Progress) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.progress[id] = progress
	return nil
}

func (r *recorder) savedProgress(id string) (model.Progress, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	progress, ok := r.progress[id]
	return progress, ok
}

func (r *recorder) SaveCharacterState(_ context.Context, id string, state model.CharacterState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.writes[id] = state
	return nil
}

func (r *recorder) saved(id string) (model.CharacterState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.writes[id]
	return state, ok
}

// placed builds a record saved at a position, stamped as seen just now so the
// position has not expired.
func placed(id string, x, y float64) model.Character {
	return placedAt(id, x, y, time.Now())
}

func placedAt(id string, x, y float64, lastSeen time.Time) model.Character {
	return model.Character{
		ID: id, Name: "Vance", Class: model.Gunslinger, SchemaVersion: model.CharacterSchemaVersion,
		State: model.CharacterState{Position: model.Point{X: x, Y: y}, Placed: true, LastSeen: lastSeen},
	}
}

// A disconnect must cost the session, not the walk back out.
func TestSavedPositionIsRestoredOnJoin(t *testing.T) {
	world := NewWorld(DefaultTuning())
	// Inside the terrain floor (safe radius + inner margin), so the saved spot is
	// deterministically clear of generated cover and the honour path is what is
	// under test rather than a chance collision.
	p := world.AddPlayer(placed("p", 1150, -200), time.Now())
	if p.Position != (Vec{1150, -200}) {
		t.Fatalf("position = %#v, want the saved one", p.Position)
	}
}

// A position that has sat unclaimed too long stops being honoured: the
// character is recalled to safety rather than dropped back into the field.
func TestAnExpiredPositionRecallsToTheNearestSafeDestination(t *testing.T) {
	now := time.Now()
	expiry := DefaultTuning().PositionExpiry
	world := NewWorld(DefaultTuning())
	spawn := world.tuning.Tables.World.SpawnRadius

	fresh := world.AddPlayer(placedAt("fresh", 1150, -200, now.Add(-expiry+time.Minute)), now)
	if fresh.Position != (Vec{1150, -200}) {
		t.Fatalf("a position inside the expiry was not honoured: %#v", fresh.Position)
	}
	for name, character := range map[string]model.Character{
		"expired":   placedAt("expired", 1500, -200, now.Add(-expiry)),
		"unstamped": placedAt("unstamped", 1500, -200, time.Time{}),
	} {
		t.Run(name, func(t *testing.T) {
			p := NewWorld(DefaultTuning()).AddPlayer(character, now)
			if distance := math.Sqrt(p.Position.LengthSq()); math.Abs(distance-spawn) > 0.001 {
				t.Fatalf("recalled to %g from the origin, want the %g hub spawn ring", distance, spawn)
			}
		})
	}
}

// With outposts on the map, the recall picks whichever safe fixture is nearest
// to where the character logged out — including the hub when it is closer.
func TestRecallPrefersTheNearestUnlockedOutpost(t *testing.T) {
	tables := tablesWithOutposts(t)
	world := NewWorld(FromTables(tables))
	now := time.Now()
	rim := Vec{2600, 0}

	unlocked := placedAt("a", rim.X, rim.Y, time.Time{})
	unlocked.State.Outposts = []string{"ridge", "hollow"}
	if got := world.AddPlayer(unlocked, now); got.Position != (Vec{2500, 0}) {
		t.Fatalf("recalled to %#v, want the nearer unlocked outpost", got.Position)
	}
	// An outpost the character never discovered is not a destination.
	locked := placedAt("b", rim.X, rim.Y, time.Time{})
	if got := world.AddPlayer(locked, now); math.Abs(math.Sqrt(got.Position.LengthSq())-tables.World.SpawnRadius) > 0.001 {
		t.Fatalf("recalled to %#v, want the hub for a character with no unlocks", got.Position)
	}
	// Logging out near the middle recalls to the hub, not to a distant outpost.
	near := placedAt("c", 500, 0, time.Time{})
	near.State.Outposts = []string{"ridge"}
	if got := world.AddPlayer(near, now); math.Abs(math.Sqrt(got.Position.LengthSq())-tables.World.SpawnRadius) > 0.001 {
		t.Fatalf("recalled to %#v, want the closer hub", got.Position)
	}
}

func TestUnplacedAndUnusableSavesFallBackToTheHubSpawn(t *testing.T) {
	world := NewWorld(DefaultTuning())
	spawn := world.tuning.Tables.World.SpawnRadius
	tree := worldItemByKind(world, "tree")
	if tree == nil {
		t.Fatal("generated world has no tree")
	}
	cases := map[string]model.Character{
		"never placed":      {ID: "a", Name: "A", Class: model.Gunslinger, SchemaVersion: model.CharacterSchemaVersion},
		"outside the rim":   placed("b", world.tuning.WorldRadius+500, 0),
		"inside a new tree": placed("c", tree.Position.X, tree.Position.Y),
		"not a real number": placed("d", math.NaN(), 0),
	}
	for name, character := range cases {
		t.Run(name, func(t *testing.T) {
			p := NewWorld(DefaultTuning()).AddPlayer(character, time.Now())
			if distance := math.Sqrt(p.Position.LengthSq()); math.Abs(distance-spawn) > 0.001 {
				t.Fatalf("spawn distance = %g, want the %g spawn ring", distance, spawn)
			}
		})
	}
}

// Carried materials are references. Content that retires one owes the character
// its replacement or its refund; only an ID from no build at all is dropped.
func TestCarriedMaterialsResolveThroughRetirement(t *testing.T) {
	tables := tablesWithRetiredMaterials(t)
	balance := FromTables(tables)
	world := NewWorld(balance)
	character := model.Character{
		ID: "p", Name: "Ilse", Class: model.Mage, SchemaVersion: model.CharacterSchemaVersion,
		State: model.CharacterState{Materials: map[string]int{"iron": 1, "old-iron": 2, "lost-alloy": 3, "never-shipped": 9}},
	}
	p := world.AddPlayer(character, time.Now())
	// 1 held + 2 replaced + 3 × a 2-iron refund.
	if !reflect.DeepEqual(p.Materials, map[string]int{"iron": 9}) {
		t.Fatalf("carried materials = %#v", p.Materials)
	}
}

func TestStateOfCapturesCarriedStateAndUnplacesTheDead(t *testing.T) {
	world := NewWorld(DefaultTuning())
	p := world.AddPlayer(placed("p", 900, 120), time.Now())
	p.Materials["iron"] = 4
	p.Outposts = []string{"ridge"}

	state, ok := world.StateOf("p", time.Now())
	if !ok || !state.Placed || state.Position != (model.Point{X: 900, Y: 120}) {
		t.Fatalf("state = %#v, %v", state, ok)
	}
	if state.Materials["iron"] != 4 || !reflect.DeepEqual(state.Outposts, []string{"ridge"}) {
		t.Fatalf("state = %#v", state)
	}
	// The captured state is a copy: later play must not mutate a queued save.
	p.Materials["iron"] = 99
	if state.Materials["iron"] != 4 {
		t.Fatal("StateOf aliased the live inventory")
	}
	// A dead player is saved unplaced, so the next join enters at the hub.
	p.Alive = false
	if state, _ := world.StateOf("p", time.Now()); state.Placed {
		t.Fatal("a dead player was saved as placed")
	}
	if _, ok := world.StateOf("absent", time.Now()); ok {
		t.Fatal("state for an absent player")
	}
}

// Dropping the connection must not remove the target. The body holds its
// ground, stops acting, and stays killable until the logout window closes.
func TestLingeringBodyCannotActButCanStillBeKilled(t *testing.T) {
	balance := DefaultTuning()
	// The compact test arena testWorld() explains: this is about the logout
	// window, not about where PvP protection ends.
	balance.scaleSafety(430, 1000)
	world := NewWorld(balance)
	world.setWorldItems()
	now := time.Unix(1_700_000_000, 0)
	victim := world.AddPlayer(placed("victim", 1500, 0), now)
	attacker := world.AddPlayer(placed("attacker", 1400, 0), now)
	world.recordHistory(victim, now)
	world.recordHistory(attacker, now)

	// The victim is mid-sprint east when the connection drops.
	world.ApplyInput("victim", protocol.Input{Sequence: 1, Buttons: ButtonRight | ButtonFire, AimX: 1})
	if !world.BeginLinger("victim", now) {
		t.Fatal("linger not started")
	}
	world.ApplyInput("attacker", protocol.Input{Sequence: 1, Buttons: ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())})
	before := victim.Position
	for i := 1; i <= 60 && victim.Health == world.tuning.MaxHealth; i++ {
		world.Step(now.Add(time.Duration(i) * time.Second / 60))
	}
	if victim.Position != before {
		t.Fatalf("a lingering body moved from %#v to %#v", before, victim.Position)
	}
	if victim.Health == world.tuning.MaxHealth {
		t.Fatal("a lingering body took no damage; disconnecting escaped the fight")
	}
	if len(world.projectiles) > 0 {
		for _, projectile := range world.projectiles {
			if projectile.OwnerID == "victim" {
				t.Fatal("a lingering body kept firing")
			}
		}
	}
}

// Being killed while logged out must cost the position. Reconnecting inside the
// window re-enters at the hub rather than resuming the corpse where it fell.
func TestReconnectingToABodyKilledWhileLoggedOutEntersAtTheHub(t *testing.T) {
	engine := NewEngine(DefaultTuning(), nil)
	character := placed("p", 1500, -200)
	now := time.Now()

	client, _ := engine.Join(character, now)
	engine.Leave(client)
	engine.mu.Lock()
	killed := engine.world.players["p"]
	killed.Health, killed.Alive = 0, false
	engine.mu.Unlock()

	_, _ = engine.Join(character, now.Add(time.Second))
	engine.mu.Lock()
	back := engine.world.players["p"]
	engine.mu.Unlock()
	if !back.Alive || back.Health != engine.world.tuning.MaxHealth {
		t.Fatalf("rejoined body = alive %v, health %v; the corpse was resumed", back.Alive, back.Health)
	}
	if back.Position != engine.world.hubSpawn("p") {
		t.Fatalf("rejoined at %#v; a death while logged out kept its position", back.Position)
	}
	if back.Lingering() {
		t.Fatal("the replacement body is still inside a logout window")
	}
}

func TestReconnectingInsideTheLogoutWindowResumesTheSameBody(t *testing.T) {
	store := newRecorder()
	engine := NewEngine(DefaultTuning(), store)
	character := placed("p", 1500, -200)
	now := time.Now()

	client, _ := engine.Join(character, now)
	engine.world.players["p"].Position = Vec{1600, -100}
	engine.Leave(client)

	engine.mu.Lock()
	lingering := engine.world.players["p"].Lingering()
	engine.mu.Unlock()
	if !lingering {
		t.Fatal("leaving did not leave a body behind")
	}
	// Reconnecting picks the body back up where the fight left it, rather than
	// restoring the position the record was saved at.
	_, _ = engine.Join(character, now.Add(time.Second))
	engine.mu.Lock()
	resumed := engine.world.players["p"]
	engine.mu.Unlock()
	if resumed.Lingering() || resumed.Position != (Vec{1600, -100}) {
		t.Fatalf("resumed player = %#v", resumed)
	}
	if _, ok := store.saved("p"); ok {
		t.Fatal("a resumed session was saved as if it had ended")
	}
}

// A reconnected client counts its inputs from zero again. The resumed body must
// accept them, or the player is stuck in place unable to act.
func TestAResumedBodyAcceptsInputFromTheNewConnection(t *testing.T) {
	engine := NewEngine(DefaultTuning(), nil)
	character := placed("p", 1500, -200)
	now := time.Now()

	client, _ := engine.Join(character, now)
	engine.Input("p", protocol.Input{Sequence: 40, Buttons: ButtonRight, AimX: 1})
	engine.Leave(client)
	_, _ = engine.Join(character, now.Add(time.Second))

	engine.Input("p", protocol.Input{Sequence: 1, Buttons: ButtonRight, AimX: 1})
	engine.mu.Lock()
	resumed := engine.world.players["p"]
	engine.mu.Unlock()
	if resumed.Input.Sequence != 1 {
		t.Fatalf("input sequence after reconnect = %d; the new connection's input was rejected", resumed.Input.Sequence)
	}

	before := resumed.Position
	engine.world.Step(now.Add(time.Second))
	if engine.world.players["p"].Position == before {
		t.Fatal("a resumed player could not move")
	}
}

func TestExpiredLogoutWindowSavesAndRemovesTheBody(t *testing.T) {
	store := newRecorder()
	balance := DefaultTuning()
	balance.LogoutLinger = 100 * time.Millisecond
	engine := NewEngine(balance, store)
	engine.saveEvery = time.Hour // Only the reap may save here.
	client, _ := engine.Join(placed("p", 1500, -200), time.Now())
	engine.world.players["p"].Position = Vec{1600, -100}
	engine.Leave(client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		engine.mu.Lock()
		remaining := engine.world.players["p"]
		engine.mu.Unlock()
		state, saved := store.saved("p")
		if remaining == nil && saved {
			// The reaped body is saved where the world left it, stamped so the
			// expiry clock can start.
			if state.Position != (model.Point{X: 1600, Y: -100}) || state.LastSeen.IsZero() {
				t.Fatalf("saved state = %#v", state)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The save is written asynchronously after the removal, so the loop waits
	// for both rather than treating the gap between them as a failure.
	t.Fatal("the body was never reaped and saved")
}

// A connection that has already been replaced owns neither the player nor its
// fate; acting on it would strand or overwrite the live session.
func TestReplacedConnectionLeavesTheLiveSessionAlone(t *testing.T) {
	store := newRecorder()
	engine := NewEngine(DefaultTuning(), store)
	character := placed("p", 1500, -200)
	old, _ := engine.Join(character, time.Now())
	_, _ = engine.Join(character, time.Now())
	engine.Leave(old)

	engine.mu.Lock()
	player := engine.world.players["p"]
	engine.mu.Unlock()
	if player == nil || player.Lingering() {
		t.Fatalf("player = %#v, want the live replacement session", player)
	}
	if _, ok := store.saved("p"); ok {
		t.Fatal("a replaced connection wrote a save")
	}
}

// A graceful shutdown must not discard the session, bodies mid-logout included.
func TestShutdownFlushesEveryPresentPlayer(t *testing.T) {
	store := newRecorder()
	engine := NewEngine(DefaultTuning(), store)
	engine.saveEvery = time.Hour // Only the shutdown flush can save here.
	_, _ = engine.Join(placed("connected", 1500, -200), time.Now())
	client, _ := engine.Join(placed("logging-out", -900, 300), time.Now())
	engine.Leave(client)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { engine.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	for _, id := range []string{"connected", "logging-out"} {
		if _, ok := store.saved(id); !ok {
			t.Fatalf("%s was not flushed on shutdown", id)
		}
	}
}

func TestEngineAutosavesWhileConnectedAndSurvivesAFailedWrite(t *testing.T) {
	store := newRecorder()
	store.fail = errors.New("database is locked")
	engine := NewEngine(DefaultTuning(), store)
	engine.saveEvery = 20 * time.Millisecond
	_, _ = engine.Join(placed("p", 1150, -200), time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)

	// The early autosaves all fail; the world must keep stepping regardless.
	time.Sleep(100 * time.Millisecond)
	engine.mu.Lock()
	stepped := engine.world.tick > 0
	engine.mu.Unlock()
	if !stepped {
		t.Fatal("a failing persister stalled the tick loop")
	}
	if _, ok := store.saved("p"); ok {
		t.Fatal("a failed write was recorded")
	}

	store.mu.Lock()
	store.fail = nil
	store.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if state, ok := store.saved("p"); ok {
			if !state.Placed || state.Position != (model.Point{X: 1150, Y: -200}) {
				t.Fatalf("autosaved state = %#v", state)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a connected player was never autosaved")
}

// tablesWithOutposts places two outposts on the rim so the recall search has
// real geography to choose between. Phase 3 owns where they actually sit.
func tablesWithOutposts(t *testing.T) *tuning.Tables {
	t.Helper()
	files := edit(t, shipped(t), "outposts.json", func(document map[string]any) {
		document["ridge"] = map[string]any{"name": "Ridge Station", "band": "fringe", "position": []any{2500.0, 0.0}, "safe_radius": 650.0, "discovery_radius": 900.0, "services": []any{"loadout", "crafting", "respawn"}}
		document["hollow"] = map[string]any{"name": "Hollow Post", "band": "fringe", "position": []any{-2500.0, 0.0}, "safe_radius": 650.0, "discovery_radius": 900.0, "services": []any{"loadout", "crafting", "respawn"}}
	})
	tables, err := tuning.Parse(files)
	if err != nil {
		t.Fatalf("patched tables are invalid: %v", err)
	}
	return tables
}

// tablesWithRetiredMaterials builds a table set holding one live material and
// the retirements that point at it, the way a content patch would.
func tablesWithRetiredMaterials(t *testing.T) *tuning.Tables {
	t.Helper()
	files := edit(t, shipped(t), "materials.json", func(document map[string]any) {
		document["materials"].(map[string]any)["iron"] = map[string]any{
			"name": "Iron", "grade": "common", "kind": "structural",
		}
	})
	files = edit(t, files, "retired.json", func(document map[string]any) {
		document["old-iron"] = map[string]any{"kind": "material", "replacement": "iron", "note": "renamed"}
		document["lost-alloy"] = map[string]any{"kind": "material", "refund": map[string]any{"iron": 2.0}, "note": "recipe withdrawn"}
	})
	tables, err := tuning.Parse(files)
	if err != nil {
		t.Fatalf("patched tables are invalid: %v", err)
	}
	return tables
}
