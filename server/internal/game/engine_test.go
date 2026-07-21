package game

import (
	"context"
	"testing"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

func TestEngineJoinInputSnapshotAndLeave(t *testing.T) {
	tuning := DefaultTuning()
	engine := NewEngine(tuning)
	now := time.Now()
	client := engine.Join(model.Character{ID: "p", Name: "Player", Class: model.Gunslinger}, now)
	select {
	case welcome := <-client.Send:
		if len(welcome) == 0 {
			t.Fatal("empty welcome")
		}
	case <-time.After(time.Second):
		t.Fatal("welcome not queued")
	}
	engine.Input("p", protocol.Input{Sequence: 1, Buttons: ButtonRight, AimX: 1})
	ctx, cancel := context.WithCancel(context.Background())
	go engine.Run(ctx)
	time.Sleep(45 * time.Millisecond)
	cancel()
	select {
	case snapshot := <-client.Send:
		if len(snapshot) == 0 {
			t.Fatal("empty snapshot")
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot not queued")
	}
	engine.Leave(client)
	engine.mu.Lock()
	_, clientExists := engine.clients["p"]
	_, playerExists := engine.world.players["p"]
	engine.mu.Unlock()
	if clientExists || playerExists {
		t.Fatal("leave retained client or player")
	}
}

func TestEngineOldConnectionCannotRemoveReplacement(t *testing.T) {
	engine := NewEngine(DefaultTuning())
	character := model.Character{ID: "p", Name: "Player", Class: model.Mage}
	old := engine.Join(character, time.Now())
	replacement := engine.Join(character, time.Now())
	engine.Leave(old)
	engine.mu.Lock()
	got := engine.clients["p"]
	engine.mu.Unlock()
	if got != replacement {
		t.Fatal("old connection removed replacement")
	}
}
