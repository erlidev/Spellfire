package config

import (
	"os"
	"strconv"
	"time"

	"spellfire/server/internal/tuning"
)

type Config struct {
	Address         string
	DatabasePath    string
	WebRoot         string
	TickRate        int
	SendRate        int
	AOIRadius       float64
	MaxRewind       time.Duration
	SessionLifetime time.Duration
	ShutdownTimeout time.Duration
}

// Load reads deployment configuration. The netcode defaults come from the
// simulation tuning table, so an unset environment reproduces exactly what the
// tables — and therefore the client bundle — declare.
func Load(simulation tuning.Simulation) Config {
	return Config{
		Address:         env("SPELLFIRE_ADDRESS", ":8080"),
		DatabasePath:    env("SPELLFIRE_DATABASE", "spellfire.db"),
		WebRoot:         env("SPELLFIRE_WEB_ROOT", "dist"),
		TickRate:        envInt("SPELLFIRE_TICK_RATE", simulation.TickRate),
		SendRate:        envInt("SPELLFIRE_SEND_RATE", simulation.SendRate),
		AOIRadius:       float64(envInt("SPELLFIRE_AOI_RADIUS", int(simulation.AOIRadius))),
		MaxRewind:       time.Duration(envInt("SPELLFIRE_MAX_REWIND_MS", simulation.MaxRewindMS)) * time.Millisecond,
		SessionLifetime: 7 * 24 * time.Hour,
		ShutdownTimeout: 5 * time.Second,
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
