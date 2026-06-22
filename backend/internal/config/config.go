package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Security    SecurityConfig    `yaml:"security"`
	Database    DatabaseConfig    `yaml:"database"`
	Upstream    UpstreamConfig    `yaml:"upstream"`
	Proxy       ProxyConfig       `yaml:"proxy"`
	Features    FeatureConfig     `yaml:"features"`
	Dispatch    DispatchConfig    `yaml:"dispatch"`
	Sticky      StickyConfig      `yaml:"sticky"`
	Storage     StorageConfig     `yaml:"storage"`
	LoginHelper LoginHelperConfig `yaml:"login_helper"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type SecurityConfig struct {
	AdminTokenTTLHours int                  `yaml:"admin_token_ttl_hours"`
	BootstrapAdmin     BootstrapAdminConfig `yaml:"bootstrap_admin"`
	BootstrapAPIKey    string               `yaml:"bootstrap_api_key"`
	APIKeyPrefix       string               `yaml:"api_key_prefix"`
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
	WebSearch                     bool `yaml:"web_search"`
	GenerateImage                 bool `yaml:"generate_image"`
	CSVAttachment                 bool `yaml:"csv_attachment"`
	FreshThreadPerRequest         bool `yaml:"fresh_thread_per_request"`
	PreserveSystemMessagesAsSteps bool `yaml:"preserve_system_messages_as_steps"`
}

type DispatchConfig struct {
	ProbeCacheTTLSeconds int `yaml:"probe_cache_ttl_seconds"`
}

type StickyConfig struct {
	Enabled             bool    `yaml:"enabled"`
	TTLSeconds          int     `yaml:"ttl_seconds"`
	LocalEnabled        bool    `yaml:"local_enabled"`
	RedisURL            string  `yaml:"redis_url"`
	RedisKeyPrefix      string  `yaml:"redis_key_prefix"`
	RedisTimeoutSeconds float64 `yaml:"redis_timeout_seconds"`
}

type StorageConfig struct {
	PersistConversations bool `yaml:"persist_conversations"`
	PersistResponses     bool `yaml:"persist_responses"`
	ResponseTTLSeconds   int  `yaml:"response_ttl_seconds"`
}

type LoginHelperConfig struct {
	SessionsDir string `yaml:"sessions_dir"`
	TimeoutSec  int    `yaml:"timeout_sec"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{Host: "127.0.0.1", Port: 8787},
		Security: SecurityConfig{
			AdminTokenTTLHours: 24,
			APIKeyPrefix:       "n2a",
			BootstrapAdmin: BootstrapAdminConfig{
				Username: "admin",
				Password: "change-me",
			},
		},
		Database: DatabaseConfig{
			Driver:                 "sqlite",
			DSN:                    "data/notion2api.sqlite",
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
			WebSearch:                     true,
			GenerateImage:                 true,
			CSVAttachment:                 true,
			FreshThreadPerRequest:         true,
			PreserveSystemMessagesAsSteps: false,
		},
		Dispatch: DispatchConfig{
			ProbeCacheTTLSeconds: 60,
		},
		Sticky: StickyConfig{
			Enabled:             true,
			TTLSeconds:          604800,
			LocalEnabled:        true,
			RedisKeyPrefix:      "n2a:sticky:thread:",
			RedisTimeoutSeconds: 2,
		},
		Storage: StorageConfig{
			PersistConversations: true,
			PersistResponses:     true,
			ResponseTTLSeconds:   3600,
		},
		LoginHelper: LoginHelperConfig{
			SessionsDir: "data/notion_accounts",
			TimeoutSec:  120,
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
	cfg.Security.APIKeyPrefix = envString("N2A_API_KEY_PREFIX", cfg.Security.APIKeyPrefix)
	cfg.Database.Driver = envString("N2A_DATABASE_DRIVER", cfg.Database.Driver)
	cfg.Database.DSN = envString("N2A_DATABASE_DSN", cfg.Database.DSN)
	cfg.Proxy.Mode = envString("N2A_PROXY_MODE", cfg.Proxy.Mode)
	cfg.Proxy.URL = envString("N2A_PROXY_URL", cfg.Proxy.URL)
	cfg.Proxy.HTTPURL = envString("N2A_PROXY_HTTP_URL", cfg.Proxy.HTTPURL)
	cfg.Proxy.HTTPSURL = envString("N2A_PROXY_HTTPS_URL", cfg.Proxy.HTTPSURL)
	cfg.Dispatch.ProbeCacheTTLSeconds = envInt("N2A_DISPATCH_PROBE_CACHE_TTL_SECONDS", cfg.Dispatch.ProbeCacheTTLSeconds)
	cfg.Sticky.Enabled = envBool("N2A_STICKY_ENABLED", cfg.Sticky.Enabled)
	cfg.Sticky.TTLSeconds = envInt("N2A_STICKY_TTL_SECONDS", cfg.Sticky.TTLSeconds)
	cfg.Sticky.LocalEnabled = envBool("N2A_STICKY_LOCAL_ENABLED", cfg.Sticky.LocalEnabled)
	cfg.Sticky.RedisURL = envString("N2A_STICKY_REDIS_URL", cfg.Sticky.RedisURL)
	cfg.Sticky.RedisKeyPrefix = envString("N2A_STICKY_REDIS_KEY_PREFIX", cfg.Sticky.RedisKeyPrefix)
	cfg.Sticky.RedisTimeoutSeconds = envFloat("N2A_STICKY_REDIS_TIMEOUT_SECONDS", cfg.Sticky.RedisTimeoutSeconds)
	cfg.LoginHelper.SessionsDir = envString("N2A_LOGIN_HELPER_SESSIONS_DIR", cfg.LoginHelper.SessionsDir)
	cfg.LoginHelper.TimeoutSec = envInt("N2A_LOGIN_HELPER_TIMEOUT_SEC", cfg.LoginHelper.TimeoutSec)
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
	cfg.Security.APIKeyPrefix = strings.Trim(strings.TrimSpace(cfg.Security.APIKeyPrefix), "_")
	if cfg.Security.APIKeyPrefix == "" {
		cfg.Security.APIKeyPrefix = "n2a"
	}
	if cfg.LoginHelper.SessionsDir == "" {
		cfg.LoginHelper.SessionsDir = "data/notion_accounts"
	}
	if cfg.LoginHelper.TimeoutSec <= 0 {
		cfg.LoginHelper.TimeoutSec = 120
	}
	if cfg.Dispatch.ProbeCacheTTLSeconds < 0 {
		cfg.Dispatch.ProbeCacheTTLSeconds = 0
	}
	if cfg.Sticky.TTLSeconds <= 0 {
		cfg.Sticky.TTLSeconds = 604800
	}
	if strings.TrimSpace(cfg.Sticky.RedisKeyPrefix) == "" {
		cfg.Sticky.RedisKeyPrefix = "n2a:sticky:thread:"
	}
	if cfg.Sticky.RedisTimeoutSeconds <= 0 {
		cfg.Sticky.RedisTimeoutSeconds = 2
	}
	if !cfg.Sticky.LocalEnabled && strings.TrimSpace(cfg.Sticky.RedisURL) == "" {
		cfg.Sticky.LocalEnabled = true
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

func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
