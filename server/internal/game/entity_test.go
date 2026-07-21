package game

import (
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

func number(value float64) *float64 { return &value }

func TestEntityDefaultsAndTypedRuntimeOverrides(t *testing.T) {
	tables := DefaultTuning().Tables
	tree := newEntity("tree", "tree", Vec{10, 20}, tables.Entities["tree"], EntityOverrides{})
	if tree.Mass != -1 || tree.Health != 500 || tree.MaxHealth != 500 || tree.circleRadius() != 27 {
		t.Fatalf("tree defaults = %#v", tree)
	}
	objects := []CollisionObject{{Type: CollisionBox, HalfWidth: 12, HalfHeight: 8}}
	overridden := newEntity("custom", "tree", Vec{}, tables.Entities["tree"], EntityOverrides{
		Mass: number(3), MaxHealth: number(750), CollisionObjects: &objects,
	})
	if overridden.Mass != 3 || overridden.Health != 750 || overridden.MaxHealth != 750 || overridden.CollisionObjects[0].Type != CollisionBox {
		t.Fatalf("construction overrides = %#v", overridden)
	}
	overridden.ApplyOverrides(EntityOverrides{Health: number(125), Mass: number(0)})
	if overridden.Health != 125 || overridden.Mass != 0 || tables.Entities["tree"].MaxHealth != 500 {
		t.Fatalf("dynamic overrides = %#v; tuning was mutated: %#v", overridden, tables.Entities["tree"])
	}
	overridden.ApplyOverrides(EntityOverrides{Health: number(0)})
	if overridden.Alive {
		t.Fatal("zero-health override left entity alive")
	}
}

func TestPlayerSimulationUsesDynamicEntityOverrides(t *testing.T) {
	w, now := testWorld()
	wall := testWorldItem(w, "wall", "wall", Vec{50, 0}, CollisionObject{Type: CollisionCircle, Radius: 10})
	w.worldItems = []*Entity{wall}
	p := addTestPlayer(w, "player", model.Gunslinger, Vec{}, now)
	objects := []CollisionObject{{Type: CollisionCircle, Radius: 30}}
	p.ApplyOverrides(EntityOverrides{CollisionObjects: &objects})
	for sequence := uint32(1); sequence <= 20; sequence++ {
		w.ApplyInput(p.ID, protocol.Input{Sequence: sequence, Buttons: ButtonRight, AimX: 1})
		w.Step(now.Add(time.Duration(sequence) * time.Second / 60))
	}
	if p.Position.X > 10.01 {
		t.Fatalf("overridden player radius did not drive collision: x=%g", p.Position.X)
	}
	w.worldItems = nil
	p.ApplyOverrides(EntityOverrides{Mass: number(-1)})
	before := p.Position
	w.ApplyInput(p.ID, protocol.Input{Sequence: 21, Buttons: ButtonRight | ButtonDash, AimX: 1})
	w.Step(now.Add(time.Second))
	if p.Position != before || p.Velocity != (Vec{}) {
		t.Fatalf("immovable player moved from %#v to %#v at velocity %#v", before, p.Position, p.Velocity)
	}
}

func TestProjectileDamagesAndDestroysTreeEntity(t *testing.T) {
	w, now := testWorld()
	tree := testWorldItem(w, "tree", "tree", Vec{50, 0}, CollisionObject{Type: CollisionCircle, Radius: 20})
	w.worldItems = []*Entity{tree}
	projectile := &Projectile{Damage: 200, Remaining: 1}
	projectile.Entity = w.newProjectileEntity("projectile", Vec{}, Vec{100, 0}, 5)
	if !w.advanceProjectile(projectile, 1, now, false) || tree.Health != 300 || !tree.Alive {
		t.Fatalf("first tree hit = health %g alive %v", tree.Health, tree.Alive)
	}
	projectile.Position = Vec{}
	projectile.Damage = 300
	if !w.advanceProjectile(projectile, 1, now.Add(time.Second), false) || tree.Health != 0 || tree.Alive {
		t.Fatalf("lethal tree hit = health %g alive %v", tree.Health, tree.Alive)
	}
	if w.collides(tree.Position, 1) {
		t.Fatal("destroyed tree still collides")
	}
}

func TestSquareWallIsImmovableUndestroyableAndSolid(t *testing.T) {
	w := NewWorld(DefaultTuning())
	wall := worldItemByKind(w, "wall")
	if wall == nil {
		t.Fatal("tuned wall fixture was not materialized")
	}
	if wall.Mass != -1 || wall.Health != -1 || wall.MaxHealth != -1 || len(wall.CollisionObjects) != 1 {
		t.Fatalf("wall base = %#v", wall)
	}
	box := wall.CollisionObjects[0]
	if box.Type != CollisionBox || box.HalfWidth != box.HalfHeight {
		t.Fatalf("wall collision = %#v", box)
	}
	if applied, destroyed := wall.TakeDamage(1_000_000); applied != 0 || destroyed || !wall.Alive || wall.Health != -1 {
		t.Fatalf("wall damage = applied %g destroyed %v wall %#v", applied, destroyed, wall)
	}
	if !wall.intersectsCircle(wall.Position.Add(Vec{box.HalfWidth + w.tuning.PlayerRadius - 1, 0}), w.tuning.PlayerRadius) {
		t.Fatal("player-sized circle did not collide with square wall")
	}
}
