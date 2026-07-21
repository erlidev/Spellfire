package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"spellfire/server/internal/api"
	"spellfire/server/internal/auth"
	"spellfire/server/internal/config"
	"spellfire/server/internal/game"
	"spellfire/server/internal/store"
	"spellfire/server/internal/transport"
)

func main() {
	cfg := config.Load()
	data, err := store.OpenSQLite(cfg.DatabasePath)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer data.Close()
	authService := auth.New(data, cfg.SessionLifetime)
	tuning := game.DefaultTuning()
	tuning.TickRate, tuning.SendRate, tuning.AOIRadius, tuning.MaxRewind = cfg.TickRate, cfg.SendRate, cfg.AOIRadius, cfg.MaxRewind
	engine := game.NewEngine(tuning)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go engine.Run(ctx)
	mux := http.NewServeMux()
	api.New(authService, data).RegisterRoutes(mux)
	mux.Handle("/ws", transport.NewWebSocket(authService, data, engine))
	mux.Handle("/", spaHandler(cfg.WebRoot))
	server := &http.Server{Addr: cfg.Address, Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		slog.Info("SpellFire server listening", "address", cfg.Address, "tick_rate", cfg.TickRate, "send_rate", cfg.SendRate)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		slog.Error("shutdown", "error", err)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:")
		next.ServeHTTP(w, r)
	})
}

func spaHandler(root string) http.Handler {
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(root, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
}
