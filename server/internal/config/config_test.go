package config

import "testing"

func TestLoadDefaultsAndOverrides(t *testing.T) {
	t.Setenv("SPELLFIRE_ADDRESS", "127.0.0.1:9000")
	t.Setenv("SPELLFIRE_TICK_RATE", "30")
	t.Setenv("SPELLFIRE_SEND_RATE", "invalid")
	cfg := Load()
	if cfg.Address != "127.0.0.1:9000" || cfg.TickRate != 30 || cfg.SendRate != 20 {
		t.Fatalf("config = %#v", cfg)
	}
}
