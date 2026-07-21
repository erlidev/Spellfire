package game

import (
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

func TestEntityMetadataSpawnsLiveEntitiesAndRegistryAppliesOverrides(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	world := NewWorld(DefaultTuning())
	viewer := world.AddPlayer(model.Character{ID: "viewer", Name: "Viewer", Class: model.Gunslinger}, now)
	if err := world.adminSpawn(AdminSpawn{ID: "player", Position: Vec{X: 300, Y: 0}, Config: map[string]string{"player.name": "Practice Mage", "player.class": "mage", "player.speed_multiplier": "2"}}, now); err != nil {
		t.Fatalf("spawn player: %v", err)
	}
	fixture, ok := world.PlayerState("admin-player-1")
	if !ok || !fixture.AdminSpawned || fixture.Name != "Practice Mage" || fixture.SpeedMultiplier != 2 || fixture.Position != (Vec{X: 300}) {
		t.Fatalf("spawned fixture = %#v, %v", fixture, ok)
	}
	if _, persisted := world.StateOf(fixture.ID, now); persisted {
		t.Fatal("admin fixture was exposed as a persistent character state")
	}
	if err := world.adminSpawn(AdminSpawn{ID: "projectile", Position: Vec{X: 100, Y: 25}, Config: map[string]string{"projectile.ability": "rifle-shot", "transform.heading_degrees": "90"}}, now); err != nil {
		t.Fatalf("spawn projectile: %v", err)
	}
	projectile := world.projectiles["p-0"]
	if projectile == nil || projectile.Position != (Vec{X: 100, Y: 25}) || projectile.Velocity.Y <= 0 || projectile.Velocity.X > .001 {
		t.Fatalf("projectile = %#v", projectile)
	}
	if err := world.adminSpawn(AdminSpawn{ID: "telegraph", Position: Vec{X: 200, Y: 20}, Config: map[string]string{"telegraph.ability": "fire-bolt-cast", "transform.heading_degrees": "180"}}, now); err != nil {
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
	if err := world.adminSpawn(AdminSpawn{ID: "player", Position: Vec{X: 9_999}, Config: map[string]string{}}, now); err == nil {
		t.Fatal("out-of-world placement was accepted")
	}
}

func TestAdminCanInspectEditAndGracefullyDeleteAnyEntity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	world := NewWorld(DefaultTuning())
	player := world.AddPlayer(model.Character{ID: "connected", AccountID: "account", Name: "Connected", Class: model.Gunslinger}, now)
	if _, err := world.adminEdit(player.ID, map[string]string{"transform.position.x": "120", "transform.position.y": "-40", "player.speed_multiplier": "1.75"}, now); err != nil {
		t.Fatal(err)
	}
	state, err := world.adminInspect(player.ID)
	if err != nil || state.DefinitionID != "player" || state.Values["transform.position.x"] != "120" || player.SpeedMultiplier != 1.75 {
		t.Fatalf("state=%#v player=%#v err=%v", state, player, err)
	}

	if err := world.adminSpawn(AdminSpawn{ID: "tree", Position: Vec{X: 200}}, now); err != nil {
		t.Fatal(err)
	}
	treeID := "admin-tree-1"
	if _, err := world.adminEdit(treeID, map[string]string{"vitals.health": "250"}, now); err != nil {
		t.Fatal(err)
	}
	if target, _ := world.adminTarget(treeID); target.entity.Health != 250 {
		t.Fatalf("tree health = %g", target.entity.Health)
	}
	if err := world.adminDelete(treeID, now); err != nil {
		t.Fatal(err)
	}
	target, _ := world.adminTarget(treeID)
	if !target.entity.Deleting || target.entity.Alive {
		t.Fatalf("tree did not enter graceful deletion: %#v", target.entity)
	}
	world.Step(now.Add(entityDeleteFade + entityDeleteReapDelay))
	if _, ok := world.adminTarget(treeID); ok {
		t.Fatal("tree was not reaped after its fade")
	}

	if err := world.adminDelete(player.ID, now); err != nil {
		t.Fatal(err)
	}
	world.Step(now.Add(entityDeleteFade + entityDeleteReapDelay))
	if world.players[player.ID] == nil || player.Alive {
		t.Fatal("connected player was removed instead of remaining in the death flow")
	}
	if !world.Respawn(player.ID, now.Add(time.Second)) || player.Deleting || !player.Alive {
		t.Fatal("connected player did not respawn cleanly")
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

func TestEveryTunedEditableAttributeHasAnExplicitRegistryAdapter(t *testing.T) {
	for definitionID, definition := range DefaultTuning().Tables.Entities {
		for _, field := range definition.Admin.Fields {
			if field.Scope == "spawn" {
				continue
			}
			adapter, ok := adminAttributeRegistry[field.Attribute]
			if !ok || adapter.get == nil || adapter.set == nil {
				t.Fatalf("%s editable attribute %q has no complete adapter", definitionID, field.Attribute)
			}
		}
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
