package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxDevLogBodyBytes = 64 * 1024

var logMarkdownFileLinkPattern = regexp.MustCompile(`\[([^\]\n]{1,512})\]\(([^)\s]+)\)`)

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
	mediaType := contentType
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		mediaType = parsed
	}

	if strings.Contains(mediaType, "json") {
		var value any
		if err := json.Unmarshal(body, &value); err == nil {
			if truncated {
				return map[string]any{
					"truncated":    true,
					"json_summary": summarizeJSONBodyForLog(value),
				}
			}
			return map[string]any{
				"truncated": truncated,
				"json":      redactJSONValue(value),
			}
		}
	}

	if truncated {
		body = body[:maxDevLogBodyBytes]
	}

	if isTextualContent(mediaType) {
		return map[string]any{
			"truncated": truncated,
			"text":      redactSensitiveText(string(body)),
		}
	}

	return fmt.Sprintf("[omitted %d byte %s body]", len(body), firstNonEmpty(mediaType, "unknown"))
}

func summarizeJSONBodyForLog(value any) any {
	body, ok := value.(map[string]any)
	if !ok {
		return map[string]any{"type": fmt.Sprintf("%T", value)}
	}
	summary := map[string]any{
		"keys": sortedMapKeys(body),
	}
	for _, key := range []string{"model", "stream", "store", "tool_choice", "parallel_tool_calls", "prompt_cache_key", "previous_response_id"} {
		if value, ok := body[key]; ok {
			summary[key] = redactJSONValue(value)
		}
	}
	if metadata, ok := body["client_metadata"].(map[string]any); ok {
		client := map[string]any{}
		for _, key := range []string{"session_id", "thread_id", "turn_id"} {
			if value, ok := metadata[key]; ok {
				client[key] = value
			}
		}
		if len(client) > 0 {
			summary["client_metadata"] = client
		}
	}
	if text, ok := body["text"].(map[string]any); ok {
		if format, ok := text["format"].(map[string]any); ok {
			summary["text_format"] = map[string]any{
				"type": format["type"],
				"name": format["name"],
			}
		}
	}
	if tools := summarizeToolsForLog(body["tools"]); len(tools) > 0 {
		summary["tools"] = tools
	}
	if input := summarizeInputForLog(body["input"]); len(input) > 0 {
		summary["input"] = input
	}
	if attachments := summarizeInputForLog(body["attachments"]); len(attachments) > 0 {
		summary["attachments"] = attachments
	}
	return summary
}

func summarizeToolsForLog(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name := stringValue(tool["name"]); name != "" {
			names = append(names, name)
			continue
		}
		if function, ok := tool["function"].(map[string]any); ok {
			if name := stringValue(function["name"]); name != "" {
				names = append(names, name)
			}
		}
	}
	if len(names) > 20 {
		return append(names[:20], fmt.Sprintf("...+%d", len(names)-20))
	}
	return names
}

func summarizeInputForLog(value any) map[string]any {
	summary := map[string]any{}
	roleCounts := map[string]int{}
	typeCounts := map[string]int{}
	userTexts := []string{}
	fileRefs := []string{}
	var walk func(any)
	walk = func(item any) {
		switch typed := item.(type) {
		case string:
			if text := strings.TrimSpace(typed); text != "" {
				typeCounts["input_text"]++
				userTexts = append(userTexts, truncateLogText(redactSensitiveText(text), 280))
				fileRefs = append(fileRefs, fileRefsFromLogText(text)...)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case map[string]any:
			role := stringValue(typed["role"])
			itemType := stringValue(typed["type"])
			if role != "" {
				roleCounts[role]++
			}
			if itemType != "" {
				typeCounts[itemType]++
			}
			if role == "user" {
				if text := textFromLogContent(typed["content"]); text != "" {
					userTexts = append(userTexts, truncateLogText(redactSensitiveText(text), 280))
					fileRefs = append(fileRefs, fileRefsFromLogText(text)...)
				}
			}
			if content, ok := typed["content"]; ok {
				walk(content)
			}
		}
	}
	walk(value)
	if len(roleCounts) > 0 {
		summary["roles"] = roleCounts
	}
	if len(typeCounts) > 0 {
		summary["types"] = typeCounts
	}
	if len(userTexts) > 0 {
		if len(userTexts) > 3 {
			userTexts = userTexts[len(userTexts)-3:]
		}
		summary["last_user_text"] = userTexts
	}
	if refs := uniqueStrings(fileRefs, 12); len(refs) > 0 {
		summary["file_refs"] = refs
	}
	return summary
}

func textFromLogContent(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := []string{}
		for _, item := range typed {
			if text := textFromLogContent(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case map[string]any:
		if text := stringValue(typed["text"]); text != "" {
			return text
		}
		return textFromLogContent(typed["content"])
	default:
		return ""
	}
}

func fileRefsFromLogText(text string) []string {
	matches := logMarkdownFileLinkPattern.FindAllStringSubmatch(text, -1)
	refs := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) >= 3 {
			refs = append(refs, firstNonEmpty(strings.TrimSpace(match[2]), strings.TrimSpace(match[1])))
		}
	}
	return refs
}

func sortedMapKeys(items map[string]any) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func truncateLogText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "...[truncated]"
}

func uniqueStrings(values []string, max int) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
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
