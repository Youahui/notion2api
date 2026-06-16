package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Security SecurityConfig `yaml:"security"`
	Database DatabaseConfig `yaml:"database"`
	Upstream UpstreamConfig `yaml:"upstream"`
	Proxy    ProxyConfig    `yaml:"proxy"`
	Features FeatureConfig  `yaml:"features"`
	Storage  StorageConfig  `yaml:"storage"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type SecurityConfig struct {
	AdminTokenTTLHours int                  `yaml:"admin_token_ttl_hours"`
	BootstrapAdmin     BootstrapAdminConfig `yaml:"bootstrap_admin"`
	BootstrapAPIKey    string               `yaml:"bootstrap_api_key"`
}

type BootstrapAdminConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type DatabaseConfig struct {
	Driver                 string `yaml:"driver"`
	DSN                    string `yaml:"dsn"`
	MaxIdleConns           int    `yaml:"max_idle_conns"`
	MaxOpenConns           int    `yaml:"max_open_conns"`
	ConnMaxLifetimeSeconds int    `yaml:"conn_max_lifetime_seconds"`
}

type UpstreamConfig struct {
	BaseURL             string  `yaml:"base_url"`
	Origin              string  `yaml:"origin"`
	TimeoutSeconds      int     `yaml:"timeout_seconds"`
	PollIntervalSeconds float64 `yaml:"poll_interval_seconds"`
	PollMaxRounds       int     `yaml:"poll_max_rounds"`
}

type ProxyConfig struct {
	Mode     string `yaml:"mode"`
	URL      string `yaml:"url"`
	HTTPURL  string `yaml:"http_url"`
	HTTPSURL string `yaml:"https_url"`
}

type FeatureConfig struct {
	WebSearch             bool `yaml:"web_search"`
	GenerateImage         bool `yaml:"generate_image"`
	CSVAttachment         bool `yaml:"csv_attachment"`
	FreshThreadPerRequest bool `yaml:"fresh_thread_per_request"`
}

type StorageConfig struct {
	PersistConversations bool `yaml:"persist_conversations"`
	PersistResponses     bool `yaml:"persist_responses"`
	ResponseTTLSeconds   int  `yaml:"response_ttl_seconds"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{Host: "127.0.0.1", Port: 8787},
		Security: SecurityConfig{
			AdminTokenTTLHours: 24,
			BootstrapAdmin: BootstrapAdminConfig{
				Username: "admin",
				Password: "change-me",
			},
		},
		Database: DatabaseConfig{
			Driver:                 "sqlite",
			DSN:                    "data/notaion2api.sqlite",
			MaxIdleConns:           5,
			MaxOpenConns:           20,
			ConnMaxLifetimeSeconds: 3600,
		},
		Upstream: UpstreamConfig{
			BaseURL:             "https://www.notion.so",
			Origin:              "https://www.notion.so",
			TimeoutSeconds:      180,
			PollIntervalSeconds: 1.5,
			PollMaxRounds:       40,
		},
		Proxy: ProxyConfig{Mode: "off"},
		Features: FeatureConfig{
			WebSearch:             true,
			GenerateImage:         true,
			CSVAttachment:         true,
			FreshThreadPerRequest: true,
		},
		Storage: StorageConfig{
			PersistConversations: true,
			PersistResponses:     true,
			ResponseTTLSeconds:   3600,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if strings.TrimSpace(path) != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}
	applyEnv(&cfg)
	normalize(&cfg)
	return cfg, nil
}

func applyEnv(cfg *Config) {
	cfg.Server.Host = envString("N2A_SERVER_HOST", cfg.Server.Host)
	cfg.Server.Port = envInt("N2A_SERVER_PORT", cfg.Server.Port)
	cfg.Security.AdminTokenTTLHours = envInt("N2A_ADMIN_TOKEN_TTL_HOURS", cfg.Security.AdminTokenTTLHours)
	cfg.Security.BootstrapAdmin.Username = envString("N2A_BOOTSTRAP_ADMIN_USERNAME", cfg.Security.BootstrapAdmin.Username)
	cfg.Security.BootstrapAdmin.Password = envString("N2A_BOOTSTRAP_ADMIN_PASSWORD", cfg.Security.BootstrapAdmin.Password)
	cfg.Security.BootstrapAPIKey = envString("N2A_BOOTSTRAP_API_KEY", cfg.Security.BootstrapAPIKey)
	cfg.Database.Driver = envString("N2A_DATABASE_DRIVER", cfg.Database.Driver)
	cfg.Database.DSN = envString("N2A_DATABASE_DSN", cfg.Database.DSN)
	cfg.Proxy.Mode = envString("N2A_PROXY_MODE", cfg.Proxy.Mode)
	cfg.Proxy.URL = envString("N2A_PROXY_URL", cfg.Proxy.URL)
	cfg.Proxy.HTTPURL = envString("N2A_PROXY_HTTP_URL", cfg.Proxy.HTTPURL)
	cfg.Proxy.HTTPSURL = envString("N2A_PROXY_HTTPS_URL", cfg.Proxy.HTTPSURL)
}

func normalize(cfg *Config) {
	cfg.Database.Driver = strings.ToLower(strings.TrimSpace(cfg.Database.Driver))
	if cfg.Database.Driver == "" {
		cfg.Database.Driver = "sqlite"
	}
	if cfg.Database.MaxIdleConns <= 0 {
		cfg.Database.MaxIdleConns = 5
	}
	if cfg.Database.MaxOpenConns <= 0 {
		cfg.Database.MaxOpenConns = 20
	}
	if cfg.Security.AdminTokenTTLHours <= 0 {
		cfg.Security.AdminTokenTTLHours = 24
	}
}

func envString(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
