package game

import (
	"math"
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

// rideRecipe picks the shipped recipe for a class, so these tests follow the
// live table rather than restating its costs.
func rideRecipe(t *testing.T, w *World, class model.Class) tuning.Rideable {
	t.Helper()
	for _, id := range w.tuning.Tables.RideablesFor(string(class)) {
		return w.tuning.Tables.Rideables[id]
	}
	t.Fatalf("no shipped rideable for %s", class)
	return tuning.Rideable{}
}

// funded gives a body exactly what a recipe costs, the way a haul would.
func funded(p *Player, recipe tuning.Rideable) *Player {
	for material, count := range recipe.Cost {
		p.Materials[material] += count
	}
	return p
}

// rideWorld is a full-scale world with a body standing at a crafting outpost,
// which is the only place a ride can be built.
func rideWorld(t *testing.T, class model.Class) (*World, *Player, tuning.Rideable, time.Time) {
	t.Helper()
	w := NewWorld(DefaultTuning())
	now := time.Unix(1_700_000_000, 0)
	outpost := shippedOutpost(t, w, func(o tuning.Outpost) bool { return o.Offers("crafting") })
	recipe := rideRecipe(t, w, class)
	p := funded(addTestPlayer(w, "rider", class, outpostPosition(outpost), now), recipe)
	return w, p, recipe, now
}

// Both classes build their ride into the world rather than into inventory, and
// it is gated to an outpost that offers crafting.
func TestCraftingARideSpawnsItAndIsGatedToAnOutpost(t *testing.T) {
	for _, class := range []model.Class{model.Gunslinger, model.Mage} {
		t.Run(string(class), func(t *testing.T) {
			w, p, recipe, now := rideWorld(t, class)
			if _, err := w.CraftRideable(p.ID, recipe.ID, now); err != nil {
				t.Fatalf("craft refused at an outpost that offers crafting: %v", err)
			}
			rides := w.Rideables()
			if len(rides) != 1 {
				t.Fatalf("world holds %d rides, want 1", len(rides))
			}
			ride := rides[0]
			if ride.OwnerID != p.ID || ride.Kind != recipe.Entity || ride.RideSpeed != recipe.RideSpeed {
				t.Fatalf("ride = %#v, want one owned by %s from %s", ride, p.ID, recipe.ID)
			}
			if ride.MaxHealth <= 0 || ride.Health != ride.MaxHealth {
				t.Fatalf("ride health = %g/%g, want a full pool", ride.Health, ride.MaxHealth)
			}
			// Nothing landed in inventory: the ride is the product.
			if len(p.Items) != 0 {
				t.Fatalf("crafting a ride left %d inventory items", len(p.Items))
			}
			// The materials it cost are gone.
			for material := range recipe.Cost {
				if p.Materials[material] != 0 {
					t.Fatalf("%s left %d unspent", material, p.Materials[material])
				}
			}
		})
	}
}

func TestARideCannotBeBuiltOutsideAnOutpost(t *testing.T) {
	w, p, recipe, now := rideWorld(t, model.Gunslinger)
	// Walk out into the Frontier, past every bubble.
	w.SetPlayerPosition(p.ID, Vec{12000, 0}, now)
	if _, err := w.CraftRideable(p.ID, recipe.ID, now); err == nil {
		t.Fatal("a ride was built in the field; crafting is safe-zone gated")
	}
	if len(w.Rideables()) != 0 {
		t.Fatal("a refused craft still spawned a ride")
	}
	// A refusal is atomic: nothing was spent.
	for material, count := range recipe.Cost {
		if p.Materials[material] != count {
			t.Fatalf("%s = %d after a refusal, want the original %d", material, p.Materials[material], count)
		}
	}
}

// One ride per owner: building a second replaces the first rather than
// littering the world.
func TestASecondRideReplacesTheFirst(t *testing.T) {
	w, p, recipe, now := rideWorld(t, model.Gunslinger)
	if _, err := w.CraftRideable(p.ID, recipe.ID, now); err != nil {
		t.Fatalf("first craft refused: %v", err)
	}
	first := w.Rideables()[0].ID
	funded(p, recipe)
	if _, err := w.CraftRideable(p.ID, recipe.ID, now); err != nil {
		t.Fatalf("second craft refused: %v", err)
	}
	rides := w.Rideables()
	if len(rides) != 1 {
		t.Fatalf("world holds %d rides, want 1", len(rides))
	}
	if rides[0].ID == first {
		t.Fatal("the second craft did not replace the first")
	}
}

// mountedRider builds a ride and puts its owner on it.
func mountedRider(t *testing.T, class model.Class) (*World, *Player, *Rideable, tuning.Rideable, time.Time) {
	t.Helper()
	w, p, recipe, now := rideWorld(t, class)
	if _, err := w.CraftRideable(p.ID, recipe.ID, now); err != nil {
		t.Fatalf("craft refused: %v", err)
	}
	if !w.tryMount(p, now) {
		t.Fatal("could not mount a ride standing beside the body")
	}
	ride := w.ownerRideable(p.ID)
	if ride == nil || ride.RiderID != p.ID || !p.Mounted() {
		t.Fatalf("mount did not take: ride=%#v mounted=%v", ride, p.Mounted())
	}
	return w, p, ride, recipe, now
}

// Riding is transport: it moves at the ride's speed and the use button does
// nothing.
func TestRidingMovesAtTheRideSpeedAndCannotFight(t *testing.T) {
	w, p, ride, recipe, now := mountedRider(t, model.Gunslinger)
	carrying(t, w, p, "starter-rifle")
	start := p.Position
	dt := 1 / float64(w.tuning.TickRate)
	p.Aim = Vec{1, 0}
	p.Input = protocol.Input{Sequence: 1, Buttons: ButtonRight | ButtonFire, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())}
	shots := len(w.projectiles)
	w.stepPlayer(p, now, dt)
	moved := math.Sqrt(p.Position.Sub(start).LengthSq())
	want := w.tuning.PlayerSpeed * recipe.RideSpeed * dt
	if math.Abs(moved-want) > 0.001 {
		t.Fatalf("a mounted step covered %g, want %g at %gx", moved, want, recipe.RideSpeed)
	}
	if len(w.projectiles) != shots {
		t.Fatal("a mounted body fired; riding is transport only")
	}
	// The rider and the ride stay together.
	if p.Position != ride.Position {
		t.Fatalf("rider at %#v but ride at %#v", p.Position, ride.Position)
	}
	// The interact button dismounts, and the body stands where the ride stands.
	p.PreviousButtons = 0
	p.Input = protocol.Input{Sequence: 2, Buttons: ButtonInteract, AimX: 1, ClientTimeMS: uint64(now.UnixMilli())}
	w.stepPlayer(p, now, dt)
	if p.Mounted() || ride.RiderID != "" {
		t.Fatalf("interact did not dismount: mounted=%v rider=%q", p.Mounted(), ride.RiderID)
	}
	if p.Position != ride.Position {
		t.Fatalf("dismounted to %#v, want the ride's position %#v", p.Position, ride.Position)
	}
}

// Damage aimed at a rider lands on the ride, and destroying the ride is what
// forces the dismount. The rider therefore never dies while riding.
func TestDestroyingARideDismountsItsRider(t *testing.T) {
	w, p, ride, _, now := mountedRider(t, model.Mage)
	p.Health = 100
	w.damage(p, 40, "attacker", now)
	if p.Health != 100 {
		t.Fatalf("rider health = %g, want the ride to have taken the hit", p.Health)
	}
	if ride.Health != ride.MaxHealth-40 {
		t.Fatalf("ride health = %g, want %g", ride.Health, ride.MaxHealth-40)
	}
	// Finish it off.
	w.damage(p, ride.MaxHealth, "attacker", now)
	if p.Mounted() || ride.Alive {
		t.Fatalf("a destroyed ride left the body mounted: mounted=%v alive=%v", p.Mounted(), ride.Alive)
	}
	if !p.Alive || p.Health != 100 {
		t.Fatalf("the rider was hurt by its ride being destroyed: alive=%v health=%g", p.Alive, p.Health)
	}
	// Back on foot, it takes damage itself again.
	w.damage(p, 25, "attacker", now)
	if p.Health != 75 {
		t.Fatalf("health = %g on foot, want 75", p.Health)
	}
}

// A ride cannot be entered mid-fight, which is what stops it being an escape
// from a fight rather than a way to shorten a journey.
func TestARideCannotBeMountedJustAfterCombat(t *testing.T) {
	w, p, recipe, now := rideWorld(t, model.Gunslinger)
	if _, err := w.CraftRideable(p.ID, recipe.ID, now); err != nil {
		t.Fatalf("craft refused: %v", err)
	}
	p.LastCombat = now
	if w.tryMount(p, now) {
		t.Fatal("a body mounted immediately after combat")
	}
	if w.tryMount(p, now.Add(w.tuning.MountLockout-time.Millisecond)) {
		t.Fatal("a body mounted before the lockout expired")
	}
	if !w.tryMount(p, now.Add(w.tuning.MountLockout)) {
		t.Fatal("a body could not mount once the lockout expired")
	}
}

// A rider cannot normally die while mounted — damage routes to the ride — but
// the respawn path must still put the body at its outpost rather than back on
// the ride it was sitting on.
func TestRespawningWhileMountedLandsAtTheOutpostNotOnTheRide(t *testing.T) {
	w, p, ride, _, now := mountedRider(t, model.Gunslinger)
	outpost := shippedOutpost(t, w, func(o tuning.Outpost) bool { return o.Offers("crafting") })
	p.Outposts = []string{outpost.ID}
	// Drive the ride clear of the outpost's bubble, but nearer to it than to the
	// hub, so the two candidate destinations are unambiguously distinguishable.
	away := outpostPosition(outpost).Add(Vec{2000, 0})
	w.SetPlayerPosition(p.ID, away, now)
	ride.Position = away
	w.rideGrid.update(ride)
	p.Health, p.Alive = 0, false
	if !w.Respawn(p.ID, now) {
		t.Fatal("respawn rejected")
	}
	if p.Mounted() {
		t.Fatal("respawned still mounted")
	}
	if p.Position != outpostPosition(outpost) {
		t.Fatalf("respawned at %#v, want the outpost at %#v", p.Position, outpostPosition(outpost))
	}
}

// A ride is transient: it goes with the body that owns it.
func TestARideIsReapedWhenItsOwnerLeaves(t *testing.T) {
	w, p, recipe, now := rideWorld(t, model.Gunslinger)
	if _, err := w.CraftRideable(p.ID, recipe.ID, now); err != nil {
		t.Fatalf("craft refused: %v", err)
	}
	w.RemovePlayer(p.ID)
	if len(w.Rideables()) != 0 {
		t.Fatal("a ride outlived the owner that left the world")
	}
}

// A ride reaches the wire as its own entity so both sides can see it — and its
// health — and the rider is flagged as mounted.
func TestARideAndItsRiderReachTheSnapshot(t *testing.T) {
	w, p, ride, _, now := mountedRider(t, model.Mage)
	snapshot := w.SnapshotFor(p.ID, now, protocol.ServerSnapshot)
	var sawMount, sawRider bool
	for _, entity := range snapshot.Entities {
		if entity.Type == protocol.EntityMount && entity.ID == ride.ID {
			sawMount = true
			if entity.MaxHealth != float32(ride.MaxHealth) || entity.OwnerID != p.ID {
				t.Fatalf("mount entity = %#v, want %s's ride with its health", entity, p.ID)
			}
		}
		if entity.Type == protocol.EntityPlayer && entity.ID == p.ID {
			sawRider = true
			if !entity.Mounted {
				t.Fatal("the rider is not flagged as mounted on the wire")
			}
		}
	}
	if !sawMount || !sawRider {
		t.Fatalf("snapshot carried mount=%v rider=%v, want both", sawMount, sawRider)
	}
}
