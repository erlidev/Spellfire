package game

import (
	"testing"
	"time"

	"spellfire/server/internal/model"
)

func TestReplacementKicksOldConnection(t *testing.T) {
	engine := NewEngine(DefaultTuning())
	character := model.Character{ID: "same-character", Name: "Hero", Class: model.Gunslinger}
	old := engine.Join(character, time.Now())
	engine.Join(character, time.Now())
	select {
	case <-old.Kick:
	case <-time.After(time.Second):
		t.Fatal("replacement did not kick the old connection")
	}
}
