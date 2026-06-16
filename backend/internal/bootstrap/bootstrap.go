package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/repository"
	"notaion2api/backend/internal/router"
	"notaion2api/backend/internal/service"
)

func Run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	store, err := repository.OpenDatabase(cfg.Database)
	if err != nil {
		return err
	}
	if err := store.AutoMigrate(); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}

	admins := service.NewAdminService(store, cfg.Security.AdminTokenTTLHours)
	keys := service.NewAPIKeyService(store, cfg.Security.APIKeyPrefix)
	models := service.NewModelService(store)
	accounts := service.NewAccountService(store, cfg)
	notion := service.NewNotionGateway(cfg)
	sticky := service.NewStickySessionStore(cfg.Sticky)
	openai := service.NewOpenAIService(cfg, models, accounts, notion, sticky)

	if err := admins.SeedBootstrapAdmin(ctx, cfg.Security.BootstrapAdmin); err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}
	if err := keys.SeedBootstrapAPIKey(ctx, cfg.Security.BootstrapAPIKey); err != nil {
		return fmt.Errorf("seed API 密钥: %w", err)
	}
	if err := models.SeedDefaults(ctx); err != nil {
		return fmt.Errorf("seed models: %w", err)
	}

	engine := router.New(cfg, admins, keys, models, accounts, openai, notion, store.DB())
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{Addr: addr, Handler: engine}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("notaion2api listening", "addr", addr, "database_driver", cfg.Database.Driver)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		notion.Close()
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
