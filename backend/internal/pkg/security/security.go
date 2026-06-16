package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const DefaultAPIKeyPrefix = "n2a"

func HashPassword(password string) (string, error) {
	raw, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func CheckPassword(hash string, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func GenerateOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func GenerateAPIKey(apiKeyPrefix string) (secret string, prefix string, suffix string, err error) {
	token, err := GenerateOpaqueToken()
	if err != nil {
		return "", "", "", err
	}
	apiKeyPrefix = NormalizeAPIKeyPrefix(apiKeyPrefix)
	secret = apiKeyPrefix + "_" + token
	prefix, suffix = APIKeyParts(secret)
	return secret, prefix, suffix, nil
}

func NormalizeAPIKeyPrefix(apiKeyPrefix string) string {
	apiKeyPrefix = strings.Trim(strings.TrimSpace(apiKeyPrefix), "_")
	if apiKeyPrefix == "" {
		return DefaultAPIKeyPrefix
	}
	return apiKeyPrefix
}

func APIKeyParts(secret string) (prefix string, suffix string) {
	prefix = secret
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	suffix = secret
	if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	}
	return prefix, suffix
}

func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func SecretHashEqual(hash string, secret string) bool {
	candidate := HashSecret(secret)
	return subtle.ConstantTimeCompare([]byte(hash), []byte(candidate)) == 1
}

func BearerToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const prefix = "bearer "
	if strings.HasPrefix(strings.ToLower(value), prefix) {
		return strings.TrimSpace(value[len(prefix):])
	}
	return value
}

func EstimateTokens(text string) int64 {
	runes := int64(utf8.RuneCountInString(text))
	if runes == 0 {
		return 0
	}
	tokens := runes / 4
	if runes%4 != 0 {
		tokens++
	}
	if tokens == 0 {
		return 1
	}
	return tokens
}
