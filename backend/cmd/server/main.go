package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"notion2api/backend/internal/bootstrap"
)

func main() {
	configPath := flag.String("config", "configs/config.example.yaml", "path to config yaml")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := bootstrap.Run(ctx, *configPath); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
