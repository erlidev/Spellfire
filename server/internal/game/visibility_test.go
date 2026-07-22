package game

import (
	"testing"
	"time"

	"spellfire/server/internal/model"
)

func TestTerrainOccludesSnapshotsUntilItStopsStanding(t *testing.T) {
	w, now := testWorld()
	viewer := addTestPlayer(w, "viewer", model.Gunslinger, Vec{1200, 0}, now)
	behindTree := addTestPlayer(w, "behind-tree", model.Mage, Vec{1500, 0}, now)
	besideTree := addTestPlayer(w, "beside-tree", model.Mage, Vec{1500, 160}, now)
	tree := testWorldItem(w, "tree", "tree", Vec{1350, 0}, CollisionObject{Type: CollisionCircle, Radius: 35})
	w.worldItems = []*Entity{tree}

	if visible(w, viewer.ID, behindTree.ID, now) {
		t.Fatal("a tree did not hide the body directly behind it")
	}
	if !visible(w, viewer.ID, besideTree.ID, now) {
		t.Fatal("a tree hid a body whose sightline does not cross it")
	}
	if visible(w, behindTree.ID, viewer.ID, now) {
		// Visibility must be symmetric around solid cover. If this fails while
		// the first assertion passes, one side has gained information the other
		// side cannot receive.
		t.Fatal("terrain occlusion was not symmetric")
	}

	// Destruction removes collision and sight blocking on the same authoritative
	// transition, even while the shared deletion fade remains in snapshots.
	tree.TakeDamage(tree.Health)
	tree.Delete(now.Add(time.Millisecond))
	if !visible(w, viewer.ID, behindTree.ID, now.Add(2*time.Millisecond)) {
		t.Fatal("destroyed terrain continued to hide a body")
	}
}

func TestCircleAndBoxTerrainShareTheSightRule(t *testing.T) {
	w, _ := testWorld()
	from, to := Vec{1200, 0}, Vec{1500, 0}

	w.worldItems = []*Entity{testWorldItem(w, "tree", "tree", Vec{1350, 0}, CollisionObject{Type: CollisionCircle, Radius: 30})}
	if !w.terrainOccluded(from, to) {
		t.Fatal("circular terrain did not block line of sight")
	}
	w.worldItems = []*Entity{testWorldItem(w, "wall", "wall", Vec{1350, 0}, CollisionObject{Type: CollisionBox, HalfWidth: 12, HalfHeight: 70})}
	if !w.terrainOccluded(from, to) {
		t.Fatal("box terrain did not block line of sight")
	}
	if w.terrainOccluded(from, Vec{1500, 160}) {
		t.Fatal("box terrain blocked a sightline that passes beside it")
	}
}

func TestAutomaticTargetingRequiresLineOfSight(t *testing.T) {
	w, now := testWorld()
	owner := addTestPlayer(w, "owner", model.Mage, Vec{1200, 0}, now)
	blocked := addTestPlayer(w, "blocked", model.Gunslinger, Vec{1380, 0}, now)
	open := addTestPlayer(w, "open", model.Gunslinger, Vec{1450, 300}, now)
	w.worldItems = []*Entity{testWorldItem(w, "wall", "wall", Vec{1300, 0}, CollisionObject{Type: CollisionBox, HalfWidth: 18, HalfHeight: 60})}

	if got := w.nearestPlayer(owner.Position, 500, map[string]bool{owner.ID: true}, owner); got != open {
		id := "<nil>"
		if got != nil {
			id = got.ID
		}
		t.Fatalf("automatic target = %s, want the farther visible body", id)
	}

	// Smoke participates in the same acquisition rule without becoming solid:
	// it hides only the body it completely covers.
	w.worldItems = nil
	field := *w.tuning.Tables.Abilities["smoke-throw"].Deployable
	w.deploy("", field, blocked.Position, "", now)
	if got := w.nearestPlayer(owner.Position, 500, map[string]bool{owner.ID: true}, owner); got != open {
		t.Fatalf("smoke-hidden body was acquired instead of %s: %#v", open.ID, got)
	}

	w.applyEffects(owner, []string{"flash-blind"}, blocked.ID, Vec{1, 0}, now)
	if got := w.nearestPlayer(owner.Position, 500, map[string]bool{owner.ID: true}, owner); got != nil {
		t.Fatalf("blinded owner automatically acquired %s", got.ID)
	}
}
