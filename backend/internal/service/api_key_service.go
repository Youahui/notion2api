package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/model"
	"notaion2api/backend/internal/pkg/security"
	"notaion2api/backend/internal/repository"

	"gorm.io/gorm"
)

type APIKeyService struct {
	store        *repository.Store
	apiKeyPrefix string
}

type UsageRecord struct {
	APIKeyID         uint
	Method           string
	Route            string
	StatusCode       int
	LatencyMs        int64
	Model            string
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	Estimated        bool
	ErrorSummary     string
}

func NewAPIKeyService(store *repository.Store, apiKeyPrefix string) *APIKeyService {
	return &APIKeyService{store: store, apiKeyPrefix: security.NormalizeAPIKeyPrefix(apiKeyPrefix)}
}

func (s *APIKeyService) SeedBootstrapAPIKey(ctx context.Context, secret string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	count, err := s.store.CountAPIKeys(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	prefix, suffix := security.APIKeyParts(secret)
	return s.store.CreateAPIKey(ctx, &model.APIKey{
		Name:      "bootstrap",
		KeyHash:   security.HashSecret(secret),
		APIKey:    secret,
		KeyPrefix: prefix,
		KeySuffix: suffix,
		Enabled:   true,
	})
}

func (s *APIKeyService) Create(ctx context.Context, ownerAdminID uint, req dto.CreateAPIKeyRequest) (dto.CreateAPIKeyResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return dto.CreateAPIKeyResponse{}, ErrInvalid
	}
	secret, prefix, suffix, err := security.GenerateAPIKey(s.apiKeyPrefix)
	if err != nil {
		return dto.CreateAPIKeyResponse{}, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	key := model.APIKey{
		Name:            name,
		KeyHash:         security.HashSecret(secret),
		APIKey:          secret,
		KeyPrefix:       prefix,
		KeySuffix:       suffix,
		Enabled:         enabled,
		OwnerAdminID:    &ownerAdminID,
		ExpiresAt:       req.ExpiresAt,
		TokenQuotaTotal: req.TokenQuotaTotal,
		TokenQuotaDaily: req.TokenQuotaDaily,
	}
	if err := s.store.CreateAPIKey(ctx, &key); err != nil {
		return dto.CreateAPIKeyResponse{}, err
	}
	return dto.CreateAPIKeyResponse{APIKeyDTO: APIKeyDTO(key), Secret: secret}, nil
}

func (s *APIKeyService) List(ctx context.Context) ([]dto.APIKeyDTO, error) {
	keys, err := s.store.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.APIKeyDTO, 0, len(keys))
	for _, key := range keys {
		out = append(out, APIKeyDTO(key))
	}
	return out, nil
}

func (s *APIKeyService) Get(ctx context.Context, id uint) (dto.APIKeyDTO, error) {
	key, err := s.store.GetAPIKey(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.APIKeyDTO{}, ErrNotFound
		}
		return dto.APIKeyDTO{}, err
	}
	return APIKeyDTO(key), nil
}

func (s *APIKeyService) Update(ctx context.Context, id uint, req dto.UpdateAPIKeyRequest) (dto.APIKeyDTO, error) {
	key, err := s.store.GetAPIKey(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.APIKeyDTO{}, ErrNotFound
		}
		return dto.APIKeyDTO{}, err
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		key.Name = strings.TrimSpace(*req.Name)
	}
	if req.ExpiresAt != nil {
		key.ExpiresAt = req.ExpiresAt
	}
	if req.TokenQuotaTotal != nil {
		key.TokenQuotaTotal = *req.TokenQuotaTotal
	}
	if req.TokenQuotaDaily != nil {
		key.TokenQuotaDaily = *req.TokenQuotaDaily
	}
	if err := s.store.SaveAPIKey(ctx, &key); err != nil {
		return dto.APIKeyDTO{}, err
	}
	return APIKeyDTO(key), nil
}

func (s *APIKeyService) SetEnabled(ctx context.Context, id uint, enabled bool) (dto.APIKeyDTO, error) {
	key, err := s.store.GetAPIKey(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.APIKeyDTO{}, ErrNotFound
		}
		return dto.APIKeyDTO{}, err
	}
	key.Enabled = enabled
	if err := s.store.SaveAPIKey(ctx, &key); err != nil {
		return dto.APIKeyDTO{}, err
	}
	return APIKeyDTO(key), nil
}

func (s *APIKeyService) Delete(ctx context.Context, id uint) error {
	if _, err := s.store.GetAPIKey(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	return s.store.DeleteAPIKey(ctx, id)
}

func (s *APIKeyService) Authenticate(ctx context.Context, secret string) (model.APIKey, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return model.APIKey{}, ErrUnauthorized
	}
	prefix, _ := security.APIKeyParts(secret)
	candidates, err := s.store.FindAPIKeysByPrefix(ctx, prefix)
	if err != nil {
		return model.APIKey{}, err
	}
	now := time.Now()
	for _, key := range candidates {
		if !security.SecretHashEqual(key.KeyHash, secret) {
			continue
		}
		if !key.Enabled {
			return model.APIKey{}, ErrUnauthorized
		}
		if key.ExpiresAt != nil && key.ExpiresAt.Before(now) {
			return model.APIKey{}, ErrUnauthorized
		}
		if key.TokenQuotaTotal > 0 && key.TotalTokens >= key.TokenQuotaTotal {
			return model.APIKey{}, ErrForbidden
		}
		if key.TokenQuotaDaily > 0 {
			usedToday, err := s.store.SumUsageForDate(ctx, key.ID, now.Format("2006-01-02"))
			if err != nil {
				return model.APIKey{}, err
			}
			if usedToday >= key.TokenQuotaDaily {
				return model.APIKey{}, ErrForbidden
			}
		}
		return key, nil
	}
	return model.APIKey{}, ErrUnauthorized
}

func (s *APIKeyService) RecordUsage(ctx context.Context, usage UsageRecord) error {
	now := time.Now()
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if usage.Model == "" {
		usage.Model = "unknown"
	}
	if err := s.store.IncrementAPIKeyUsage(ctx, usage.APIKeyID, usage.TotalTokens, now); err != nil {
		return err
	}
	estimatedTokens := int64(0)
	if usage.Estimated {
		estimatedTokens = usage.TotalTokens
	}
	if err := s.store.AddUsage(ctx, model.APIKeyUsage{
		APIKeyID:         usage.APIKeyID,
		Date:             now.Format("2006-01-02"),
		Model:            usage.Model,
		RequestCount:     1,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		EstimatedTokens:  estimatedTokens,
	}); err != nil {
		return err
	}
	return s.store.CreateRequestLog(ctx, &model.RequestLog{
		APIKeyID:         &usage.APIKeyID,
		Method:           usage.Method,
		Route:            usage.Route,
		StatusCode:       usage.StatusCode,
		LatencyMs:        usage.LatencyMs,
		Model:            usage.Model,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		Estimated:        usage.Estimated,
		ErrorSummary:     usage.ErrorSummary,
	})
}

func (s *APIKeyService) Usage(ctx context.Context, filter repository.UsageFilter) ([]dto.APIKeyUsageDTO, error) {
	rows, err := s.store.ListUsage(ctx, filter)
	if err != nil {
		return nil, err
	}
	keysByID, err := s.apiKeysByUsageID(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make([]dto.APIKeyUsageDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, APIKeyUsageDTO(row, keysByID))
	}
	return out, nil
}

func (s *APIKeyService) Logs(ctx context.Context, limit int) ([]dto.RequestLogDTO, error) {
	rows, err := s.store.ListRequestLogs(ctx, limit)
	if err != nil {
		return nil, err
	}
	keysByID, err := s.apiKeysByRequestLogID(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make([]dto.RequestLogDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, RequestLogDTO(row, keysByID))
	}
	return out, nil
}

func (s *APIKeyService) apiKeysByUsageID(ctx context.Context, rows []model.APIKeyUsage) (map[uint]model.APIKey, error) {
	seen := map[uint]struct{}{}
	ids := make([]uint, 0)
	for _, row := range rows {
		if row.APIKeyID == 0 {
			continue
		}
		if _, ok := seen[row.APIKeyID]; ok {
			continue
		}
		seen[row.APIKeyID] = struct{}{}
		ids = append(ids, row.APIKeyID)
	}
	keys, err := s.store.FindAPIKeysByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uint]model.APIKey, len(keys))
	for _, key := range keys {
		out[key.ID] = key
	}
	return out, nil
}

func (s *APIKeyService) apiKeysByRequestLogID(ctx context.Context, rows []model.RequestLog) (map[uint]model.APIKey, error) {
	seen := map[uint]struct{}{}
	ids := make([]uint, 0)
	for _, row := range rows {
		if row.APIKeyID == nil || *row.APIKeyID == 0 {
			continue
		}
		id := *row.APIKeyID
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	keys, err := s.store.FindAPIKeysByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uint]model.APIKey, len(keys))
	for _, key := range keys {
		out[key.ID] = key
	}
	return out, nil
}

func APIKeyDTO(key model.APIKey) dto.APIKeyDTO {
	return dto.APIKeyDTO{
		ID:              key.ID,
		Name:            key.Name,
		APIKey:          key.APIKey,
		KeyPrefix:       key.KeyPrefix,
		KeySuffix:       key.KeySuffix,
		Enabled:         key.Enabled,
		OwnerAdminID:    key.OwnerAdminID,
		ExpiresAt:       key.ExpiresAt,
		LastUsedAt:      key.LastUsedAt,
		TokenQuotaTotal: key.TokenQuotaTotal,
		TokenQuotaDaily: key.TokenQuotaDaily,
		TotalRequests:   key.TotalRequests,
		TotalTokens:     key.TotalTokens,
		CreatedAt:       key.CreatedAt,
		UpdatedAt:       key.UpdatedAt,
	}
}

func APIKeyUsageDTO(row model.APIKeyUsage, keysByID map[uint]model.APIKey) dto.APIKeyUsageDTO {
	var keyName string
	var keyValue string
	var keyMasked string
	if key, ok := keysByID[row.APIKeyID]; ok {
		keyName = key.Name
		keyValue = strings.TrimSpace(key.APIKey)
		keyMasked = maskedAPIKey(key)
	}
	return dto.APIKeyUsageDTO{
		ID:               row.ID,
		APIKeyID:         row.APIKeyID,
		APIKeyName:       keyName,
		APIKey:           keyValue,
		APIKeyMasked:     keyMasked,
		Date:             row.Date,
		Model:            row.Model,
		RequestCount:     row.RequestCount,
		PromptTokens:     row.PromptTokens,
		CompletionTokens: row.CompletionTokens,
		TotalTokens:      row.TotalTokens,
		EstimatedTokens:  row.EstimatedTokens,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func RequestLogDTO(row model.RequestLog, keysByID map[uint]model.APIKey) dto.RequestLogDTO {
	var keyName string
	var keyMasked string
	if row.APIKeyID != nil {
		if key, ok := keysByID[*row.APIKeyID]; ok {
			keyName = key.Name
			keyMasked = maskedAPIKey(key)
		}
	}
	createdAt := row.CreatedAt
	return dto.RequestLogDTO{
		ID:               row.ID,
		APIKeyID:         row.APIKeyID,
		APIKeyName:       keyName,
		APIKeyMasked:     keyMasked,
		Method:           row.Method,
		Route:            row.Route,
		StatusCode:       row.StatusCode,
		LatencyMs:        row.LatencyMs,
		Model:            row.Model,
		PromptTokens:     row.PromptTokens,
		CompletionTokens: row.CompletionTokens,
		TotalTokens:      row.TotalTokens,
		Estimated:        row.Estimated,
		ErrorSummary:     row.ErrorSummary,
		CreatedAt:        &createdAt,
	}
}

func maskedAPIKey(key model.APIKey) string {
	prefix := strings.TrimSpace(key.KeyPrefix)
	suffix := strings.TrimSpace(key.KeySuffix)
	if prefix == "" && strings.TrimSpace(key.APIKey) != "" {
		prefix, suffix = security.APIKeyParts(key.APIKey)
	}
	switch {
	case prefix != "" && suffix != "":
		return prefix + "..." + suffix
	case prefix != "":
		return prefix + "..."
	case suffix != "":
		return "..." + suffix
	default:
		return ""
	}
}
