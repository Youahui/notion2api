package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Store struct {
	db *gorm.DB
}

func OpenDatabase(cfg config.DatabaseConfig) (*Store, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "sqlite"
	}

	var dialector gorm.Dialector
	switch driver {
	case "sqlite":
		dsn := cfg.DSN
		if dsn == "" {
			dsn = "data/notaion2api.sqlite"
		}
		if err := ensureSQLiteDir(dsn); err != nil {
			return nil, err
		}
		dialector = sqlite.Open(dsn)
	case "postgres", "pgsql":
		if strings.TrimSpace(cfg.DSN) == "" {
			return nil, fmt.Errorf("database.dsn is required for postgres")
		}
		dialector = postgres.Open(cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("database handle: %w", err)
	}
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	if cfg.ConnMaxLifetimeSeconds > 0 {
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeSeconds) * time.Second)
	}
	return &Store{db: db}, nil
}

func ensureSQLiteDir(dsn string) error {
	if strings.Contains(dsn, "file:") || strings.Contains(dsn, "?") || dsn == ":memory:" {
		return nil
	}
	dir := filepath.Dir(dsn)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite dir: %w", err)
	}
	return nil
}

func (s *Store) DB() *gorm.DB {
	return s.db
}

func (s *Store) AutoMigrate() error {
	return s.db.AutoMigrate(
		&model.AdminUser{},
		&model.AdminToken{},
		&model.APIKey{},
		&model.APIKeyUsage{},
		&model.Account{},
		&model.Model{},
		&model.Conversation{},
		&model.Message{},
		&model.Response{},
		&model.Setting{},
		&model.RequestLog{},
	)
}
