package config

import (
	"testing"

	"spellfire/server/internal/tuning"
)

func TestLoadFallsBackToTheTuningTableAndAcceptsOverrides(t *testing.T) {
	simulation := tuning.MustLoad().Simulation
	t.Setenv("SPELLFIRE_ADDRESS", "127.0.0.1:9000")
	t.Setenv("SPELLFIRE_TICK_RATE", "30")
	t.Setenv("SPELLFIRE_SEND_RATE", "invalid")
	cfg := Load(simulation)
	if cfg.Address != "127.0.0.1:9000" || cfg.TickRate != 30 {
		t.Fatalf("config = %#v", cfg)
	}
	if cfg.SendRate != simulation.SendRate || cfg.AOIRadius != simulation.AOIRadius || cfg.MaxRewind != simulation.MaxRewind() {
		t.Fatalf("unset values did not fall back to the simulation table: %#v", cfg)
	}
}
