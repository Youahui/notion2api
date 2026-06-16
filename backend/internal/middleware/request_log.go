package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxDevLogBodyBytes = 64 * 1024

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		c.Header("X-Request-ID", requestID)
		c.Set("request_id", requestID)
		c.Next()
	}
}

func DevRequestDetailsLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := readAndRestoreRequestBody(c.Request)
		if err != nil {
			slog.Warn("read request body for dev log failed",
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"request_id", c.GetString("request_id"),
				"error", err,
			)
		}

		slog.Info("http request details",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"query", c.Request.URL.RawQuery,
			"client_ip", c.ClientIP(),
			"proto", c.Request.Proto,
			"host", c.Request.Host,
			"user_agent", c.Request.UserAgent(),
			"request_id", c.GetString("request_id"),
			"headers", redactedHeaders(c.Request.Header),
			"body", requestBodyForLog(c.Request.Header.Get("Content-Type"), body),
		)

		c.Next()
	}
}

func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"request_id", c.GetString("request_id"),
		)
	}
}

func readAndRestoreRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func redactedHeaders(headers http.Header) map[string][]string {
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		if isSensitiveKey(key) {
			out[key] = []string{"[REDACTED]"}
			continue
		}
		out[key] = append([]string(nil), values...)
	}
	return out
}

func requestBodyForLog(contentType string, body []byte) any {
	if len(body) == 0 {
		return ""
	}

	truncated := len(body) > maxDevLogBodyBytes
	if truncated {
		body = body[:maxDevLogBodyBytes]
	}

	mediaType := contentType
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		mediaType = parsed
	}

	if strings.Contains(mediaType, "json") {
		var value any
		if err := json.Unmarshal(body, &value); err == nil {
			return map[string]any{
				"truncated": truncated,
				"json":      redactJSONValue(value),
			}
		}
	}

	if isTextualContent(mediaType) {
		return map[string]any{
			"truncated": truncated,
			"text":      redactSensitiveText(string(body)),
		}
	}

	return fmt.Sprintf("[omitted %d byte %s body]", len(body), firstNonEmpty(mediaType, "unknown"))
}

func redactJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = redactJSONValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = redactJSONValue(item)
		}
		return out
	default:
		return typed
	}
}

func isSensitiveKey(key string) bool {
	clean := strings.ToLower(strings.TrimSpace(key))
	return clean == "authorization" ||
		clean == "api_key" ||
		clean == "cookie" ||
		clean == "set-cookie" ||
		clean == "x-admin-token" ||
		clean == "x-api-key" ||
		strings.Contains(clean, "password") ||
		strings.Contains(clean, "secret") ||
		strings.Contains(clean, "token") ||
		strings.Contains(clean, "cookie")
}

func isTextualContent(mediaType string) bool {
	return strings.HasPrefix(mediaType, "text/") ||
		strings.Contains(mediaType, "json") ||
		mediaType == "application/x-www-form-urlencoded" ||
		mediaType == "application/xml" ||
		mediaType == "application/graphql"
}

func redactSensitiveText(value string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		clean := strings.ToLower(line)
		if strings.Contains(clean, "password") ||
			strings.Contains(clean, "secret") ||
			strings.Contains(clean, "token") ||
			strings.Contains(clean, "cookie") {
			lines[index] = "[REDACTED]"
		}
	}
	return strings.Join(lines, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
