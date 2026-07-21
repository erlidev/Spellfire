package config

import (
	"os"
	"strconv"
	"time"
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

func Load() Config {
	return Config{
		Address:         env("SPELLFIRE_ADDRESS", ":8080"),
		DatabasePath:    env("SPELLFIRE_DATABASE", "spellfire.db"),
		WebRoot:         env("SPELLFIRE_WEB_ROOT", "dist"),
		TickRate:        envInt("SPELLFIRE_TICK_RATE", 60),
		SendRate:        envInt("SPELLFIRE_SEND_RATE", 20),
		AOIRadius:       float64(envInt("SPELLFIRE_AOI_RADIUS", 1200)),
		MaxRewind:       time.Duration(envInt("SPELLFIRE_MAX_REWIND_MS", 200)) * time.Millisecond,
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
