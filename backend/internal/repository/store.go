package repository

import (
	"context"
	"strings"
	"time"

	"notaion2api/backend/internal/model"

	"gorm.io/gorm"
)

type UsageFilter struct {
	APIKeyID *uint
	FromDate string
	ToDate   string
	Model    string
	Limit    int
}

type AccountFilter struct {
	IncludeDisabled bool
}

func (s *Store) CountAdmins(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.AdminUser{}).Count(&count).Error
	return count, err
}

func (s *Store) CountEnabledAdmins(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.AdminUser{}).Where("enabled = ?", true).Count(&count).Error
	return count, err
}

func (s *Store) CreateAdmin(ctx context.Context, admin *model.AdminUser) error {
	return s.db.WithContext(ctx).Create(admin).Error
}

func (s *Store) ListAdmins(ctx context.Context) ([]model.AdminUser, error) {
	var admins []model.AdminUser
	err := s.db.WithContext(ctx).Order("id asc").Find(&admins).Error
	return admins, err
}

func (s *Store) GetAdmin(ctx context.Context, id uint) (model.AdminUser, error) {
	var admin model.AdminUser
	err := s.db.WithContext(ctx).First(&admin, id).Error
	return admin, err
}

func (s *Store) GetAdminByUsername(ctx context.Context, username string) (model.AdminUser, error) {
	var admin model.AdminUser
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&admin).Error
	return admin, err
}

func (s *Store) SaveAdmin(ctx context.Context, admin *model.AdminUser) error {
	return s.db.WithContext(ctx).Save(admin).Error
}

func (s *Store) DeleteAdmin(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&model.AdminUser{}, id).Error
}

func (s *Store) CreateAdminToken(ctx context.Context, token *model.AdminToken) error {
	return s.db.WithContext(ctx).Create(token).Error
}

func (s *Store) FindAdminToken(ctx context.Context, tokenHash string, now time.Time) (model.AdminToken, error) {
	var token model.AdminToken
	err := s.db.WithContext(ctx).
		Where("token_hash = ? AND expires_at > ? AND revoked_at IS NULL", tokenHash, now).
		First(&token).Error
	return token, err
}

func (s *Store) RevokeAdminToken(ctx context.Context, tokenHash string, now time.Time) error {
	return s.db.WithContext(ctx).Model(&model.AdminToken{}).
		Where("token_hash = ? AND revoked_at IS NULL", tokenHash).
		Update("revoked_at", &now).Error
}

func (s *Store) CreateAPIKey(ctx context.Context, key *model.APIKey) error {
	return s.db.WithContext(ctx).Create(key).Error
}

func (s *Store) CountAPIKeys(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.APIKey{}).Count(&count).Error
	return count, err
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]model.APIKey, error) {
	var keys []model.APIKey
	err := s.db.WithContext(ctx).Order("id desc").Find(&keys).Error
	return keys, err
}

func (s *Store) GetAPIKey(ctx context.Context, id uint) (model.APIKey, error) {
	var key model.APIKey
	err := s.db.WithContext(ctx).First(&key, id).Error
	return key, err
}

func (s *Store) FindAPIKeysByPrefix(ctx context.Context, prefix string) ([]model.APIKey, error) {
	var keys []model.APIKey
	err := s.db.WithContext(ctx).Where("key_prefix = ?", prefix).Find(&keys).Error
	return keys, err
}

func (s *Store) SaveAPIKey(ctx context.Context, key *model.APIKey) error {
	return s.db.WithContext(ctx).Save(key).Error
}

func (s *Store) DeleteAPIKey(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&model.APIKey{}, id).Error
}

func (s *Store) IncrementAPIKeyUsage(ctx context.Context, keyID uint, totalTokens int64, usedAt time.Time) error {
	return s.db.WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", keyID).Updates(map[string]any{
		"last_used_at":   &usedAt,
		"total_requests": gorm.Expr("total_requests + ?", 1),
		"total_tokens":   gorm.Expr("total_tokens + ?", totalTokens),
	}).Error
}

func (s *Store) AddUsage(ctx context.Context, usage model.APIKeyUsage) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.APIKeyUsage
		err := tx.Where("api_key_id = ? AND date = ? AND model = ?", usage.APIKeyID, usage.Date, usage.Model).
			FirstOrCreate(&row, model.APIKeyUsage{
				APIKeyID: usage.APIKeyID,
				Date:     usage.Date,
				Model:    usage.Model,
			}).Error
		if err != nil {
			return err
		}
		return tx.Model(&row).Updates(map[string]any{
			"request_count":     gorm.Expr("request_count + ?", usage.RequestCount),
			"prompt_tokens":     gorm.Expr("prompt_tokens + ?", usage.PromptTokens),
			"completion_tokens": gorm.Expr("completion_tokens + ?", usage.CompletionTokens),
			"total_tokens":      gorm.Expr("total_tokens + ?", usage.TotalTokens),
			"estimated_tokens":  gorm.Expr("estimated_tokens + ?", usage.EstimatedTokens),
		}).Error
	})
}

func (s *Store) SumUsageForDate(ctx context.Context, apiKeyID uint, date string) (int64, error) {
	var total int64
	err := s.db.WithContext(ctx).Model(&model.APIKeyUsage{}).
		Where("api_key_id = ? AND date = ?", apiKeyID, date).
		Select("COALESCE(SUM(total_tokens), 0)").
		Scan(&total).Error
	return total, err
}

func (s *Store) ListUsage(ctx context.Context, filter UsageFilter) ([]model.APIKeyUsage, error) {
	query := s.db.WithContext(ctx).Model(&model.APIKeyUsage{})
	if filter.APIKeyID != nil {
		query = query.Where("api_key_id = ?", *filter.APIKeyID)
	}
	if filter.FromDate != "" {
		query = query.Where("date >= ?", filter.FromDate)
	}
	if filter.ToDate != "" {
		query = query.Where("date <= ?", filter.ToDate)
	}
	if filter.Model != "" {
		query = query.Where("model = ?", filter.Model)
	}
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	var rows []model.APIKeyUsage
	err := query.Order("date desc, api_key_id asc, model asc").Limit(filter.Limit).Find(&rows).Error
	return rows, err
}

func (s *Store) CreateRequestLog(ctx context.Context, row *model.RequestLog) error {
	return s.db.WithContext(ctx).Create(row).Error
}

func (s *Store) ListRequestLogs(ctx context.Context, limit int) ([]model.RequestLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []model.RequestLog
	err := s.db.WithContext(ctx).Order("id desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *Store) UpsertModel(ctx context.Context, row model.Model) error {
	var existing model.Model
	return s.db.WithContext(ctx).
		Where(model.Model{PublicID: row.PublicID}).
		Assign(row).
		FirstOrCreate(&existing).Error
}

func (s *Store) ListModels(ctx context.Context) ([]model.Model, error) {
	var rows []model.Model
	err := s.db.WithContext(ctx).Order("public_id asc").Find(&rows).Error
	return rows, err
}

func (s *Store) ListEnabledModels(ctx context.Context) ([]model.Model, error) {
	var rows []model.Model
	err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("public_id asc").Find(&rows).Error
	return rows, err
}

func (s *Store) GetModelByPublicID(ctx context.Context, publicID string) (model.Model, error) {
	var row model.Model
	err := s.db.WithContext(ctx).Where("public_id = ? AND enabled = ?", publicID, true).First(&row).Error
	return row, err
}

func (s *Store) ListAccounts(ctx context.Context, filter AccountFilter) ([]model.Account, error) {
	query := s.db.WithContext(ctx).Model(&model.Account{})
	if !filter.IncludeDisabled {
		query = query.Where("enabled = ?", true)
	}
	var rows []model.Account
	err := query.Order("active desc, priority desc, id asc").Find(&rows).Error
	return rows, err
}

func (s *Store) CreateAccount(ctx context.Context, account *model.Account) error {
	return s.db.WithContext(ctx).Create(account).Error
}

func (s *Store) SaveAccount(ctx context.Context, account *model.Account) error {
	return s.db.WithContext(ctx).Save(account).Error
}

func (s *Store) GetAccount(ctx context.Context, id uint) (model.Account, error) {
	var account model.Account
	err := s.db.WithContext(ctx).First(&account, id).Error
	return account, err
}

func (s *Store) GetAccountByEmail(ctx context.Context, email string) (model.Account, error) {
	var account model.Account
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&account).Error
	return account, err
}

func (s *Store) FindActiveAccount(ctx context.Context) (model.Account, error) {
	var account model.Account
	err := s.db.WithContext(ctx).
		Where("active = ? AND enabled = ?", true, true).
		Order("priority desc, id asc").
		First(&account).Error
	return account, err
}

func (s *Store) FindBestAccount(ctx context.Context) (model.Account, error) {
	var account model.Account
	err := s.db.WithContext(ctx).
		Where("enabled = ?", true).
		Order("active desc, priority desc, last_used_at asc, id asc").
		First(&account).Error
	return account, err
}

func (s *Store) ActivateAccount(ctx context.Context, id uint) (model.Account, error) {
	var account model.Account
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&account, id).Error; err != nil {
			return err
		}
		if !account.Enabled {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Model(&model.Account{}).Where("active = ?", true).Update("active", false).Error; err != nil {
			return err
		}
		account.Active = true
		if strings.TrimSpace(account.Status) == "" || account.Status == "inactive" {
			account.Status = "ready"
		}
		return tx.Save(&account).Error
	})
	return account, err
}

func (s *Store) DeleteAccount(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&model.Account{}, id).Error
}

func (s *Store) MarkAccountSuccess(ctx context.Context, id uint, now time.Time) error {
	return s.db.WithContext(ctx).Model(&model.Account{}).Where("id = ?", id).Updates(map[string]any{
		"status":               "ready",
		"last_error":           "",
		"last_used_at":         &now,
		"last_success_at":      &now,
		"consecutive_failures": 0,
		"total_successes":      gorm.Expr("total_successes + ?", 1),
	}).Error
}

func (s *Store) MarkAccountFailure(ctx context.Context, id uint, message string, now time.Time) error {
	return s.db.WithContext(ctx).Model(&model.Account{}).Where("id = ?", id).Updates(map[string]any{
		"status":               "error",
		"last_error":           message,
		"last_used_at":         &now,
		"consecutive_failures": gorm.Expr("consecutive_failures + ?", 1),
		"total_failures":       gorm.Expr("total_failures + ?", 1),
	}).Error
}
