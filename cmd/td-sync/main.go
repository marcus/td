package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/marcus/td/internal/api"
	"github.com/marcus/td/internal/serverdb"
)

func main() {
	// Route to admin subcommands if present
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		runAdmin(os.Args[2:])
		return
	}

	cfg := api.LoadConfig()

	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(cfg.LogFormat) == "text" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))

	store, err := serverdb.Open(cfg.ServerDBPath)
	if err != nil {
		slog.Error("open server db", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	srv, err := api.NewServer(cfg, store)
	if err != nil {
		slog.Error("create server", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(); err != nil {
		slog.Error("start server", "err", err)
		os.Exit(1)
	}
	slog.Info("server started", "addr", cfg.ListenAddr)

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}
}
