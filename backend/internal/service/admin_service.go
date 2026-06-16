package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/model"
	"notaion2api/backend/internal/pkg/security"
	"notaion2api/backend/internal/repository"

	"gorm.io/gorm"
)

type AdminService struct {
	store *repository.Store
	ttl   time.Duration
}

func NewAdminService(store *repository.Store, ttlHours int) *AdminService {
	if ttlHours <= 0 {
		ttlHours = 24
	}
	return &AdminService{store: store, ttl: time.Duration(ttlHours) * time.Hour}
}

func (s *AdminService) SeedBootstrapAdmin(ctx context.Context, cfg config.BootstrapAdminConfig) error {
	count, err := s.store.CountAdmins(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	username := strings.TrimSpace(cfg.Username)
	password := strings.TrimSpace(cfg.Password)
	if username == "" || password == "" {
		return nil
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}
	return s.store.CreateAdmin(ctx, &model.AdminUser{
		Username:     username,
		PasswordHash: hash,
		Role:         "super_admin",
		Enabled:      true,
	})
}

func (s *AdminService) Login(ctx context.Context, req dto.LoginRequest, userAgent string, clientIP string) (dto.LoginResponse, error) {
	admin, err := s.store.GetAdminByUsername(ctx, strings.TrimSpace(req.Username))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.LoginResponse{}, ErrUnauthorized
		}
		return dto.LoginResponse{}, err
	}
	if !admin.Enabled || !security.CheckPassword(admin.PasswordHash, req.Password) {
		return dto.LoginResponse{}, ErrUnauthorized
	}
	secret, err := security.GenerateOpaqueToken()
	if err != nil {
		return dto.LoginResponse{}, err
	}
	now := time.Now()
	expiresAt := now.Add(s.ttl)
	admin.LastLoginAt = &now
	if err := s.store.SaveAdmin(ctx, &admin); err != nil {
		return dto.LoginResponse{}, err
	}
	if err := s.store.CreateAdminToken(ctx, &model.AdminToken{
		AdminUserID: admin.ID,
		TokenHash:   security.HashSecret(secret),
		ExpiresAt:   expiresAt,
		UserAgent:   userAgent,
		ClientIP:    clientIP,
	}); err != nil {
		return dto.LoginResponse{}, err
	}
	return dto.LoginResponse{Token: secret, ExpiresAt: expiresAt, Admin: AdminDTO(admin)}, nil
}

func (s *AdminService) AuthenticateToken(ctx context.Context, token string) (model.AdminUser, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return model.AdminUser{}, ErrUnauthorized
	}
	row, err := s.store.FindAdminToken(ctx, security.HashSecret(token), time.Now())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.AdminUser{}, ErrUnauthorized
		}
		return model.AdminUser{}, err
	}
	admin, err := s.store.GetAdmin(ctx, row.AdminUserID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.AdminUser{}, ErrUnauthorized
		}
		return model.AdminUser{}, err
	}
	if !admin.Enabled {
		return model.AdminUser{}, ErrUnauthorized
	}
	return admin, nil
}

func (s *AdminService) Logout(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return s.store.RevokeAdminToken(ctx, security.HashSecret(token), time.Now())
}

func (s *AdminService) List(ctx context.Context) ([]dto.AdminDTO, error) {
	admins, err := s.store.ListAdmins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.AdminDTO, 0, len(admins))
	for _, admin := range admins {
		out = append(out, AdminDTO(admin))
	}
	return out, nil
}

func (s *AdminService) Create(ctx context.Context, req dto.CreateAdminRequest) (dto.AdminDTO, error) {
	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)
	if username == "" || password == "" {
		return dto.AdminDTO{}, ErrInvalid
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "admin"
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return dto.AdminDTO{}, err
	}
	admin := model.AdminUser{Username: username, PasswordHash: hash, Role: role, Enabled: enabled}
	if err := s.store.CreateAdmin(ctx, &admin); err != nil {
		return dto.AdminDTO{}, err
	}
	return AdminDTO(admin), nil
}

func (s *AdminService) Update(ctx context.Context, id uint, req dto.UpdateAdminRequest) (dto.AdminDTO, error) {
	admin, err := s.store.GetAdmin(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.AdminDTO{}, ErrNotFound
		}
		return dto.AdminDTO{}, err
	}
	if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
		hash, err := security.HashPassword(strings.TrimSpace(*req.Password))
		if err != nil {
			return dto.AdminDTO{}, err
		}
		admin.PasswordHash = hash
	}
	if req.Role != nil && strings.TrimSpace(*req.Role) != "" {
		admin.Role = strings.TrimSpace(*req.Role)
	}
	if req.Enabled != nil {
		if !*req.Enabled && admin.Enabled {
			if err := s.ensureAnotherEnabledAdmin(ctx); err != nil {
				return dto.AdminDTO{}, err
			}
		}
		admin.Enabled = *req.Enabled
	}
	if err := s.store.SaveAdmin(ctx, &admin); err != nil {
		return dto.AdminDTO{}, err
	}
	return AdminDTO(admin), nil
}

func (s *AdminService) Delete(ctx context.Context, id uint) error {
	admin, err := s.store.GetAdmin(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if admin.Enabled {
		if err := s.ensureAnotherEnabledAdmin(ctx); err != nil {
			return err
		}
	}
	return s.store.DeleteAdmin(ctx, id)
}

func (s *AdminService) ensureAnotherEnabledAdmin(ctx context.Context) error {
	count, err := s.store.CountEnabledAdmins(ctx)
	if err != nil {
		return err
	}
	if count <= 1 {
		return ErrForbidden
	}
	return nil
}

func AdminDTO(admin model.AdminUser) dto.AdminDTO {
	return dto.AdminDTO{
		ID:          admin.ID,
		Username:    admin.Username,
		Role:        admin.Role,
		Enabled:     admin.Enabled,
		LastLoginAt: admin.LastLoginAt,
		CreatedAt:   admin.CreatedAt,
		UpdatedAt:   admin.UpdatedAt,
	}
}
