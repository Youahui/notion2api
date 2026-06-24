package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"notion2api/backend/internal/config"
	"notion2api/backend/internal/dto"
	"notion2api/backend/internal/model"
	"notion2api/backend/internal/repository"

	"gorm.io/gorm"
)

type AccountService struct {
	store      *repository.Store
	cfg        config.Config
	probeCache *dispatchProbeCache
}

type AccountSelectionOptions struct {
	GroupID          *uint
	PreferredEmail   string
	Model            string
	FailedAccountIDs map[uint]struct{}
}

func NewAccountService(store *repository.Store, cfg config.Config) *AccountService {
	return &AccountService{store: store, cfg: cfg, probeCache: newDispatchProbeCache()}
}

func (s *AccountService) List(ctx context.Context, includeDisabled bool) ([]dto.AccountDTO, error) {
	rows, err := s.store.ListAccounts(ctx, repository.AccountFilter{IncludeDisabled: includeDisabled})
	if err != nil {
		return nil, err
	}
	return s.accountDTOsWithGroups(ctx, rows)
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
	if err := s.saveAccountGroupBindings(ctx, account, req.GroupIDs, req.GroupIDs != nil); err != nil {
		return dto.AccountDTO{}, err
	}
	if account.Active {
		activated, err := s.store.ActivateAccount(ctx, account.ID)
		if err != nil {
			return dto.AccountDTO{}, err
		}
		account = activated
	}
	return s.accountDTOWithGroups(ctx, account)
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
	if req.GroupIDs != nil {
		if err := s.saveAccountGroupBindings(ctx, next, req.GroupIDs, true); err != nil {
			return dto.AccountDTO{}, err
		}
	}
	if req.Active != nil && *req.Active {
		next, err = s.store.ActivateAccount(ctx, next.ID)
		if err != nil {
			return dto.AccountDTO{}, err
		}
	}
	return s.accountDTOWithGroups(ctx, next)
}

func (s *AccountService) Delete(ctx context.Context, id uint) error {
	account, err := s.store.GetAccount(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.store.DeleteAccount(ctx, id); err != nil {
		return err
	}
	if err := s.store.DeleteAccountGroupBindings(ctx, id); err != nil {
		return err
	}
	_ = s.store.DeleteLoginSession(ctx, account.Email)
	s.removeAccountSessionFiles(account)
	return nil
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
	return s.SelectWithScope(ctx, AccountSelectionOptions{PreferredEmail: preferredEmail})
}

func (s *AccountService) SelectWithScope(ctx context.Context, opts AccountSelectionOptions) (model.Account, error) {
	preferredEmail := strings.TrimSpace(opts.PreferredEmail)
	if preferredEmail != "" {
		var account model.Account
		var err error
		if opts.GroupID != nil && *opts.GroupID != 0 {
			account, err = s.store.GetAccountByEmailInGroup(ctx, *opts.GroupID, preferredEmail)
		} else {
			account, err = s.store.GetAccountByEmail(ctx, preferredEmail)
		}
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return model.Account{}, ErrNotFound
			}
			return model.Account{}, err
		}
		if !s.accountSchedulable(account, opts) {
			return model.Account{}, ErrForbidden
		}
		return account, nil
	}
	if opts.GroupID != nil && *opts.GroupID != 0 {
		accounts, err := s.store.ListSchedulableAccountsByGroup(ctx, *opts.GroupID)
		if err != nil {
			return model.Account{}, err
		}
		for _, account := range accounts {
			if s.accountSchedulable(account, opts) {
				return account, nil
			}
		}
		return model.Account{}, ErrNotFound
	}
	account, err := s.store.FindActiveAccount(ctx)
	if err == nil && s.accountSchedulable(account, opts) {
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
	if !s.accountSchedulable(account, opts) {
		return model.Account{}, ErrNotFound
	}
	return account, nil
}

func (s *AccountService) AccountSchedulableInScope(ctx context.Context, account model.Account, opts AccountSelectionOptions) (bool, error) {
	if opts.GroupID != nil && *opts.GroupID != 0 {
		member, err := s.store.FindAccountGroupMember(ctx, *opts.GroupID, account.ID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
		if !member.Enabled {
			return false, nil
		}
	}
	return s.accountSchedulable(account, opts), nil
}

func (s *AccountService) accountSchedulable(account model.Account, opts AccountSelectionOptions) bool {
	if account.ID == 0 || !account.Enabled {
		return false
	}
	if opts.FailedAccountIDs != nil {
		if _, failed := opts.FailedAccountIDs[account.ID]; failed {
			return false
		}
	}
	status := strings.ToLower(strings.TrimSpace(account.Status))
	if status != "" && status != "ready" && status != "active" {
		return false
	}
	if account.CooldownUntil != nil && account.CooldownUntil.After(time.Now()) {
		return false
	}
	if account.HourlyQuota > 0 && account.WindowStartedAt != nil && time.Since(*account.WindowStartedAt) < time.Hour && account.WindowRequestCount >= account.HourlyQuota {
		return false
	}
	return true
}

func (s *AccountService) MarkSuccess(ctx context.Context, id uint) {
	_ = s.store.MarkAccountSuccess(ctx, id, time.Now())
}

func (s *AccountService) MarkFailure(ctx context.Context, id uint, message string) {
	_ = s.store.MarkAccountFailure(ctx, id, trimError(message), time.Now())
	if account, err := s.store.GetAccount(ctx, id); err == nil {
		s.probeCache.markFailure(account.Email)
	}
}

func (s *AccountService) EnsureDefaultGroup(ctx context.Context) (dto.GroupDTO, error) {
	group, err := s.store.EnsureDefaultAccountGroup(ctx)
	if err != nil {
		return dto.GroupDTO{}, err
	}
	members, err := s.store.ListAccountGroupMembers(ctx, group.ID)
	if err != nil {
		return dto.GroupDTO{}, err
	}
	return GroupDTO(group, int64(len(members))), nil
}

func (s *AccountService) ListGroups(ctx context.Context) ([]dto.GroupDTO, error) {
	groups, err := s.store.ListAccountGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.GroupDTO, 0, len(groups))
	for _, group := range groups {
		members, err := s.store.ListAccountGroupMembers(ctx, group.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, GroupDTO(group, int64(len(members))))
	}
	return out, nil
}

func (s *AccountService) GetGroup(ctx context.Context, id uint) (dto.GroupDTO, error) {
	group, err := s.store.GetAccountGroup(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.GroupDTO{}, ErrNotFound
		}
		return dto.GroupDTO{}, err
	}
	members, err := s.store.ListAccountGroupMembers(ctx, group.ID)
	if err != nil {
		return dto.GroupDTO{}, err
	}
	return GroupDTO(group, int64(len(members))), nil
}

func (s *AccountService) CreateGroup(ctx context.Context, req dto.CreateGroupRequest) (dto.GroupDTO, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return dto.GroupDTO{}, ErrInvalid
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.IsDefault != nil && *req.IsDefault {
		return dto.GroupDTO{}, ErrInvalid
	}
	group := model.AccountGroup{
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		IsDefault:   false,
	}
	if err := s.store.CreateAccountGroup(ctx, &group); err != nil {
		return dto.GroupDTO{}, err
	}
	return GroupDTO(group, 0), nil
}

func (s *AccountService) UpdateGroup(ctx context.Context, id uint, req dto.UpdateGroupRequest) (dto.GroupDTO, error) {
	group, err := s.store.GetAccountGroup(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.GroupDTO{}, ErrNotFound
		}
		return dto.GroupDTO{}, err
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return dto.GroupDTO{}, ErrInvalid
		}
		group.Name = name
	}
	if req.Description != nil {
		group.Description = strings.TrimSpace(*req.Description)
	}
	if req.Enabled != nil {
		if group.IsDefault && !*req.Enabled {
			return dto.GroupDTO{}, ErrInvalid
		}
		group.Enabled = *req.Enabled
	}
	if req.IsDefault != nil && *req.IsDefault != group.IsDefault {
		return dto.GroupDTO{}, ErrInvalid
	}
	if err := s.store.SaveAccountGroup(ctx, &group); err != nil {
		return dto.GroupDTO{}, err
	}
	members, err := s.store.ListAccountGroupMembers(ctx, group.ID)
	if err != nil {
		return dto.GroupDTO{}, err
	}
	return GroupDTO(group, int64(len(members))), nil
}

func (s *AccountService) DeleteGroup(ctx context.Context, id uint) error {
	group, err := s.store.GetAccountGroup(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if group.IsDefault {
		return ErrInvalid
	}
	defaultGroup, err := s.store.GetDefaultAccountGroup(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		defaultGroup, err = s.store.EnsureDefaultAccountGroup(ctx)
	}
	if err != nil {
		return err
	}
	if err := s.store.ReassignAPIKeysToGroup(ctx, group.ID, defaultGroup.ID); err != nil {
		return err
	}
	return s.store.DeleteAccountGroup(ctx, id)
}

func (s *AccountService) GroupMembers(ctx context.Context, id uint) ([]dto.GroupMemberDTO, error) {
	if _, err := s.store.GetAccountGroup(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	members, err := s.store.ListAccountGroupMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]dto.GroupMemberDTO, 0, len(members))
	for _, member := range members {
		account, err := s.store.GetAccount(ctx, member.AccountID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, GroupMemberDTO(member, account))
	}
	return out, nil
}

func (s *AccountService) ReplaceGroupMembers(ctx context.Context, id uint, req dto.UpdateGroupMembersRequest) ([]dto.GroupMemberDTO, error) {
	if _, err := s.store.GetAccountGroup(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	seen := map[uint]struct{}{}
	members := make([]model.AccountGroupMember, 0, len(req.Members))
	for _, item := range req.Members {
		if item.AccountID == 0 {
			return nil, ErrInvalid
		}
		if _, ok := seen[item.AccountID]; ok {
			continue
		}
		seen[item.AccountID] = struct{}{}
		account, err := s.store.GetAccount(ctx, item.AccountID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		members = append(members, model.AccountGroupMember{
			GroupID:   id,
			AccountID: account.ID,
			Enabled:   item.Enabled,
			Priority:  item.Priority,
		})
	}
	if err := s.store.ReplaceAccountGroupMembers(ctx, id, members); err != nil {
		return nil, err
	}
	return s.GroupMembers(ctx, id)
}

func (s *AccountService) ProbeProtocolHealth(ctx context.Context, gateway *NotionGateway, account model.Account) error {
	if gateway == nil {
		return nil
	}
	now := time.Now()
	ttl := time.Duration(s.cfg.Dispatch.ProbeCacheTTLSeconds) * time.Second
	if !s.probeCache.shouldProbe(account.Email, ttl, now) {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, dispatchProbeTimeout(s.cfg))
	defer cancel()
	if err := gateway.ProbeProtocolHealth(probeCtx, account); err != nil {
		if probeCtx.Err() != nil {
			s.probeCache.markSuccess(account.Email, now)
			return nil
		}
		s.probeCache.markFailure(account.Email)
		return err
	}
	s.probeCache.markSuccess(account.Email, now)
	return nil
}

func (s *AccountService) saveAccountGroupBindings(ctx context.Context, account model.Account, groupIDs []uint, explicit bool) error {
	if account.ID == 0 {
		return nil
	}
	if !explicit {
		return s.store.EnsureAccountInDefaultGroup(ctx, account)
	}
	ids := uniquePositiveIDs(groupIDs)
	if len(ids) == 0 {
		group, err := s.store.GetDefaultAccountGroup(ctx)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			group, err = s.store.EnsureDefaultAccountGroup(ctx)
		}
		if err != nil {
			return err
		}
		ids = []uint{group.ID}
	}
	members := make([]model.AccountGroupMember, 0, len(ids))
	for _, groupID := range ids {
		group, err := s.store.GetAccountGroup(ctx, groupID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if !group.Enabled {
			return ErrForbidden
		}
		members = append(members, model.AccountGroupMember{
			GroupID:   group.ID,
			AccountID: account.ID,
			Enabled:   true,
			Priority:  account.Priority,
		})
	}
	return s.replaceAccountMemberships(ctx, account.ID, members)
}

func (s *AccountService) replaceAccountMemberships(ctx context.Context, accountID uint, members []model.AccountGroupMember) error {
	return s.store.ReplaceAccountGroupBindings(ctx, accountID, members)
}

func uniquePositiveIDs(ids []uint) []uint {
	seen := map[uint]struct{}{}
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *AccountService) accountDTOWithGroups(ctx context.Context, row model.Account) (dto.AccountDTO, error) {
	rows, err := s.accountDTOsWithGroups(ctx, []model.Account{row})
	if err != nil {
		return dto.AccountDTO{}, err
	}
	if len(rows) == 0 {
		return AccountDTO(row), nil
	}
	return rows[0], nil
}

func (s *AccountService) accountDTOsWithGroups(ctx context.Context, rows []model.Account) ([]dto.AccountDTO, error) {
	accountIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		accountIDs = append(accountIDs, row.ID)
	}
	members, err := s.store.ListAccountGroupMembersByAccountIDs(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	groupIDs := make([]uint, 0, len(members))
	seen := map[uint]struct{}{}
	for _, member := range members {
		if _, ok := seen[member.GroupID]; ok {
			continue
		}
		seen[member.GroupID] = struct{}{}
		groupIDs = append(groupIDs, member.GroupID)
	}
	groups, err := s.store.FindAccountGroupsByIDs(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	groupsByID := make(map[uint]model.AccountGroup, len(groups))
	for _, group := range groups {
		groupsByID[group.ID] = group
	}
	accountGroups := map[uint][]dto.GroupDTO{}
	for _, member := range members {
		group, ok := groupsByID[member.GroupID]
		if !ok {
			continue
		}
		accountGroups[member.AccountID] = append(accountGroups[member.AccountID], GroupDTO(group, 0))
	}
	out := make([]dto.AccountDTO, 0, len(rows))
	for _, row := range rows {
		item := AccountDTO(row)
		item.Groups = accountGroups[row.ID]
		out = append(out, item)
	}
	return out, nil
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
	if clean := strings.TrimSpace(req.PlanType); clean != "" {
		existing.PlanType = clean
	}
	if clean := strings.TrimSpace(req.Status); clean != "" {
		existing.Status = clean
	}
	if req.Priority != 0 {
		existing.Priority = req.Priority
	}
	if req.HourlyQuota >= 0 {
		existing.HourlyQuota = req.HourlyQuota
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
	if clean := strings.TrimSpace(req.ProfileDir); clean != "" {
		existing.ProfileDir = clean
	}
	if clean := strings.TrimSpace(req.StorageStatePath); clean != "" {
		existing.StorageStatePath = clean
	}
	if clean := strings.TrimSpace(req.PendingStatePath); clean != "" {
		existing.PendingStatePath = clean
	}
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
		PlanType:            account.PlanType,
		Status:              account.Status,
		Priority:            account.Priority,
		HourlyQuota:         account.HourlyQuota,
		WindowStartedAt:     account.WindowStartedAt,
		WindowRequestCount:  account.WindowRequestCount,
		CooldownUntil:       account.CooldownUntil,
		MaxConcurrency:      account.MaxConcurrency,
		ProxyMode:           account.ProxyMode,
		ProxyURL:            account.ProxyURL,
		ProxyHTTPURL:        account.ProxyHTTPURL,
		ProxyHTTPSURL:       account.ProxyHTTPSURL,
		ProbeJSON:           account.ProbeJSON,
		ProfileDir:          account.ProfileDir,
		StorageStatePath:    account.StorageStatePath,
		PendingStatePath:    account.PendingStatePath,
		LoginMessage:        account.LoginMessage,
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

func GroupDTO(group model.AccountGroup, accountCount int64) dto.GroupDTO {
	return dto.GroupDTO{
		ID:           group.ID,
		Name:         group.Name,
		Description:  group.Description,
		Enabled:      group.Enabled,
		IsDefault:    group.IsDefault,
		AccountCount: accountCount,
		CreatedAt:    group.CreatedAt,
		UpdatedAt:    group.UpdatedAt,
	}
}

func GroupMemberDTO(member model.AccountGroupMember, account model.Account) dto.GroupMemberDTO {
	return dto.GroupMemberDTO{
		AccountID: account.ID,
		Email:     account.Email,
		Enabled:   member.Enabled,
		Priority:  member.Priority,
		Status:    account.Status,
		LastUsed:  account.LastUsedAt,
	}
}

func trimError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		return value[:512]
	}
	return value
}
