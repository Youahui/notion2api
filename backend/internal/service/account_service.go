package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/model"
	"notaion2api/backend/internal/repository"

	"gorm.io/gorm"
)

type AccountService struct {
	store *repository.Store
}

func NewAccountService(store *repository.Store) *AccountService {
	return &AccountService{store: store}
}

func (s *AccountService) List(ctx context.Context, includeDisabled bool) ([]dto.AccountDTO, error) {
	rows, err := s.store.ListAccounts(ctx, repository.AccountFilter{IncludeDisabled: includeDisabled})
	if err != nil {
		return nil, err
	}
	out := make([]dto.AccountDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, AccountDTO(row))
	}
	return out, nil
}

func (s *AccountService) Get(ctx context.Context, id uint) (model.Account, error) {
	account, err := s.store.GetAccount(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Account{}, ErrNotFound
		}
		return model.Account{}, err
	}
	return account, nil
}

func (s *AccountService) Create(ctx context.Context, req dto.UpsertAccountRequest) (dto.AccountDTO, error) {
	account, err := accountFromRequest(req, model.Account{})
	if err != nil {
		return dto.AccountDTO{}, err
	}
	if account.Status == "" {
		account.Status = "ready"
	}
	if err := s.store.CreateAccount(ctx, &account); err != nil {
		return dto.AccountDTO{}, err
	}
	if account.Active {
		activated, err := s.store.ActivateAccount(ctx, account.ID)
		if err != nil {
			return dto.AccountDTO{}, err
		}
		account = activated
	}
	return AccountDTO(account), nil
}

func (s *AccountService) Update(ctx context.Context, id uint, req dto.UpsertAccountRequest) (dto.AccountDTO, error) {
	existing, err := s.store.GetAccount(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.AccountDTO{}, ErrNotFound
		}
		return dto.AccountDTO{}, err
	}
	next, err := accountFromRequest(req, existing)
	if err != nil {
		return dto.AccountDTO{}, err
	}
	if err := s.store.SaveAccount(ctx, &next); err != nil {
		return dto.AccountDTO{}, err
	}
	if req.Active != nil && *req.Active {
		next, err = s.store.ActivateAccount(ctx, next.ID)
		if err != nil {
			return dto.AccountDTO{}, err
		}
	}
	return AccountDTO(next), nil
}

func (s *AccountService) Delete(ctx context.Context, id uint) error {
	if _, err := s.store.GetAccount(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	return s.store.DeleteAccount(ctx, id)
}

func (s *AccountService) Activate(ctx context.Context, id uint) (dto.AccountDTO, error) {
	account, err := s.store.ActivateAccount(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.AccountDTO{}, ErrNotFound
		}
		return dto.AccountDTO{}, err
	}
	return AccountDTO(account), nil
}

func (s *AccountService) Select(ctx context.Context, preferredEmail string) (model.Account, error) {
	preferredEmail = strings.TrimSpace(preferredEmail)
	if preferredEmail != "" {
		account, err := s.store.GetAccountByEmail(ctx, preferredEmail)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return model.Account{}, ErrNotFound
			}
			return model.Account{}, err
		}
		if !account.Enabled {
			return model.Account{}, ErrForbidden
		}
		return account, nil
	}
	account, err := s.store.FindActiveAccount(ctx)
	if err == nil {
		return account, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Account{}, err
	}
	account, err = s.store.FindBestAccount(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Account{}, ErrNotFound
		}
		return model.Account{}, err
	}
	return account, nil
}

func (s *AccountService) MarkSuccess(ctx context.Context, id uint) {
	_ = s.store.MarkAccountSuccess(ctx, id, time.Now())
}

func (s *AccountService) MarkFailure(ctx context.Context, id uint, message string) {
	_ = s.store.MarkAccountFailure(ctx, id, trimError(message), time.Now())
}

func accountFromRequest(req dto.UpsertAccountRequest, existing model.Account) (model.Account, error) {
	email := strings.TrimSpace(req.Email)
	if email == "" {
		return model.Account{}, ErrInvalid
	}
	existing.Email = email
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	} else if existing.ID == 0 {
		existing.Enabled = true
	}
	if req.Active != nil {
		existing.Active = *req.Active
	}
	if clean := strings.TrimSpace(req.UserID); clean != "" {
		existing.UserID = clean
	}
	if clean := strings.TrimSpace(req.UserName); clean != "" {
		existing.UserName = clean
	}
	if clean := strings.TrimSpace(req.SpaceID); clean != "" {
		existing.SpaceID = clean
	}
	if clean := strings.TrimSpace(req.SpaceViewID); clean != "" {
		existing.SpaceViewID = clean
	}
	if clean := strings.TrimSpace(req.SpaceName); clean != "" {
		existing.SpaceName = clean
	}
	if clean := strings.TrimSpace(req.ClientVersion); clean != "" {
		existing.ClientVersion = clean
	}
	if clean := strings.TrimSpace(req.Status); clean != "" {
		existing.Status = clean
	}
	if req.Priority != 0 {
		existing.Priority = req.Priority
	}
	if req.MaxConcurrency > 0 {
		existing.MaxConcurrency = req.MaxConcurrency
	} else if existing.MaxConcurrency <= 0 {
		existing.MaxConcurrency = 1
	}
	existing.ProxyMode = strings.TrimSpace(req.ProxyMode)
	existing.ProxyURL = strings.TrimSpace(req.ProxyURL)
	existing.ProxyHTTPURL = strings.TrimSpace(req.ProxyHTTPURL)
	existing.ProxyHTTPSURL = strings.TrimSpace(req.ProxyHTTPSURL)
	existing.ProbeJSON = strings.TrimSpace(req.ProbeJSON)
	if req.ClearAuthToken {
		existing.AuthToken = ""
	} else if clean := strings.TrimSpace(req.AuthToken); clean != "" {
		existing.AuthToken = clean
	}
	if req.ClearCookie {
		existing.Cookie = ""
	} else if clean := strings.TrimSpace(req.Cookie); clean != "" {
		existing.Cookie = clean
	}
	if existing.Status == "" {
		existing.Status = "inactive"
	}
	return existing, nil
}

func AccountDTO(account model.Account) dto.AccountDTO {
	return dto.AccountDTO{
		ID:                  account.ID,
		Email:               account.Email,
		Enabled:             account.Enabled,
		Active:              account.Active,
		UserID:              account.UserID,
		UserName:            account.UserName,
		SpaceID:             account.SpaceID,
		SpaceViewID:         account.SpaceViewID,
		SpaceName:           account.SpaceName,
		ClientVersion:       account.ClientVersion,
		Status:              account.Status,
		Priority:            account.Priority,
		MaxConcurrency:      account.MaxConcurrency,
		ProxyMode:           account.ProxyMode,
		ProxyURL:            account.ProxyURL,
		ProxyHTTPURL:        account.ProxyHTTPURL,
		ProxyHTTPSURL:       account.ProxyHTTPSURL,
		ProbeJSON:           account.ProbeJSON,
		HasAuthToken:        strings.TrimSpace(account.AuthToken) != "",
		HasCookie:           strings.TrimSpace(account.Cookie) != "",
		LastError:           account.LastError,
		LastLoginAt:         account.LastLoginAt,
		LastUsedAt:          account.LastUsedAt,
		LastSuccessAt:       account.LastSuccessAt,
		LastRefreshAt:       account.LastRefreshAt,
		ConsecutiveFailures: account.ConsecutiveFailures,
		TotalSuccesses:      account.TotalSuccesses,
		TotalFailures:       account.TotalFailures,
		CreatedAt:           account.CreatedAt,
		UpdatedAt:           account.UpdatedAt,
	}
}

func trimError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		return value[:512]
	}
	return value
}
