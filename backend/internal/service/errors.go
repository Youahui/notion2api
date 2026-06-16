package service

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"notaion2api/backend/internal/config"
)

var (
	ErrInvalid      = errors.New("invalid input")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
)

func isSessionRetryableError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *NotionAPIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
			return true
		}
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "forbidden") ||
		strings.Contains(message, "session expired") ||
		strings.Contains(message, "client version") ||
		strings.Contains(message, "notion-client-version") ||
		strings.Contains(message, "login") ||
		strings.Contains(message, "notion_user_id missing") ||
		strings.Contains(message, "cookie jar empty")
}

func dispatchProbeTimeout(cfg config.Config) time.Duration {
	seconds := cfg.Upstream.TimeoutSeconds
	if seconds <= 0 || seconds > 20 {
		seconds = 20
	}
	if seconds < 5 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}
