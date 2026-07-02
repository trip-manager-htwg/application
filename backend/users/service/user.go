package service

import (
	"context"
	"errors"
	"fmt"
	"tenantdb"
	"time"

	"github.com/trip-manager-htwg/application/backend/shared/firebaseclient"
	"github.com/trip-manager-htwg/application/backend/users/repository"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type User struct {
	ID          string
	Email       string
	Name        string
	Bio         string
	AvatarKey   string
	FirebaseUID string
	TenantID    string
	Role        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProvisionInput struct {
	FirebaseUID string
	Email       string
	Name        string
	TenantID    string // optional, default "default"
	Role        string // optional, default "tenant_member"
}

type UpdateInput struct {
	ID        string
	Name      *string
	Email     *string
	Bio       *string
	AvatarKey *string
}

// ── Interface ─────────────────────────────────────────────────────────────────

type Service interface {
	GetByID(ctx context.Context, id string) (*User, error)
	GetByFirebaseUID(ctx context.Context, firebaseUID string) (*User, error)
	Provision(ctx context.Context, input ProvisionInput) (*User, bool, error)
	ProvisionWithTenant(ctx context.Context, input ProvisionInput) (*User, bool, error)
	Update(ctx context.Context, input UpdateInput) (*User, error)
}

// ── Implementation ────────────────────────────────────────────────────────────

type serviceImpl struct {
	repo         repository.Repository
	firebaseAuth *firebaseclient.Client
}

func NewService(repo repository.Repository, firebaseAuth *firebaseclient.Client) Service {
	return &serviceImpl{repo: repo, firebaseAuth: firebaseAuth}
}

func (s *serviceImpl) GetByID(ctx context.Context, id string) (*User, error) {
	u, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return repoToService(u), nil
}

func (s *serviceImpl) GetByFirebaseUID(ctx context.Context, firebaseUID string) (*User, error) {
	u, err := s.repo.GetByFirebaseUID(ctx, firebaseUID)
	if err != nil {
		return nil, err
	}
	return repoToService(u), nil
}

func (s *serviceImpl) Provision(ctx context.Context, input ProvisionInput) (*User, bool, error) {
	tenantID := input.TenantID
	if tenantID == "" {
		tenantID = tenantdb.GetTenantID(ctx)
	}
	if tenantID == "" {
		tenantID = "default"
	}

	// Lookup immer mit dem tatsächlichen tenant_id des Users versuchen
	// dann fallback auf default falls nicht gefunden
	lookupCtx := tenantdb.WithTenantID(ctx, tenantID)
	existing, err := s.repo.GetByFirebaseUID(lookupCtx, input.FirebaseUID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, false, fmt.Errorf("checking existing user: %w", err)
	}

	// Falls nicht gefunden, mit default versuchen
	if existing == nil {
		lookupCtx = tenantdb.WithTenantID(ctx, "default")
		existing, err = s.repo.GetByFirebaseUID(lookupCtx, input.FirebaseUID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return nil, false, fmt.Errorf("checking existing user (default): %w", err)
		}
	}

	if existing != nil {
		return repoToService(existing), false, nil
	}

	// Neuer User
	ctx = tenantdb.WithTenantID(ctx, tenantID)
	role := input.Role
	if role == "" {
		role = "tenant_member"
	}

	created, err := s.repo.Create(ctx, &repository.User{
		Email:       input.Email,
		Name:        input.Name,
		FirebaseUID: input.FirebaseUID,
		TenantID:    tenantID,
		Role:        role,
	})
	if err != nil {
		return nil, false, err
	}

	if s.firebaseAuth != nil {
		claims := map[string]interface{}{
			"tenant_id": tenantID,
			"role":      role,
		}
		if err := s.firebaseAuth.SetCustomClaims(ctx, input.FirebaseUID, claims); err != nil {
			fmt.Printf("warn: failed to set firebase custom claims: %v\n", err)
		}
	}

	return repoToService(created), true, nil
}

func (s *serviceImpl) Update(ctx context.Context, input UpdateInput) (*User, error) {
	existing, err := s.repo.GetByFirebaseUID(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		existing.Name = *input.Name
	}
	if input.Email != nil {
		existing.Email = *input.Email
	}
	if input.Bio != nil {
		existing.Bio = *input.Bio
	}
	if input.AvatarKey != nil {
		existing.AvatarKey = *input.AvatarKey
	}
	existing.UpdatedAt = time.Now()

	updated, err := s.repo.Update(ctx, existing)
	if err != nil {
		return nil, err
	}
	return repoToService(updated), nil
}

func (s *serviceImpl) ProvisionWithTenant(ctx context.Context, input ProvisionInput) (*User, bool, error) {
	// User mit default Context suchen
	defaultCtx := tenantdb.WithTenantID(ctx, "default")
	existing, err := s.repo.GetByFirebaseUID(defaultCtx, input.FirebaseUID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, false, fmt.Errorf("checking existing user: %w", err)
	}

	if existing != nil {
		// update Tenant & Role
		existing.TenantID = input.TenantID
		existing.Role = input.Role

		// Update (default) Context
		updated, err := s.repo.Update(defaultCtx, existing)
		if err != nil {
			return nil, false, fmt.Errorf("updating user tenant: %w", err)
		}

		// Firebase Claims
		if s.firebaseAuth != nil {
			claims := map[string]interface{}{
				"tenant_id": input.TenantID,
				"role":      input.Role,
			}
			if err := s.firebaseAuth.SetCustomClaims(ctx, input.FirebaseUID, claims); err != nil {
				fmt.Printf("warn: failed to set firebase custom claims: %v\n", err)
			}
		}
		return repoToService(updated), false, nil
	}
	// New User
	created, err := s.repo.Create(ctx, &repository.User{
		Email:       input.Email,
		Name:        input.Name,
		FirebaseUID: input.FirebaseUID,
		TenantID:    input.TenantID,
		Role:        input.Role,
	})
	if err != nil {
		return nil, false, err
	}

	if s.firebaseAuth != nil {
		claims := map[string]interface{}{
			"tenant_id": input.TenantID,
			"role":      input.Role,
		}
		if err := s.firebaseAuth.SetCustomClaims(ctx, input.FirebaseUID, claims); err != nil {
			fmt.Printf("warn: failed to set firebase custom claims: %v\n", err)
		}
	}

	return repoToService(created), true, nil
}

// ── Mapper ────────────────────────────────────────────────────────────────────

func repoToService(u *repository.User) *User {
	return &User{
		ID:          u.ID,
		Email:       u.Email,
		Name:        u.Name,
		Bio:         u.Bio,
		AvatarKey:   u.AvatarKey,
		FirebaseUID: u.FirebaseUID,
		TenantID:    u.TenantID,
		Role:        u.Role,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}
