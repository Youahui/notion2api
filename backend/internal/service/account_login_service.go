package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"notion2api/backend/internal/dto"
	"notion2api/backend/internal/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const notionLoginUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126 Safari/537.36"

var notionVersionPattern = regexp.MustCompile(`data-notion-version=["']([^"']+)["']`)

type accountProbeCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain,omitempty"`
	Path     string  `json:"path,omitempty"`
	Expires  float64 `json:"expires,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	SameSite string  `json:"sameSite,omitempty"`
}

type accountProbePayload struct {
	Email         string               `json:"email"`
	UserID        string               `json:"user_id"`
	UserName      string               `json:"user_name"`
	SpaceID       string               `json:"space_id"`
	SpaceViewID   string               `json:"space_view_id"`
	SpaceName     string               `json:"space_name"`
	ClientVersion string               `json:"client_version"`
	Cookies       []accountProbeCookie `json:"cookies"`
}

type accountSpacePointer struct {
	SpaceID     string
	SpaceViewID string
}

type accountSessionPaths struct {
	Dir              string
	ProbePath        string
	StorageStatePath string
	PendingStatePath string
}

type storageState struct {
	Cookies []accountProbeCookie `json:"cookies"`
	Origins []any                `json:"origins"`
}

func (s *AccountService) ManualImport(ctx context.Context, req dto.ManualImportAccountRequest) (dto.AccountLoginResponse, error) {
	probe, err := parseAccountProbeJSON(req.ProbeJSONText)
	if err != nil {
		return dto.AccountLoginResponse{}, fmt.Errorf("%w: invalid probe json", ErrInvalid)
	}
	probe.Email = firstNonEmpty(req.Email, probe.Email)
	probe.UserID = firstNonEmpty(req.UserID, probe.UserID)
	probe.UserName = firstNonEmpty(req.UserName, probe.UserName)
	probe.SpaceID = firstNonEmpty(req.SpaceID, probe.SpaceID)
	probe.SpaceViewID = firstNonEmpty(req.SpaceViewID, probe.SpaceViewID)
	probe.SpaceName = firstNonEmpty(req.SpaceName, probe.SpaceName)
	probe.ClientVersion = firstNonEmpty(req.ClientVersion, probe.ClientVersion)
	cookies := mergeAccountCookies(probe.Cookies, parseAccountCookieHeader(req.CookieHeader))
	if token := strings.TrimSpace(req.TokenV2); token != "" {
		cookies = mergeAccountCookies(cookies, []accountProbeCookie{{Name: "token_v2", Value: token}})
	}
	cookies = normalizeAccountCookies(cookies)
	if len(cookies) == 0 {
		return dto.AccountLoginResponse{}, fmt.Errorf("%w: cookies are required; paste cookie_header, token_v2, or probe_json_text", ErrInvalid)
	}
	discoveryNeeded := probe.Email == "" || probe.UserID == "" || probe.SpaceID == "" || probe.ClientVersion == "" || probe.UserName == "" || probe.SpaceName == ""
	var discoverErr error
	if discoveryNeeded {
		var discovered accountProbePayload
		discovered, discoverErr = s.discoverImportedAccountMetadata(ctx, probe, cookies)
		probe.Email = firstNonEmpty(probe.Email, discovered.Email)
		probe.UserID = firstNonEmpty(probe.UserID, discovered.UserID)
		probe.UserName = firstNonEmpty(probe.UserName, discovered.UserName)
		probe.SpaceID = firstNonEmpty(probe.SpaceID, discovered.SpaceID)
		probe.SpaceViewID = firstNonEmpty(probe.SpaceViewID, discovered.SpaceViewID)
		probe.SpaceName = firstNonEmpty(probe.SpaceName, discovered.SpaceName)
		probe.ClientVersion = firstNonEmpty(probe.ClientVersion, discovered.ClientVersion)
	}
	probe.ClientVersion = firstNonEmpty(probe.ClientVersion, "23.13.0.0")
	if probe.Email == "" || probe.UserID == "" || probe.SpaceID == "" || probe.ClientVersion == "" {
		if discoverErr != nil {
			return dto.AccountLoginResponse{}, fmt.Errorf("%w: email, user_id, space_id and client_version are required; auto-discovery failed: %v", ErrInvalid, discoverErr)
		}
		return dto.AccountLoginResponse{}, fmt.Errorf("%w: email, user_id, space_id and client_version are required", ErrInvalid)
	}
	probe.UserName = firstNonEmpty(probe.UserName, accountDefaultName(probe.Email))
	probe.SpaceName = firstNonEmpty(probe.SpaceName, probe.UserName+"'s Space")
	probe.Cookies = cookies
	paths := s.accountSessionPaths(probe.Email)
	if err := writeAccountSessionFiles(paths, probe); err != nil {
		return dto.AccountLoginResponse{}, err
	}

	now := time.Now()
	account, err := s.accountByEmailOrNew(ctx, probe.Email)
	if err != nil {
		return dto.AccountLoginResponse{}, err
	}
	account.Email = probe.Email
	account.Enabled = true
	account.Active = req.Active
	account.UserID = probe.UserID
	account.UserName = probe.UserName
	account.SpaceID = probe.SpaceID
	account.SpaceViewID = probe.SpaceViewID
	account.SpaceName = probe.SpaceName
	account.ClientVersion = probe.ClientVersion
	account.Status = "ready"
	account.Priority = req.Priority
	account.HourlyQuota = req.HourlyQuota
	if req.MaxConcurrency > 0 {
		account.MaxConcurrency = req.MaxConcurrency
	} else if account.MaxConcurrency <= 0 {
		account.MaxConcurrency = 1
	}
	account.Cookie = cookieHeaderFromAccountCookies(cookies)
	account.AuthToken = accountCookieValue(cookies, "token_v2")
	account.ProbeJSON = marshalAccountProbe(probe)
	account.ProfileDir = paths.Dir
	account.StorageStatePath = paths.StorageStatePath
	account.PendingStatePath = paths.PendingStatePath
	account.LoginMessage = "manual session imported"
	account.LastError = ""
	account.LastLoginAt = &now

	account, err = s.saveLoginAccount(ctx, account, req.Active)
	if err != nil {
		return dto.AccountLoginResponse{}, err
	}
	status := model.AccountLoginSession{
		Email:            probe.Email,
		Status:           "ready",
		Success:          true,
		Message:          "manual session imported",
		ClientVersion:    probe.ClientVersion,
		CookiesJSON:      marshalAccountCookies(cookies),
		ProfileDir:       paths.Dir,
		StorageStatePath: paths.StorageStatePath,
		PendingStatePath: paths.PendingStatePath,
		ProbePath:        paths.ProbePath,
		CurrentURL:       strings.TrimRight(firstNonEmpty(s.cfg.Upstream.BaseURL, "https://www.notion.so"), "/") + "/",
		LastLoginAt:      &now,
	}
	if err := s.store.UpsertLoginSession(ctx, &status); err != nil {
		return dto.AccountLoginResponse{}, err
	}
	return dto.AccountLoginResponse{Success: true, Account: AccountDTO(account), Status: AccountLoginStatusDTO(status)}, nil
}

func (s *AccountService) StartEmailLogin(ctx context.Context, req dto.AccountLoginStartRequest) (dto.AccountLoginResponse, error) {
	email := strings.TrimSpace(req.Email)
	if email == "" {
		return dto.AccountLoginResponse{}, ErrInvalid
	}
	paths := s.accountSessionPaths(email)
	_ = os.MkdirAll(paths.Dir, 0o755)
	now := time.Now()
	expires := now.Add(time.Duration(s.cfg.LoginHelper.TimeoutSec) * time.Second)
	account, err := s.accountByEmailOrNew(ctx, email)
	if err != nil {
		return dto.AccountLoginResponse{}, err
	}
	account.Email = email
	account.Enabled = true
	account.Status = "pending_code"
	account.ProfileDir = paths.Dir
	account.StorageStatePath = paths.StorageStatePath
	account.PendingStatePath = paths.PendingStatePath
	account.LoginMessage = "temporary password requested"
	account.LastError = ""
	if account.MaxConcurrency <= 0 {
		account.MaxConcurrency = 1
	}

	client, jar, base, err := s.newLoginHTTPClient()
	if err != nil {
		return dto.AccountLoginResponse{}, err
	}
	clientVersion, deviceID, err := bootstrapNotionLogin(ctx, client, jar, base)
	if err != nil {
		return s.saveLoginFailure(ctx, account, paths, clientVersion, "login_page_failed", err)
	}
	loginOptionsToken, err := s.getLoginOptions(ctx, client, base, email, clientVersion)
	if err != nil {
		return s.saveLoginFailure(ctx, account, paths, clientVersion, "get_login_options_failed", err)
	}
	csrfState, err := s.sendTemporaryPassword(ctx, client, base, email, clientVersion, loginOptionsToken, deviceID)
	if err != nil {
		return s.saveLoginFailure(ctx, account, paths, clientVersion, "send_code_failed", err)
	}

	account.ClientVersion = clientVersion
	account, err = s.saveLoginAccount(ctx, account, false)
	if err != nil {
		return dto.AccountLoginResponse{}, err
	}
	status := model.AccountLoginSession{
		Email:             email,
		Status:            "pending_code",
		Success:           false,
		Message:           "temporary password requested",
		ClientVersion:     clientVersion,
		LoginOptionsToken: loginOptionsToken,
		CSRFState:         csrfState,
		DeviceID:          deviceID,
		CookiesJSON:       marshalAccountCookies(cookiesFromJar(jar, base)),
		ProfileDir:        paths.Dir,
		StorageStatePath:  paths.StorageStatePath,
		PendingStatePath:  paths.PendingStatePath,
		ProbePath:         paths.ProbePath,
		CurrentURL:        strings.TrimRight(base.String(), "/") + "/login",
		ExpiresAt:         &expires,
	}
	if err := writeAccountLoginStatus(paths.PendingStatePath, status); err != nil {
		return dto.AccountLoginResponse{}, err
	}
	if err := s.store.UpsertLoginSession(ctx, &status); err != nil {
		return dto.AccountLoginResponse{}, err
	}
	return dto.AccountLoginResponse{Success: true, Account: AccountDTO(account), Status: AccountLoginStatusDTO(status)}, nil
}

func (s *AccountService) VerifyEmailLogin(ctx context.Context, req dto.AccountLoginVerifyRequest) (dto.AccountLoginResponse, error) {
	email := strings.TrimSpace(req.Email)
	code := strings.TrimSpace(req.Code)
	if email == "" || code == "" {
		return dto.AccountLoginResponse{}, ErrInvalid
	}
	status, err := s.store.GetLoginSession(ctx, email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.AccountLoginResponse{}, ErrNotFound
		}
		return dto.AccountLoginResponse{}, err
	}
	if status.ExpiresAt != nil && time.Now().After(*status.ExpiresAt) {
		return dto.AccountLoginResponse{}, fmt.Errorf("%w: login session expired", ErrInvalid)
	}
	account, err := s.accountByEmailOrNew(ctx, email)
	if err != nil {
		return dto.AccountLoginResponse{}, err
	}
	paths := s.accountSessionPaths(email)
	client, jar, base, err := s.newLoginHTTPClient()
	if err != nil {
		return dto.AccountLoginResponse{}, err
	}
	restoreCookiesToJar(jar, base, unmarshalAccountCookies(status.CookiesJSON))
	clientVersion := firstNonEmpty(status.ClientVersion, account.ClientVersion, "23.13.0.0")
	userID, err := s.loginWithEmail(ctx, client, base, clientVersion, status.CSRFState, code)
	if err != nil {
		return s.saveLoginFailure(ctx, account, paths, clientVersion, "verify_code_failed", err)
	}
	bootstrap, err := s.getSpacesInitial(ctx, client, base, clientVersion, userID)
	if err != nil {
		return s.saveLoginFailure(ctx, account, paths, clientVersion, "load_spaces_failed", err)
	}
	bootstrap.Email = firstNonEmpty(bootstrap.Email, email)
	bootstrap.UserID = firstNonEmpty(bootstrap.UserID, userID)
	bootstrap.ClientVersion = clientVersion
	bootstrap.Cookies = normalizeAccountCookies(cookiesFromJar(jar, base))
	if len(bootstrap.Cookies) == 0 {
		return s.saveLoginFailure(ctx, account, paths, clientVersion, "no_cookies", fmt.Errorf("login completed but no cookies were captured"))
	}
	if err := writeAccountSessionFiles(paths, bootstrap); err != nil {
		return dto.AccountLoginResponse{}, err
	}

	now := time.Now()
	account.Email = bootstrap.Email
	account.Enabled = true
	account.Active = true
	account.UserID = bootstrap.UserID
	account.UserName = bootstrap.UserName
	account.SpaceID = bootstrap.SpaceID
	account.SpaceViewID = bootstrap.SpaceViewID
	account.SpaceName = bootstrap.SpaceName
	account.ClientVersion = bootstrap.ClientVersion
	account.Status = "ready"
	account.Cookie = cookieHeaderFromAccountCookies(bootstrap.Cookies)
	account.AuthToken = accountCookieValue(bootstrap.Cookies, "token_v2")
	account.ProbeJSON = marshalAccountProbe(bootstrap)
	account.ProfileDir = paths.Dir
	account.StorageStatePath = paths.StorageStatePath
	account.PendingStatePath = paths.PendingStatePath
	account.LoginMessage = "session refreshed"
	account.LastError = ""
	account.LastLoginAt = &now
	if account.MaxConcurrency <= 0 {
		account.MaxConcurrency = 1
	}
	account, err = s.saveLoginAccount(ctx, account, true)
	if err != nil {
		return dto.AccountLoginResponse{}, err
	}
	status = model.AccountLoginSession{
		Email:            account.Email,
		Status:           "ready",
		Success:          true,
		Message:          "session refreshed",
		ClientVersion:    clientVersion,
		DeviceID:         status.DeviceID,
		CookiesJSON:      marshalAccountCookies(bootstrap.Cookies),
		ProfileDir:       paths.Dir,
		StorageStatePath: paths.StorageStatePath,
		PendingStatePath: paths.PendingStatePath,
		ProbePath:        paths.ProbePath,
		CurrentURL:       strings.TrimRight(base.String(), "/") + "/",
		FinalURL:         strings.TrimRight(base.String(), "/") + "/",
		Title:            "Notion",
		LastLoginAt:      &now,
	}
	if err := writeAccountLoginStatus(paths.PendingStatePath, status); err != nil {
		return dto.AccountLoginResponse{}, err
	}
	if err := s.store.UpsertLoginSession(ctx, &status); err != nil {
		return dto.AccountLoginResponse{}, err
	}
	return dto.AccountLoginResponse{Success: true, Account: AccountDTO(account), Status: AccountLoginStatusDTO(status)}, nil
}

func (s *AccountService) RefreshSession(ctx context.Context, id uint) (model.Account, error) {
	account, err := s.Get(ctx, id)
	if err != nil {
		return model.Account{}, err
	}
	paths := s.accountSessionPaths(account.Email)
	cookies := normalizeAccountCookies(mergeAccountCookies(
		parseAccountCookieHeader(account.Cookie),
		[]accountProbeCookie{{Name: "token_v2", Value: strings.TrimSpace(account.AuthToken)}},
	))
	if len(cookies) == 0 {
		err := fmt.Errorf("%w: account credentials are required", ErrInvalid)
		s.writeRefreshFailure(ctx, account, paths, err)
		return model.Account{}, err
	}
	client, jar, base, err := s.newLoginHTTPClient()
	if err != nil {
		s.writeRefreshFailure(ctx, account, paths, err)
		return model.Account{}, err
	}
	restoreCookiesToJar(jar, base, cookies)
	clientVersion, _, err := bootstrapNotionLogin(ctx, client, jar, base)
	if err != nil {
		s.writeRefreshFailure(ctx, account, paths, err)
		return model.Account{}, err
	}
	clientVersion = firstNonEmpty(clientVersion, account.ClientVersion, "23.13.0.0")
	activeUserID := firstNonEmpty(
		account.UserID,
		accountCookieValue(cookiesFromJar(jar, base), "notion_user_id"),
		accountCookieValue(cookies, "notion_user_id"),
	)
	if activeUserID == "" {
		err := fmt.Errorf("%w: notion_user_id missing during session refresh", ErrInvalid)
		s.writeRefreshFailure(ctx, account, paths, err)
		return model.Account{}, err
	}
	spaces, err := s.getSpacesInitial(ctx, client, base, clientVersion, activeUserID)
	if err != nil {
		s.writeRefreshFailure(ctx, account, paths, err)
		return model.Account{}, err
	}
	refreshedCookies := cookiesFromJar(jar, base)
	if len(refreshedCookies) == 0 {
		refreshedCookies = cookies
	}
	probe := accountProbePayload{
		Email:         firstNonEmpty(account.Email, spaces.Email),
		UserID:        firstNonEmpty(account.UserID, spaces.UserID, activeUserID),
		UserName:      firstNonEmpty(spaces.UserName, account.UserName, accountDefaultName(account.Email)),
		SpaceID:       firstNonEmpty(account.SpaceID, spaces.SpaceID),
		SpaceViewID:   firstNonEmpty(account.SpaceViewID, spaces.SpaceViewID),
		SpaceName:     firstNonEmpty(account.SpaceName, spaces.SpaceName),
		ClientVersion: clientVersion,
		Cookies:       refreshedCookies,
	}
	if probe.SpaceName == "" {
		probe.SpaceName = probe.UserName + "'s Space"
	}
	if err := writeAccountSessionFiles(paths, probe); err != nil {
		s.writeRefreshFailure(ctx, account, paths, err)
		return model.Account{}, err
	}
	now := time.Now()
	account.UserID = probe.UserID
	account.UserName = probe.UserName
	account.SpaceID = probe.SpaceID
	account.SpaceViewID = probe.SpaceViewID
	account.SpaceName = probe.SpaceName
	account.ClientVersion = probe.ClientVersion
	account.Cookie = cookieHeaderFromAccountCookies(refreshedCookies)
	account.AuthToken = accountCookieValue(refreshedCookies, "token_v2")
	account.ProbeJSON = marshalAccountProbe(probe)
	account.ProfileDir = paths.Dir
	account.StorageStatePath = paths.StorageStatePath
	account.PendingStatePath = paths.PendingStatePath
	account.Status = "ready"
	account.LoginMessage = "session refreshed"
	account.LastError = ""
	account.LastLoginAt = &now
	account.LastRefreshAt = &now
	account.ConsecutiveFailures = 0
	if account.MaxConcurrency <= 0 {
		account.MaxConcurrency = 1
	}
	if err := s.store.SaveAccount(ctx, &account); err != nil {
		s.writeRefreshFailure(ctx, account, paths, err)
		return model.Account{}, err
	}
	status := model.AccountLoginSession{
		Email:            account.Email,
		Status:           "ready",
		Success:          true,
		Message:          "session refreshed",
		ClientVersion:    probe.ClientVersion,
		CookiesJSON:      marshalAccountCookies(refreshedCookies),
		ProfileDir:       paths.Dir,
		StorageStatePath: paths.StorageStatePath,
		PendingStatePath: paths.PendingStatePath,
		ProbePath:        paths.ProbePath,
		CurrentURL:       strings.TrimRight(base.String(), "/") + "/",
		FinalURL:         strings.TrimRight(base.String(), "/") + "/",
		Title:            "Notion",
		LastLoginAt:      &now,
	}
	_ = writeAccountLoginStatus(paths.PendingStatePath, status)
	_ = s.store.UpsertLoginSession(ctx, &status)
	s.probeCache.markFailure(account.Email)
	return account, nil
}

func (s *AccountService) LoginStatus(ctx context.Context, email string) ([]dto.AccountLoginStatusDTO, error) {
	email = strings.TrimSpace(email)
	if email != "" {
		status, err := s.store.GetLoginSession(ctx, email)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return []dto.AccountLoginStatusDTO{}, nil
			}
			return nil, err
		}
		return []dto.AccountLoginStatusDTO{AccountLoginStatusDTO(status)}, nil
	}
	rows, err := s.store.ListLoginSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.AccountLoginStatusDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, AccountLoginStatusDTO(row))
	}
	return out, nil
}

func (s *AccountService) Spaces(ctx context.Context, id uint) ([]dto.AccountSpaceDTO, error) {
	account, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	cookies := normalizeAccountCookies(mergeAccountCookies(
		parseAccountCookieHeader(account.Cookie),
		[]accountProbeCookie{{Name: "token_v2", Value: strings.TrimSpace(account.AuthToken)}},
	))
	if len(cookies) == 0 {
		return nil, fmt.Errorf("%w: account credentials are required", ErrInvalid)
	}
	client, jar, base, err := s.newLoginHTTPClient()
	if err != nil {
		return nil, err
	}
	restoreCookiesToJar(jar, base, cookies)
	clientVersion := firstNonEmpty(account.ClientVersion, "23.13.0.0")
	activeUserID := firstNonEmpty(account.UserID, accountCookieValue(cookies, "notion_user_id"))
	payload, err := postNotionAccountJSON(ctx, client, base, "getSpacesInitial", clientVersion, activeUserID, "", map[string]any{})
	if err != nil {
		return nil, err
	}
	pointers := collectAccountSpacePointers(payload, activeUserID)
	if account.SpaceID != "" {
		pointers = append([]accountSpacePointer{{SpaceID: account.SpaceID, SpaceViewID: account.SpaceViewID}}, pointers...)
	}
	pointers = uniqueAccountSpacePointers(pointers)
	if len(pointers) == 0 {
		return nil, fmt.Errorf("%w: no spaces found", ErrInvalid)
	}
	spaces := make([]dto.AccountSpaceDTO, 0, len(pointers))
	for _, pointer := range pointers {
		spaces = append(spaces, dto.AccountSpaceDTO{
			ID:          pointer.SpaceID,
			SpaceID:     pointer.SpaceID,
			SpaceViewID: pointer.SpaceViewID,
			Current:     pointer.SpaceID == account.SpaceID,
		})
	}
	details := s.getPublicSpaceDataBySpace(ctx, client, base, clientVersion, activeUserID, pointers)
	applyAccountSpaceDetails(spaces, details)
	for idx := range spaces {
		if spaces[idx].Name == "" && spaces[idx].SpaceID == account.SpaceID {
			spaces[idx].Name = account.SpaceName
			spaces[idx].PlanType = account.PlanType
		}
	}
	return spaces, nil
}

func (s *AccountService) writeRefreshFailure(ctx context.Context, account model.Account, paths accountSessionPaths, cause error) {
	if cause == nil {
		return
	}
	now := time.Now()
	account.Status = "error"
	account.LastError = trimError(cause.Error())
	account.LoginMessage = "session refresh failed"
	account.LastRefreshAt = &now
	account.ProfileDir = firstNonEmpty(account.ProfileDir, paths.Dir)
	account.StorageStatePath = firstNonEmpty(account.StorageStatePath, paths.StorageStatePath)
	account.PendingStatePath = firstNonEmpty(account.PendingStatePath, paths.PendingStatePath)
	_ = s.store.SaveAccount(ctx, &account)
	session := model.AccountLoginSession{
		Email:            account.Email,
		Status:           "error",
		Success:          false,
		Message:          "session refresh failed",
		Error:            trimError(cause.Error()),
		ClientVersion:    account.ClientVersion,
		ProfileDir:       paths.Dir,
		StorageStatePath: paths.StorageStatePath,
		PendingStatePath: paths.PendingStatePath,
		ProbePath:        paths.ProbePath,
		UpdatedAt:        now,
	}
	_ = writeAccountLoginStatus(paths.PendingStatePath, session)
	_ = s.store.UpsertLoginSession(ctx, &session)
	s.probeCache.markFailure(account.Email)
}

func (s *AccountService) accountByEmailOrNew(ctx context.Context, email string) (model.Account, error) {
	account, err := s.store.GetAccountByEmail(ctx, email)
	if err == nil {
		return account, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Account{Email: email, Enabled: true, MaxConcurrency: 1}, nil
	}
	return model.Account{}, err
}

func (s *AccountService) saveLoginAccount(ctx context.Context, account model.Account, activate bool) (model.Account, error) {
	if account.MaxConcurrency <= 0 {
		account.MaxConcurrency = 1
	}
	if account.ID == 0 {
		if err := s.store.CreateAccount(ctx, &account); err != nil {
			return model.Account{}, err
		}
		if err := s.store.EnsureAccountInDefaultGroup(ctx, account); err != nil {
			return model.Account{}, err
		}
	} else if err := s.store.SaveAccount(ctx, &account); err != nil {
		return model.Account{}, err
	}
	if activate {
		return s.store.ActivateAccount(ctx, account.ID)
	}
	return account, nil
}

func (s *AccountService) saveLoginFailure(ctx context.Context, account model.Account, paths accountSessionPaths, clientVersion string, statusText string, cause error) (dto.AccountLoginResponse, error) {
	now := time.Now()
	account.Status = "error"
	account.Enabled = true
	account.ClientVersion = firstNonEmpty(clientVersion, account.ClientVersion)
	account.ProfileDir = paths.Dir
	account.StorageStatePath = paths.StorageStatePath
	account.PendingStatePath = paths.PendingStatePath
	account.LastError = trimError(cause.Error())
	account.LoginMessage = statusText
	if account.MaxConcurrency <= 0 {
		account.MaxConcurrency = 1
	}
	saved, saveErr := s.saveLoginAccount(ctx, account, false)
	if saveErr != nil {
		return dto.AccountLoginResponse{}, saveErr
	}
	session := model.AccountLoginSession{
		Email:            account.Email,
		Status:           statusText,
		Success:          false,
		Message:          statusText,
		Error:            trimError(cause.Error()),
		ClientVersion:    account.ClientVersion,
		ProfileDir:       paths.Dir,
		StorageStatePath: paths.StorageStatePath,
		PendingStatePath: paths.PendingStatePath,
		ProbePath:        paths.ProbePath,
		UpdatedAt:        now,
	}
	_ = writeAccountLoginStatus(paths.PendingStatePath, session)
	_ = s.store.UpsertLoginSession(ctx, &session)
	return dto.AccountLoginResponse{Success: false, Account: AccountDTO(saved), Status: AccountLoginStatusDTO(session)}, nil
}

func (s *AccountService) accountSessionPaths(email string) accountSessionPaths {
	base := strings.TrimSpace(s.cfg.LoginHelper.SessionsDir)
	if base == "" {
		base = "data/notion_accounts"
	}
	dir := filepath.Join(base, accountSlug(email))
	return accountSessionPaths{
		Dir:              dir,
		ProbePath:        filepath.Join(dir, "probe.json"),
		StorageStatePath: filepath.Join(dir, "storage_state.json"),
		PendingStatePath: filepath.Join(dir, "login_status.json"),
	}
}

func (s *AccountService) removeAccountSessionFiles(account model.Account) {
	dir := strings.TrimSpace(account.ProfileDir)
	if dir == "" {
		dir = s.accountSessionPaths(account.Email).Dir
	}
	base := strings.TrimSpace(s.cfg.LoginHelper.SessionsDir)
	if base == "" {
		base = "data/notion_accounts"
	}
	cleanBase := filepath.Clean(base)
	cleanDir := filepath.Clean(dir)
	rel, err := filepath.Rel(cleanBase, cleanDir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return
	}
	_ = os.RemoveAll(cleanDir)
}

func (s *AccountService) newLoginHTTPClient() (*http.Client, *cookiejar.Jar, *url.URL, error) {
	base, err := url.Parse(strings.TrimRight(firstNonEmpty(s.cfg.Upstream.BaseURL, "https://www.notion.so"), "/"))
	if err != nil {
		return nil, nil, nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, nil, err
	}
	timeout := s.cfg.LoginHelper.TimeoutSec
	if timeout <= 0 {
		timeout = 120
	}
	client := &http.Client{Jar: jar, Timeout: time.Duration(timeout) * time.Second}
	return client, jar, base, nil
}

func bootstrapNotionLogin(ctx context.Context, client *http.Client, jar *cookiejar.Jar, base *url.URL) (string, string, error) {
	loginURL := base.ResolveReference(&url.URL{Path: "/login"})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL.String(), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("User-Agent", notionLoginUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("login page returned %d", resp.StatusCode)
	}
	clientVersion := "23.13.0.0"
	if match := notionVersionPattern.FindSubmatch(raw); len(match) > 1 {
		clientVersion = string(match[1])
	}
	deviceID := ""
	for _, cookie := range jar.Cookies(base) {
		if cookie.Name == "notion_browser_id" || cookie.Name == "device_id" {
			deviceID = cookie.Value
			break
		}
	}
	if deviceID == "" {
		deviceID = uuid.NewString()
		jar.SetCookies(base, []*http.Cookie{{Name: "notion_browser_id", Value: deviceID, Path: "/"}})
	}
	return clientVersion, deviceID, nil
}

func (s *AccountService) getLoginOptions(ctx context.Context, client *http.Client, base *url.URL, email string, clientVersion string) (string, error) {
	payload, err := postNotionLoginJSON(ctx, client, base, "getLoginOptions", clientVersion, "", map[string]any{
		"email":                email,
		"requireWorkTypeEmail": false,
	})
	if err != nil {
		return "", err
	}
	token := findStringByKeys(payload, "loginOptionsToken", "login_options_token")
	if token == "" {
		return "", fmt.Errorf("getLoginOptions returned empty loginOptionsToken")
	}
	return token, nil
}

func (s *AccountService) sendTemporaryPassword(ctx context.Context, client *http.Client, base *url.URL, email string, clientVersion string, token string, deviceID string) (string, error) {
	payload, err := postNotionLoginJSON(ctx, client, base, "sendTemporaryPassword", clientVersion, "", map[string]any{
		"email":              email,
		"disableLoginLink":   false,
		"native":             false,
		"isSignup":           false,
		"shouldHidePasscode": false,
		"loginOptionsToken":  token,
		"deviceId":           deviceID,
		"loginRouteOrigin":   "login",
	})
	if err != nil {
		return "", err
	}
	state := findStringByKeys(payload, "csrfState", "csrf_state", "state")
	if state == "" {
		return "", fmt.Errorf("sendTemporaryPassword returned empty csrfState")
	}
	return state, nil
}

func (s *AccountService) loginWithEmail(ctx context.Context, client *http.Client, base *url.URL, clientVersion string, csrfState string, code string) (string, error) {
	payload, err := postNotionLoginJSON(ctx, client, base, "loginWithEmail", clientVersion, "", map[string]any{
		"state":            csrfState,
		"password":         code,
		"loginRouteOrigin": "login",
	})
	if err != nil {
		return "", err
	}
	userID := findStringByKeys(payload, "userId", "user_id")
	if userID == "" {
		return "", fmt.Errorf("loginWithEmail returned empty userId")
	}
	return userID, nil
}

func (s *AccountService) getSpacesInitial(ctx context.Context, client *http.Client, base *url.URL, clientVersion string, userID string) (accountProbePayload, error) {
	payload, err := postNotionLoginJSON(ctx, client, base, "getSpacesInitial", clientVersion, userID, map[string]any{})
	if err != nil {
		return accountProbePayload{}, err
	}
	spaceID, spaceViewID := findSpacePointer(payload)
	probe := accountProbePayload{
		Email:         findStringByKeys(payload, "email"),
		UserID:        firstNonEmpty(userID, findStringByKeys(payload, "userId", "user_id")),
		UserName:      findStringByKeys(payload, "userName", "user_name", "name"),
		SpaceID:       firstNonEmpty(spaceID, findStringByKeys(payload, "spaceId", "space_id")),
		SpaceViewID:   firstNonEmpty(spaceViewID, findStringByKeys(payload, "spaceViewId", "space_view_id")),
		SpaceName:     findStringByKeys(payload, "spaceName", "space_name"),
		ClientVersion: clientVersion,
	}
	if probe.SpaceID == "" {
		return accountProbePayload{}, fmt.Errorf("getSpacesInitial returned empty space_id")
	}
	return probe, nil
}

func (s *AccountService) discoverImportedAccountMetadata(ctx context.Context, fallback accountProbePayload, cookies []accountProbeCookie) (accountProbePayload, error) {
	client, jar, base, err := s.newLoginHTTPClient()
	if err != nil {
		return fallback, err
	}
	restoreCookiesToJar(jar, base, cookies)
	clientVersion := strings.TrimSpace(fallback.ClientVersion)
	if clientVersion == "" {
		discoveredVersion, _, err := bootstrapNotionLogin(ctx, client, jar, base)
		if err == nil {
			clientVersion = discoveredVersion
		}
	}
	clientVersion = firstNonEmpty(clientVersion, "23.13.0.0")
	meta := fallback
	meta.ClientVersion = clientVersion
	activeUserID := firstNonEmpty(
		meta.UserID,
		accountCookieValue(cookies, "notion_user_id"),
		accountCookieValue(cookiesFromJar(jar, base), "notion_user_id"),
	)
	var primaryErr error
	if discovered, err := s.loadUserContentMetadata(ctx, client, base, clientVersion, activeUserID); err == nil {
		meta.Email = firstNonEmpty(meta.Email, discovered.Email)
		meta.UserID = firstNonEmpty(meta.UserID, discovered.UserID)
		meta.UserName = firstNonEmpty(meta.UserName, discovered.UserName)
		meta.SpaceID = firstNonEmpty(meta.SpaceID, discovered.SpaceID)
		meta.SpaceViewID = firstNonEmpty(meta.SpaceViewID, discovered.SpaceViewID)
		meta.SpaceName = firstNonEmpty(meta.SpaceName, discovered.SpaceName)
		activeUserID = firstNonEmpty(meta.UserID, activeUserID)
	} else {
		primaryErr = err
	}
	if (meta.UserID == "" || meta.SpaceID == "" || meta.Email == "") && activeUserID != "" {
		if discovered, err := s.getSpacesInitial(ctx, client, base, clientVersion, activeUserID); err == nil {
			meta.Email = firstNonEmpty(meta.Email, discovered.Email)
			meta.UserID = firstNonEmpty(meta.UserID, discovered.UserID, activeUserID)
			meta.UserName = firstNonEmpty(meta.UserName, discovered.UserName)
			meta.SpaceID = firstNonEmpty(meta.SpaceID, discovered.SpaceID)
			meta.SpaceViewID = firstNonEmpty(meta.SpaceViewID, discovered.SpaceViewID)
			meta.SpaceName = firstNonEmpty(meta.SpaceName, discovered.SpaceName)
		} else if primaryErr == nil {
			primaryErr = err
		}
	}
	if meta.Email == "" || meta.UserID == "" || meta.SpaceID == "" || meta.ClientVersion == "" {
		if primaryErr != nil {
			return meta, primaryErr
		}
		return meta, fmt.Errorf("auto-discovery incomplete")
	}
	return meta, nil
}

func (s *AccountService) loadUserContentMetadata(ctx context.Context, client *http.Client, base *url.URL, clientVersion string, activeUserID string) (accountProbePayload, error) {
	payload, err := postNotionLoginJSON(ctx, client, base, "loadUserContent", clientVersion, activeUserID, map[string]any{})
	if err != nil {
		return accountProbePayload{}, err
	}
	recordMap, _ := payload.(map[string]any)
	meta := parseLoadUserContentProbe(recordMap)
	if meta.Email == "" && meta.UserID == "" && meta.SpaceID == "" {
		return accountProbePayload{}, fmt.Errorf("loadUserContent returned no account metadata")
	}
	meta.ClientVersion = clientVersion
	return meta, nil
}

func postNotionLoginJSON(ctx context.Context, client *http.Client, base *url.URL, action string, clientVersion string, activeUserID string, body map[string]any) (any, error) {
	return postNotionAccountJSON(ctx, client, base, action, clientVersion, activeUserID, "", body)
}

func postNotionAccountJSON(ctx context.Context, client *http.Client, base *url.URL, action string, clientVersion string, activeUserID string, activeSpaceID string, body map[string]any) (any, error) {
	endpoint := base.ResolveReference(&url.URL{Path: "/api/v3/" + action})
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", strings.TrimRight(base.String(), "/"))
	req.Header.Set("Referer", strings.TrimRight(base.String(), "/")+"/login")
	req.Header.Set("User-Agent", notionLoginUserAgent)
	req.Header.Set("Notion-Client-Version", firstNonEmpty(clientVersion, "23.13.0.0"))
	req.Header.Set("Notion-Audit-Log-Platform", "web")
	if activeUserID != "" {
		req.Header.Set("X-Notion-Active-User-Header", activeUserID)
	}
	if activeSpaceID != "" {
		req.Header.Set("X-Notion-Space-ID", activeSpaceID)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respRaw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %d: %s", action, resp.StatusCode, strings.TrimSpace(string(respRaw)))
	}
	var payload any
	if len(bytes.TrimSpace(respRaw)) == 0 {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal(respRaw, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *AccountService) getPublicSpaceData(ctx context.Context, client *http.Client, base *url.URL, clientVersion string, activeUserID string, activeSpaceID string, pointers []accountSpacePointer) (map[string]dto.AccountSpaceDTO, error) {
	ids := make([]string, 0, len(pointers))
	seen := map[string]struct{}{}
	for _, pointer := range pointers {
		if pointer.SpaceID == "" {
			continue
		}
		if _, ok := seen[pointer.SpaceID]; ok {
			continue
		}
		seen[pointer.SpaceID] = struct{}{}
		ids = append(ids, pointer.SpaceID)
	}
	payload, err := postNotionAccountJSON(ctx, client, base, "getPublicSpaceData", clientVersion, activeUserID, activeSpaceID, map[string]any{
		"type":          "space-ids",
		"spaceIds":      ids,
		"includeCounts": false,
	})
	if err != nil {
		return nil, err
	}
	root := accountMapValue(payload)
	results := accountSliceValue(root["results"])
	out := make(map[string]dto.AccountSpaceDTO, len(results))
	for _, raw := range results {
		item := accountMapValue(raw)
		spaceID := strings.TrimSpace(stringFromAny(item["id"]))
		if spaceID == "" {
			continue
		}
		out[spaceID] = dto.AccountSpaceDTO{
			ID:             spaceID,
			SpaceID:        spaceID,
			Name:           strings.TrimSpace(stringFromAny(item["name"])),
			PlanType:       strings.TrimSpace(stringFromAny(item["planType"])),
			MembershipType: strings.TrimSpace(stringFromAny(item["membershipType"])),
			Role:           strings.TrimSpace(stringFromAny(item["role"])),
			Domain:         strings.TrimSpace(stringFromAny(item["domain"])),
			Region:         strings.TrimSpace(stringFromAny(item["region"])),
		}
	}
	return out, nil
}

func (s *AccountService) getPublicSpaceDataBySpace(ctx context.Context, client *http.Client, base *url.URL, clientVersion string, activeUserID string, pointers []accountSpacePointer) map[string]dto.AccountSpaceDTO {
	out := make(map[string]dto.AccountSpaceDTO, len(pointers))
	for _, pointer := range pointers {
		if pointer.SpaceID == "" {
			continue
		}
		details, err := s.getPublicSpaceData(ctx, client, base, clientVersion, activeUserID, pointer.SpaceID, []accountSpacePointer{pointer})
		if err != nil {
			continue
		}
		for spaceID, detail := range details {
			out[spaceID] = detail
		}
	}
	return out
}

func parseAccountProbeJSON(raw string) (accountProbePayload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return accountProbePayload{}, nil
	}
	var root any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return accountProbePayload{}, err
	}
	var probe accountProbePayload
	_ = json.Unmarshal([]byte(raw), &probe)
	probe.Email = firstNonEmpty(probe.Email, findStringByKeys(root, "email"))
	probe.UserID = firstNonEmpty(probe.UserID, findStringByKeys(root, "user_id", "userId"))
	probe.UserName = firstNonEmpty(probe.UserName, findStringByKeys(root, "user_name", "userName"))
	probe.SpaceID = firstNonEmpty(probe.SpaceID, findStringByKeys(root, "space_id", "spaceId"))
	probe.SpaceViewID = firstNonEmpty(probe.SpaceViewID, findStringByKeys(root, "space_view_id", "spaceViewId", "spaceViewID"))
	probe.SpaceName = firstNonEmpty(probe.SpaceName, findStringByKeys(root, "space_name", "spaceName"))
	probe.ClientVersion = firstNonEmpty(probe.ClientVersion, findStringByKeys(root, "client_version", "clientVersion", "notion_client_version"))
	probe.Cookies = normalizeAccountCookies(mergeAccountCookies(probe.Cookies, findAccountCookies(root)))
	return probe, nil
}

func parseAccountCookieHeader(header string) []accountProbeCookie {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	if !strings.Contains(header, "=") {
		return []accountProbeCookie{{Name: "token_v2", Value: header}}
	}
	parts := strings.Split(header, ";")
	cookies := make([]accountProbeCookie, 0, len(parts))
	for _, part := range parts {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		cookies = append(cookies, accountProbeCookie{Name: name, Value: value})
	}
	return cookies
}

func findAccountCookies(value any) []accountProbeCookie {
	var out []accountProbeCookie
	switch typed := value.(type) {
	case map[string]any:
		if name := stringFromAny(typed["name"]); name != "" {
			if value := stringFromAny(typed["value"]); value != "" {
				out = append(out, accountProbeCookie{
					Name:     name,
					Value:    value,
					Domain:   stringFromAny(typed["domain"]),
					Path:     stringFromAny(typed["path"]),
					SameSite: stringFromAny(typed["sameSite"]),
					HTTPOnly: boolValueFromAny(typed["httpOnly"]),
					Secure:   boolValueFromAny(typed["secure"]),
				})
				return out
			}
		}
		for _, child := range typed {
			out = append(out, findAccountCookies(child)...)
		}
	case []any:
		for _, child := range typed {
			out = append(out, findAccountCookies(child)...)
		}
	}
	return out
}

func mergeAccountCookies(groups ...[]accountProbeCookie) []accountProbeCookie {
	var merged []accountProbeCookie
	seen := map[string]int{}
	for _, group := range groups {
		for _, cookie := range group {
			cookie.Name = strings.TrimSpace(cookie.Name)
			cookie.Value = strings.TrimSpace(cookie.Value)
			if cookie.Name == "" || cookie.Value == "" {
				continue
			}
			key := strings.ToLower(cookie.Name)
			if idx, ok := seen[key]; ok {
				merged[idx] = cookie
				continue
			}
			seen[key] = len(merged)
			merged = append(merged, cookie)
		}
	}
	return merged
}

func normalizeAccountCookies(cookies []accountProbeCookie) []accountProbeCookie {
	cookies = mergeAccountCookies(cookies)
	for idx := range cookies {
		if cookies[idx].Domain == "" {
			cookies[idx].Domain = ".notion.so"
		}
		if cookies[idx].Path == "" {
			cookies[idx].Path = "/"
		}
	}
	return cookies
}

func cookieHeaderFromAccountCookies(cookies []accountProbeCookie) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name != "" && cookie.Value != "" {
			parts = append(parts, cookie.Name+"="+cookie.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func accountCookieValue(cookies []accountProbeCookie, name string) string {
	for _, cookie := range cookies {
		if strings.EqualFold(cookie.Name, name) {
			return cookie.Value
		}
	}
	return ""
}

func marshalAccountProbe(probe accountProbePayload) string {
	raw, err := json.MarshalIndent(probe, "", "  ")
	if err != nil {
		return ""
	}
	return string(raw)
}

func marshalAccountCookies(cookies []accountProbeCookie) string {
	raw, err := json.Marshal(cookies)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func unmarshalAccountCookies(raw string) []accountProbeCookie {
	var cookies []accountProbeCookie
	_ = json.Unmarshal([]byte(raw), &cookies)
	return normalizeAccountCookies(cookies)
}

func writeAccountSessionFiles(paths accountSessionPaths, probe accountProbePayload) error {
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return err
	}
	if err := writeJSONFile(paths.ProbePath, probe); err != nil {
		return err
	}
	return writeJSONFile(paths.StorageStatePath, storageState{Cookies: normalizeAccountCookies(probe.Cookies), Origins: []any{}})
}

func writeAccountLoginStatus(path string, status model.AccountLoginSession) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeJSONFile(path, AccountLoginStatusDTO(status))
}

func writeJSONFile(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func cookiesFromJar(jar *cookiejar.Jar, base *url.URL) []accountProbeCookie {
	cookies := jar.Cookies(base)
	out := make([]accountProbeCookie, 0, len(cookies))
	for _, cookie := range cookies {
		out = append(out, accountProbeCookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   ".notion.so",
			Path:     firstNonEmpty(cookie.Path, "/"),
			HTTPOnly: cookie.HttpOnly,
			Secure:   cookie.Secure,
		})
	}
	return normalizeAccountCookies(out)
}

func restoreCookiesToJar(jar *cookiejar.Jar, base *url.URL, cookies []accountProbeCookie) {
	httpCookies := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name == "" || cookie.Value == "" {
			continue
		}
		httpCookies = append(httpCookies, &http.Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Path:     firstNonEmpty(cookie.Path, "/"),
			Domain:   cookie.Domain,
			HttpOnly: cookie.HTTPOnly,
			Secure:   cookie.Secure,
		})
	}
	jar.SetCookies(base, httpCookies)
}

func findStringByKeys(value any, keys ...string) string {
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[strings.ToLower(key)] = struct{}{}
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, ok := keySet[strings.ToLower(key)]; ok {
				if text := stringFromAny(child); text != "" {
					return text
				}
			}
		}
		for _, child := range typed {
			if text := findStringByKeys(child, keys...); text != "" {
				return text
			}
		}
	case []any:
		for _, child := range typed {
			if text := findStringByKeys(child, keys...); text != "" {
				return text
			}
		}
	}
	return ""
}

func findSpacePointer(value any) (string, string) {
	switch typed := value.(type) {
	case map[string]any:
		spaceID := firstNonEmpty(stringFromAny(typed["spaceId"]), stringFromAny(typed["space_id"]))
		if spaceID != "" {
			return spaceID, firstNonEmpty(stringFromAny(typed["spaceViewId"]), stringFromAny(typed["space_view_id"]), stringFromAny(typed["id"]))
		}
		for _, child := range typed {
			if spaceID, viewID := findSpacePointer(child); spaceID != "" {
				return spaceID, viewID
			}
		}
	case []any:
		for _, child := range typed {
			if spaceID, viewID := findSpacePointer(child); spaceID != "" {
				return spaceID, viewID
			}
		}
	}
	return "", ""
}

func collectAccountSpacePointers(value any, userID string) []accountSpacePointer {
	var out []accountSpacePointer
	collectAccountSpacePointersInto(value, strings.TrimSpace(userID), &out)
	return uniqueAccountSpacePointers(out)
}

func collectAccountSpacePointersInto(value any, userID string, out *[]accountSpacePointer) {
	switch typed := value.(type) {
	case map[string]any:
		if roots := accountMapValue(typed["user_root"]); roots != nil && userID != "" {
			if root := unwrapAccountRecordValue(roots[userID]); root != nil {
				for _, rawPointer := range accountSliceValue(root["space_view_pointers"]) {
					pointer := accountMapValue(rawPointer)
					spaceID := strings.TrimSpace(stringFromAny(pointer["spaceId"]))
					if spaceID == "" {
						continue
					}
					*out = append(*out, accountSpacePointer{
						SpaceID:     spaceID,
						SpaceViewID: strings.TrimSpace(stringFromAny(pointer["id"])),
					})
				}
			}
		}
		if table := strings.TrimSpace(stringFromAny(typed["table"])); table == "space_view" {
			spaceID := strings.TrimSpace(stringFromAny(typed["spaceId"]))
			if spaceID != "" {
				*out = append(*out, accountSpacePointer{
					SpaceID:     spaceID,
					SpaceViewID: strings.TrimSpace(stringFromAny(typed["id"])),
				})
			}
		}
		for _, child := range typed {
			collectAccountSpacePointersInto(child, userID, out)
		}
	case []any:
		for _, child := range typed {
			collectAccountSpacePointersInto(child, userID, out)
		}
	}
}

func uniqueAccountSpacePointers(pointers []accountSpacePointer) []accountSpacePointer {
	out := make([]accountSpacePointer, 0, len(pointers))
	seen := map[string]int{}
	for _, pointer := range pointers {
		pointer.SpaceID = strings.TrimSpace(pointer.SpaceID)
		pointer.SpaceViewID = strings.TrimSpace(pointer.SpaceViewID)
		if pointer.SpaceID == "" {
			continue
		}
		if idx, ok := seen[pointer.SpaceID]; ok {
			if out[idx].SpaceViewID == "" {
				out[idx].SpaceViewID = pointer.SpaceViewID
			}
			continue
		}
		seen[pointer.SpaceID] = len(out)
		out = append(out, pointer)
	}
	return out
}

func applyAccountSpaceDetails(spaces []dto.AccountSpaceDTO, details map[string]dto.AccountSpaceDTO) {
	for idx := range spaces {
		detail, ok := details[spaces[idx].SpaceID]
		if !ok {
			continue
		}
		spaces[idx].Name = detail.Name
		spaces[idx].PlanType = detail.PlanType
		spaces[idx].MembershipType = detail.MembershipType
		spaces[idx].Role = detail.Role
		spaces[idx].Domain = detail.Domain
		spaces[idx].Region = detail.Region
	}
}

func parseLoadUserContentProbe(payload map[string]any) accountProbePayload {
	recordMap := accountMapValue(payload["recordMap"])
	if recordMap == nil {
		recordMap = payload
	}
	users := accountMapValue(recordMap["notion_user"])
	var meta accountProbePayload
	for userID, rawUser := range users {
		value := unwrapAccountRecordValue(rawUser)
		if value == nil {
			continue
		}
		meta.UserID = strings.TrimSpace(userID)
		meta.Email = strings.TrimSpace(stringFromAny(value["email"]))
		meta.UserName = strings.TrimSpace(stringFromAny(value["name"]))
		break
	}
	spaceID, viewID, spaceName := chooseAccountSpace(recordMap, meta.UserID)
	meta.SpaceID = spaceID
	meta.SpaceViewID = viewID
	meta.SpaceName = spaceName
	return meta
}

func chooseAccountSpace(recordMap map[string]any, userID string) (string, string, string) {
	if recordMap == nil || userID == "" {
		return "", "", ""
	}
	userRoots := accountMapValue(recordMap["user_root"])
	spaces := accountMapValue(recordMap["space"])
	root := unwrapAccountRecordValue(userRoots[userID])
	if root == nil {
		return "", "", ""
	}
	pointers := accountSliceValue(root["space_view_pointers"])
	bestScore := -1
	var bestID, bestViewID, bestName string
	for _, rawPointer := range pointers {
		pointer := accountMapValue(rawPointer)
		spaceID := strings.TrimSpace(stringFromAny(pointer["spaceId"]))
		if spaceID == "" {
			continue
		}
		space := unwrapAccountRecordValue(spaces[spaceID])
		if space == nil {
			continue
		}
		settings := accountMapValue(space["settings"])
		disabledAI := boolValueFromAny(settings["disable_ai_feature"])
		enabledAI := boolValueFromAny(settings["enable_ai_feature"])
		name := strings.TrimSpace(stringFromAny(space["name"]))
		planType := strings.TrimSpace(stringFromAny(space["plan_type"]))
		score := 0
		if enabledAI || !disabledAI {
			score += 2
		}
		if planType != "" && planType != "free" {
			score++
		}
		if name != "" {
			score++
		}
		if score > bestScore {
			bestScore = score
			bestID = firstNonEmpty(strings.TrimSpace(stringFromAny(space["id"])), spaceID)
			bestViewID = strings.TrimSpace(stringFromAny(pointer["id"]))
			bestName = name
		}
	}
	return bestID, bestViewID, bestName
}

func unwrapAccountRecordValue(raw any) map[string]any {
	node := accountMapValue(raw)
	if node == nil {
		return nil
	}
	if inner := accountMapValue(node["value"]); inner != nil {
		if nested := accountMapValue(inner["value"]); nested != nil {
			return nested
		}
		return inner
	}
	return node
}

func accountMapValue(value any) map[string]any {
	typed, _ := value.(map[string]any)
	return typed
}

func accountSliceValue(value any) []any {
	typed, _ := value.([]any)
	return typed
}

func boolValueFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}

func accountSlug(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range email {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return strings.Trim(b.String(), "_")
}

func accountDefaultName(email string) string {
	localPart := strings.TrimSpace(email)
	if idx := strings.Index(localPart, "@"); idx > 0 {
		localPart = localPart[:idx]
	}
	return firstNonEmpty(localPart, accountSlug(email))
}

func AccountLoginStatusDTO(status model.AccountLoginSession) dto.AccountLoginStatusDTO {
	return dto.AccountLoginStatusDTO{
		ID:               status.ID,
		Email:            status.Email,
		Status:           status.Status,
		Success:          status.Success,
		Message:          status.Message,
		Error:            status.Error,
		ClientVersion:    status.ClientVersion,
		DeviceID:         status.DeviceID,
		ProfileDir:       status.ProfileDir,
		StorageStatePath: status.StorageStatePath,
		PendingStatePath: status.PendingStatePath,
		ProbePath:        status.ProbePath,
		CurrentURL:       status.CurrentURL,
		FinalURL:         status.FinalURL,
		Title:            status.Title,
		LastLoginAt:      status.LastLoginAt,
		ExpiresAt:        status.ExpiresAt,
		CreatedAt:        status.CreatedAt,
		UpdatedAt:        status.UpdatedAt,
	}
}
