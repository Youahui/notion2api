package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"notion2api/backend/internal/dto"
	"notion2api/backend/internal/model"
	"notion2api/backend/internal/repository"

	"gorm.io/gorm"
)

type ModelService struct {
	store *repository.Store
}

func NewModelService(store *repository.Store) *ModelService {
	return &ModelService{store: store}
}

func (s *ModelService) SeedDefaults(ctx context.Context) error {
	defaults := []model.Model{
		{PublicID: "auto", Name: "Auto", Family: "system", Group: "default", Enabled: true, Aliases: "default,workflow,notion-ai-workflow"},
		{PublicID: "gpt-5.2", Name: "GPT-5.2", NotionModel: "oatmeal-cookie", Family: "openai", Group: "fast", Beta: true, Enabled: true, Aliases: "gpt52,oatmeal-cookie"},
		{PublicID: "gpt-5.4", Name: "GPT-5.4", NotionModel: "oval-kumquat-medium", Family: "openai", Group: "fast", Beta: true, Enabled: true, Aliases: "gpt54,oval-kumquat-medium"},
		{PublicID: "gpt-5.5", Name: "GPT-5.5", NotionModel: "opal-quince-medium", Family: "openai", Group: "intelligent", Beta: true, Enabled: true, Aliases: "gpt55,opal-quince-medium"},
		{PublicID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", NotionModel: "vertex-gemini-2.5-flash", Family: "gemini", Group: "fast", Enabled: true},
		{PublicID: "gemini-3.5-flash", Name: "Gemini 3.5 Flash", NotionModel: "vertex-gemini-3.5-flash", Family: "gemini", Group: "fast", Beta: true, Enabled: true, Aliases: "vertex-gemini-3.5-flash"},
		{PublicID: "sonnet-4.6", Name: "Sonnet 4.6", NotionModel: "almond-croissant-low", Family: "anthropic", Group: "fast", Beta: true, Enabled: true, Aliases: "claude-sonnet-4.6,almond-croissant-low"},
		{PublicID: "opus-4.6", Name: "Opus 4.6", NotionModel: "avocado-froyo-medium", Family: "anthropic", Group: "intelligent", Beta: true, Enabled: true, Aliases: "claude-opus-4.6,opus46,avocado-froyo-medium"},
		{PublicID: "opus-4.7", Name: "Opus 4.7", NotionModel: "apricot-sorbet-high", Family: "anthropic", Group: "intelligent", Beta: true, Enabled: true, Aliases: "claude-opus-4.7,opus47,apricot-sorbet-high"},
		{PublicID: "opus-4.8", Name: "Opus 4.8", NotionModel: "ambrosia-tart-high", Family: "anthropic", Group: "intelligent", Beta: true, Enabled: true, Aliases: "claude-opus-4.8,opus48,ambrosia-tart-high"},
		{PublicID: "gpt-5.4-mini", Name: "GPT-5.4 Mini", NotionModel: "oregon-grape-medium", Family: "openai", Group: "fast", Beta: true, Enabled: true, Aliases: "gpt54-mini,oregon-grape-medium"},
		{PublicID: "gpt-5.4-nano", Name: "GPT-5.4 Nano", NotionModel: "otaheite-apple-medium", Family: "openai", Group: "fast", Beta: true, Enabled: true, Aliases: "gpt54-nano,otaheite-apple-medium"},
		{PublicID: "minimax-m2.5", Name: "MiniMax M2.5", NotionModel: "fireworks-minimax-m2.5", Family: "mystery", Group: "intelligent", Beta: true, Enabled: true, Aliases: "minimax,fireworks-minimax-m2.5"},
		{PublicID: "kimi-k2.6", Name: "Kimi K2.6", NotionModel: "fireworks-kimi-k2.6", Family: "mystery", Group: "intelligent", Beta: true, Enabled: true, Aliases: "kimi,fireworks-kimi-k2.6"},
		{PublicID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", NotionModel: "baseten-deepseek-v4-pro", Family: "mystery", Group: "intelligent", Beta: true, Enabled: true, Aliases: "deepseek,baseten-deepseek-v4-pro"},
		{PublicID: "grok-4.3", Name: "Grok 4.3", NotionModel: "xigua-mochi-medium", Family: "xai", Group: "intelligent", Beta: true, Enabled: true, Aliases: "xai-grok-4.3,xigua-mochi-medium"},
		{PublicID: "grok-build-0.1", Name: "Grok Build 0.1", NotionModel: "xinomavro-cake", Family: "xai", Group: "intelligent", Beta: true, Enabled: true, Aliases: "xai-grok-build-0.1,xinomavro-cake"},
		{PublicID: "gemini-3.1-pro", Name: "Gemini 3.1 Pro", NotionModel: "galette-medium-thinking", Family: "gemini", Group: "intelligent", Beta: true, Enabled: true, Aliases: "galette-medium-thinking"},
		{PublicID: "haiku-4.5", Name: "Haiku 4.5", NotionModel: "anthropic-haiku-4.5", Family: "anthropic", Group: "fast", Beta: true, Enabled: true, Aliases: "claude-haiku-4.5,anthropic-haiku-4.5"},
		{PublicID: "gemini-3-flash", Name: "Gemini 3 Flash", NotionModel: "gingerbread", Family: "gemini", Group: "fast", Beta: true, Enabled: true, Aliases: "gingerbread"},
		{PublicID: "fable-5", Name: "Fable 5", NotionModel: "acai-budino-high", Family: "anthropic", Group: "intelligent", Beta: true, Enabled: false, Aliases: "acai-budino,acai-budino-high"},
	}
	for _, row := range defaults {
		if err := s.store.UpsertModel(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

func (s *ModelService) ListOpenAI(ctx context.Context) ([]dto.ModelDTO, error) {
	rows, err := s.store.ListEnabledModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.ModelDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, ModelDTO(row))
	}
	return out, nil
}

func (s *ModelService) ListAdmin(ctx context.Context) ([]model.Model, error) {
	return s.store.ListModels(ctx)
}

func (s *ModelService) Resolve(ctx context.Context, publicID string) (model.Model, error) {
	publicID = strings.TrimSpace(publicID)
	if publicID == "" {
		publicID = "auto"
	}
	row, err := s.store.GetModelByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Model{}, ErrNotFound
		}
		return model.Model{}, err
	}
	return row, nil
}

func ModelDTO(row model.Model) dto.ModelDTO {
	return dto.ModelDTO{
		ID:          row.PublicID,
		Object:      "model",
		Created:     row.CreatedAt.Unix(),
		OwnedBy:     "notion2api",
		Name:        row.Name,
		Family:      row.Family,
		Group:       row.Group,
		Beta:        row.Beta,
		NotionModel: row.NotionModel,
	}
}

func modelCreatedAt(row model.Model) time.Time {
	if row.CreatedAt.IsZero() {
		return time.Unix(0, 0)
	}
	return row.CreatedAt
}
