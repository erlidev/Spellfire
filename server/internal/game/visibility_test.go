package game

import (
	"testing"
	"time"

	"spellfire/server/internal/model"
)

func TestTerrainOccludesSnapshotsUntilItStopsStanding(t *testing.T) {
	w, now := testWorld()
	viewer := addTestPlayer(w, "viewer", model.Gunslinger, Vec{1200, 0}, now)
	behindCover := addTestPlayer(w, "behind-cover", model.Mage, Vec{1500, 0}, now)
	besideCover := addTestPlayer(w, "beside-cover", model.Mage, Vec{1500, 160}, now)
	wall := testWorldItem(w, "wall", "stone-wall", Vec{1350, 0}, CollisionObject{Type: CollisionCircle, Radius: 35})
	w.worldItems = []*Entity{wall}

	if visible(w, viewer.ID, behindCover.ID, now) {
		t.Fatal("a stone wall did not hide the body directly behind it")
	}
	if !visible(w, viewer.ID, besideCover.ID, now) {
		t.Fatal("a stone wall hid a body whose sightline does not cross it")
	}
	if visible(w, behindCover.ID, viewer.ID, now) {
		// Visibility must be symmetric around solid cover. If this fails while
		// the first assertion passes, one side has gained information the other
		// side cannot receive.
		t.Fatal("terrain occlusion was not symmetric")
	}

	// Destruction removes collision and sight blocking on the same authoritative
	// transition, even while the shared deletion fade remains in snapshots.
	wall.TakeDamage(wall.Health)
	wall.Delete(now.Add(time.Millisecond))
	if !visible(w, viewer.ID, behindCover.ID, now.Add(2*time.Millisecond)) {
		t.Fatal("destroyed terrain continued to hide a body")
	}
}

func TestCircleAndBoxTerrainShareTheSightRule(t *testing.T) {
	w, _ := testWorld()
	from, to := Vec{1200, 0}, Vec{1500, 0}

	w.worldItems = []*Entity{testWorldItem(w, "stone-wall", "stone-wall", Vec{1350, 0}, CollisionObject{Type: CollisionCircle, Radius: 30})}
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

func TestCollisionDoesNotImplyVisionOcclusion(t *testing.T) {
	w, _ := testWorld()
	tree := testWorldItem(w, "tree", "tree", Vec{1350, 0}, CollisionObject{Type: CollisionCircle, Radius: 30})
	w.worldItems = []*Entity{tree}
	if w.terrainOccluded(Vec{1200, 0}, Vec{1500, 0}) {
		t.Fatal("a collidable tree occluded vision without the entity attribute")
	}
}

func TestAnyVisiblePartKeepsBodyInSnapshot(t *testing.T) {
	w, now := testWorld()
	viewer := addTestPlayer(w, "viewer", model.Gunslinger, Vec{1200, 0}, now)
	target := addTestPlayer(w, "target", model.Mage, Vec{1400, 0}, now)
	wall := testWorldItem(w, "wall", "wall", Vec{1300, 0}, CollisionObject{Type: CollisionBox, HalfWidth: 12, HalfHeight: 6})
	w.worldItems = []*Entity{wall}

	if !visible(w, viewer.ID, target.ID, now) {
		t.Fatal("a body with a visible edge was omitted because its centre was covered")
	}
	wall.CollisionObjects[0].HalfHeight = 60
	if visible(w, viewer.ID, target.ID, now) {
		t.Fatal("a body whose complete silhouette was covered remained in the snapshot")
	}
}

func TestSmokeBlocksSightlineAndSuppliesShadowCollider(t *testing.T) {
	w, now := testWorld()
	viewer := addTestPlayer(w, "viewer", model.Gunslinger, Vec{1200, 0}, now)
	target := addTestPlayer(w, "target", model.Mage, Vec{1700, 0}, now)
	field := *w.tuning.Tables.Abilities["smoke-throw"].Deployable
	cloud := w.deploy("", field, Vec{1450, 0}, "", now)

	if visible(w, viewer.ID, target.ID, now) {
		t.Fatal("smoke between two bodies did not block the complete target silhouette")
	}
	snapshot := w.SnapshotFor(viewer.ID, now, 0)
	found := 0
	for _, collider := range snapshot.Colliders {
		if collider.EntityID == cloud.ID && collider.Kind == "smoke" && collider.Shape == "circle" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("standing smoke supplied %d collider records, want one compact analytic shape", found)
	}
}

func TestAutomaticTargetingRequiresLineOfSight(t *testing.T) {
	w, now := testWorld()
	owner := addTestPlayer(w, "owner", model.Mage, Vec{1200, 0}, now)
	blocked := addTestPlayer(w, "blocked", model.Gunslinger, Vec{1500, 0}, now)
	open := addTestPlayer(w, "open", model.Gunslinger, Vec{1200, 300}, now)
	w.worldItems = []*Entity{testWorldItem(w, "wall", "wall", Vec{1300, 0}, CollisionObject{Type: CollisionBox, HalfWidth: 18, HalfHeight: 60})}

	if got := w.nearestPlayer(owner.Position, 500, map[string]bool{owner.ID: true}, owner); got != open {
		id := "<nil>"
		if got != nil {
			id = got.ID
		}
		t.Fatalf("automatic target = %s, want the farther visible body", id)
	}

	// Smoke participates in the same acquisition rule without becoming solid:
	// it blocks the crossed sightline and leaves a target in another direction.
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
