package game

import (
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

func TestSnapshotCoversTheFullViewDistanceSquare(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	world := NewWorld(DefaultTuning())
	viewer := world.AddPlayer(model.Character{ID: "viewer", Name: "Viewer", Class: model.Gunslinger}, now)
	corner := world.AddPlayer(model.Character{ID: "corner", Name: "Corner", Class: model.Mage}, now)
	outside := world.AddPlayer(model.Character{ID: "outside", Name: "Outside", Class: model.Mage}, now)
	world.setWorldItems()

	// This corner was outside the old circular AOI even though both axes are
	// within the camera's configured maximum range.
	corner.Position = viewer.Position.Add(Vec{X: world.tuning.AOIRadius - 1, Y: world.tuning.AOIRadius - 1})
	outside.Position = viewer.Position.Add(Vec{X: world.tuning.AOIRadius + 1})
	snapshot := world.SnapshotFor(viewer.ID, now, protocol.ServerSnapshot)
	if !hasPlayer(snapshot, corner.ID) {
		t.Fatal("player in a view-square corner was omitted")
	}
	if hasPlayer(snapshot, outside.ID) {
		t.Fatal("player beyond the maximum view range was included")
	}
}
