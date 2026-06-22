package proxy

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"notion2api/backend/internal/config"
	"notion2api/backend/internal/model"
)

const (
	ModeOff    = "off"
	ModeEnv    = "env"
	ModeHTTP   = "http"
	ModeHTTPS  = "https"
	ModeSOCKS5 = "socks5"
)

type Policy struct {
	Mode     string
	URL      string
	HTTPURL  string
	HTTPSURL string
}

func PolicyFromConfig(cfg config.ProxyConfig) Policy {
	policy := Policy{
		Mode:     normalizeMode(cfg.Mode),
		URL:      strings.TrimSpace(cfg.URL),
		HTTPURL:  strings.TrimSpace(cfg.HTTPURL),
		HTTPSURL: strings.TrimSpace(cfg.HTTPSURL),
	}
	if policy.Mode == "" {
		policy.Mode = ModeOff
	}
	return policy.withFallbackURLs()
}

func PolicyForAccount(cfg config.ProxyConfig, account model.Account) Policy {
	policy := PolicyFromConfig(cfg)
	if mode := normalizeMode(account.ProxyMode); mode != "" {
		policy.Mode = mode
	}
	if value := strings.TrimSpace(account.ProxyURL); value != "" {
		policy.URL = value
	}
	if value := strings.TrimSpace(account.ProxyHTTPURL); value != "" {
		policy.HTTPURL = value
	}
	if value := strings.TrimSpace(account.ProxyHTTPSURL); value != "" {
		policy.HTTPSURL = value
	}
	return policy.withFallbackURLs()
}

func (p Policy) withFallbackURLs() Policy {
	if p.Mode == ModeEnv {
		if p.HTTPURL == "" {
			p.HTTPURL = proxyURLFromEnv("http")
		}
		if p.HTTPSURL == "" {
			p.HTTPSURL = proxyURLFromEnv("https")
		}
	}
	if p.HTTPURL == "" {
		p.HTTPURL = p.URL
	}
	if p.HTTPSURL == "" {
		p.HTTPSURL = p.URL
	}
	return p
}

func (p Policy) CacheKey() string {
	return strings.Join([]string{
		p.Mode,
		strings.TrimSpace(p.URL),
		strings.TrimSpace(p.HTTPURL),
		strings.TrimSpace(p.HTTPSURL),
	}, "|")
}

func (p Policy) ProxyURLForScheme(scheme string) string {
	if strings.EqualFold(strings.TrimSpace(scheme), "https") {
		return strings.TrimSpace(firstNonEmpty(p.HTTPSURL, p.URL))
	}
	return strings.TrimSpace(firstNonEmpty(p.HTTPURL, p.URL))
}

func (p Policy) ProxyFunc() (func(*http.Request) (*url.URL, error), error) {
	switch p.Mode {
	case "", ModeOff:
		return nil, nil
	case ModeEnv:
		return http.ProxyFromEnvironment, nil
	case ModeHTTP, ModeHTTPS, ModeSOCKS5:
		return func(req *http.Request) (*url.URL, error) {
			raw := p.ProxyURLForScheme(req.URL.Scheme)
			if raw == "" {
				return nil, nil
			}
			return ParseURL(raw, p.Mode)
		}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy mode %q", p.Mode)
	}
}

func ParseURL(raw string, fallbackScheme string) (*url.URL, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, nil
	}
	if !strings.Contains(clean, "://") {
		scheme := normalizeMode(fallbackScheme)
		if scheme == "" || scheme == ModeOff || scheme == ModeEnv {
			scheme = ModeHTTP
		}
		clean = scheme + "://" + clean
	}
	parsed, err := url.Parse(clean)
	if err != nil {
		return nil, fmt.Errorf("parse proxy url %q: %w", raw, err)
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	switch scheme {
	case ModeHTTP, ModeHTTPS, ModeSOCKS5:
		return parsed, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", parsed.Scheme)
	}
}

func normalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case ModeOff, "none", "direct", "disabled":
		return ModeOff
	case ModeEnv, "environment":
		return ModeEnv
	case ModeHTTP:
		return ModeHTTP
	case ModeHTTPS:
		return ModeHTTPS
	case ModeSOCKS5, "socks":
		return ModeSOCKS5
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func proxyURLFromEnv(scheme string) string {
	for _, key := range proxyEnvKeys(scheme) {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func proxyEnvKeys(scheme string) []string {
	if strings.EqualFold(strings.TrimSpace(scheme), "https") {
		return []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"}
	}
	return []string{"HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}
