package game

import (
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

func TestAdminCatalogSpawnsLiveEntitiesAndAppliesOverrides(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	world := NewWorld(DefaultTuning())
	viewer := world.AddPlayer(model.Character{ID: "viewer", Name: "Viewer", Class: model.Gunslinger}, now)
	if err := world.adminSpawn(AdminSpawn{ID: "training-mage", Position: Vec{X: 300, Y: 0}, Config: map[string]string{"name": "Practice Mage", "speed_multiplier": "2"}}, now); err != nil {
		t.Fatalf("spawn player: %v", err)
	}
	fixture, ok := world.PlayerState("admin-player-1")
	if !ok || !fixture.AdminSpawned || fixture.Name != "Practice Mage" || fixture.SpeedMultiplier != 2 || fixture.Position != (Vec{X: 300}) {
		t.Fatalf("spawned fixture = %#v, %v", fixture, ok)
	}
	if _, persisted := world.StateOf(fixture.ID, now); persisted {
		t.Fatal("admin fixture was exposed as a persistent character state")
	}
	if err := world.adminSpawn(AdminSpawn{ID: "rifle-projectile", Position: Vec{X: 100, Y: 25}, Config: map[string]string{"heading_degrees": "90"}}, now); err != nil {
		t.Fatalf("spawn projectile: %v", err)
	}
	projectile := world.projectiles["p-0"]
	if projectile == nil || projectile.Position != (Vec{X: 100, Y: 25}) || projectile.Velocity.Y <= 0 || projectile.Velocity.X > .001 {
		t.Fatalf("projectile = %#v", projectile)
	}
	if err := world.adminSpawn(AdminSpawn{ID: "fire-bolt-telegraph", Position: Vec{X: 200, Y: 20}, Config: map[string]string{"heading_degrees": "180"}}, now); err != nil {
		t.Fatalf("spawn telegraph: %v", err)
	}
	if telegraph := world.telegraphs["t-0"]; telegraph == nil || telegraph.Position != (Vec{X: 200, Y: 20}) || telegraph.Direction.X >= 0 {
		t.Fatalf("telegraph = %#v", telegraph)
	}
	if err := world.setAdminAttributes(viewer.ID, map[string]float64{"speed_multiplier": 1.5, "view_distance": 2000}); err != nil {
		t.Fatalf("set attributes: %v", err)
	}
	updated, _ := world.PlayerState(viewer.ID)
	if updated.SpeedMultiplier != 1.5 || updated.ViewDistance != 2000 {
		t.Fatalf("updated player = %#v", updated)
	}
	if err := world.adminSpawn(AdminSpawn{ID: "training-mage", Position: Vec{X: 9_999}, Config: map[string]string{}}, now); err == nil {
		t.Fatal("out-of-world placement was accepted")
	}
}

func TestAdminViewDistanceChangesOnlyTheViewerSnapshot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	world := NewWorld(DefaultTuning())
	viewer := world.AddPlayer(model.Character{ID: "viewer", Name: "Viewer", Class: model.Gunslinger}, now)
	world.AddPlayer(model.Character{ID: "far", Name: "Far", Class: model.Mage}, now)
	if !world.SetPlayerPosition("far", Vec{X: 1600}, now) {
		t.Fatal("could not move far test player")
	}
	if hasPlayer(world.SnapshotFor(viewer.ID, now, protocol.ServerSnapshot), "far") {
		t.Fatal("far player appeared at the default view distance")
	}
	if err := world.setAdminAttributes(viewer.ID, map[string]float64{"view_distance": 2000}); err != nil {
		t.Fatal(err)
	}
	if !hasPlayer(world.SnapshotFor(viewer.ID, now, protocol.ServerSnapshot), "far") {
		t.Fatal("far player did not appear at the admin view distance")
	}
}

func hasPlayer(snapshot protocol.ServerEnvelope, id string) bool {
	for _, entity := range snapshot.Entities {
		if entity.Type == protocol.EntityPlayer && entity.ID == id {
			return true
		}
	}
	return false
}
