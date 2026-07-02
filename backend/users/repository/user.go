package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb"
)

// ── Record ────────────────────────────────────────────────────────────────────

type userRecord struct {
	ID          uuid.UUID `db:"id"`
	Email       string    `db:"email"`
	Name        string    `db:"name"`
	Bio         *string   `db:"bio"`
	AvatarKey   *string   `db:"avatar_key"`
	FirebaseUID string    `db:"firebase_uid"`
	TenantID    string    `db:"tenant_id"`
	Role        string    `db:"role"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// ── Domain Type ───────────────────────────────────────────────────────────────

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

// ── Errors ────────────────────────────────────────────────────────────────────

var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrInternal     = errors.New("internal error")
	ErrInvalidInput = errors.New("invalid input")
)

// ── Mapper ────────────────────────────────────────────────────────────────────

func (r *userRecord) toUser() *User {
	return &User{
		ID:          r.ID.String(),
		Email:       r.Email,
		Name:        r.Name,
		Bio:         fromPtr(r.Bio),
		AvatarKey:   fromPtr(r.AvatarKey),
		FirebaseUID: r.FirebaseUID,
		TenantID:    r.TenantID,
		Role:        r.Role,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func toRecord(u *User) (*userRecord, error) {
	var id uuid.UUID
	var err error
	if u.ID != "" {
		id, err = uuid.Parse(u.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid user ID: %w", err)
		}
	}
	return &userRecord{
		ID:          id,
		Email:       u.Email,
		Name:        u.Name,
		Bio:         toPtr(u.Bio),
		AvatarKey:   toPtr(u.AvatarKey),
		FirebaseUID: u.FirebaseUID,
		TenantID:    u.TenantID,
		Role:        u.Role,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}, nil
}

func fromPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ── Repository ────────────────────────────────────────────────────────────────

type Repository interface {
	GetByID(ctx context.Context, id string) (*User, error)
	GetByFirebaseUID(ctx context.Context, uid string) (*User, error)
	Create(ctx context.Context, user *User) (*User, error)
	Update(ctx context.Context, user *User) (*User, error)
	ListByTenant(ctx context.Context) ([]*User, error)
	RemoveFromTenant(ctx context.Context, userID string) error
	ResetTenantUsers(ctx context.Context, tenantID string) error
}

type repositoryImpl struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) Repository {
	return &repositoryImpl{db: db}
}

func (r *repositoryImpl) GetByID(ctx context.Context, id string) (*User, error) {
	var result *User
	err := tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		var rec userRecord
		query := `SELECT id, email, name, bio, avatar_key, firebase_uid, tenant_id, role, created_at, updated_at
		          FROM users WHERE id = $1`
		if err := tx.GetContext(ctx, &rec, query, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toUser()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) GetByFirebaseUID(ctx context.Context, uid string) (*User, error) {
	var result *User
	log.Printf("[Repository] GetByFirebaseUID: %s", uid)
	err := tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		var rec userRecord
		query := `SELECT id, email, name, bio, avatar_key, firebase_uid, tenant_id, role, created_at, updated_at
		          FROM users WHERE firebase_uid = $1`
		if err := tx.GetContext(ctx, &rec, query, uid); err != nil {
			log.Printf("[Repository] GetByFirebaseUID error: %v", err)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		log.Printf("[Repository] GetByFirebaseUID found: %s", rec.ID)
		result = rec.toUser()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) Create(ctx context.Context, user *User) (*User, error) {
	if user.TenantID == "" {
		user.TenantID = tenantdb.GetTenantID(ctx)
	}
	if user.Role == "" {
		user.Role = "tenant_member"
	}
	rec, err := toRecord(user)
	if err != nil {
		return nil, err
	}
	var result *User
	err = tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		query := `INSERT INTO users (email, name, bio, firebase_uid, tenant_id, role, created_at, updated_at)
		          VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		          RETURNING id, email, name, bio, avatar_key, firebase_uid, tenant_id, role, created_at, updated_at`
		err := tx.QueryRowContext(ctx, query,
			rec.Email, rec.Name, rec.Bio, rec.FirebaseUID, rec.TenantID, rec.Role,
		).Scan(&rec.ID, &rec.Email, &rec.Name, &rec.Bio, &rec.AvatarKey,
			&rec.FirebaseUID, &rec.TenantID, &rec.Role, &rec.CreatedAt, &rec.UpdatedAt)
		if err != nil {
			var pgErr *pq.Error
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrConflict
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toUser()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) Update(ctx context.Context, user *User) (*User, error) {
	rec, err := toRecord(user)
	if err != nil {
		return nil, err
	}
	var result *User
	err = tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		query := `UPDATE users 
                  SET email = $1, name = $2, bio = $3, avatar_key = $4, 
                      tenant_id = $5, role = $6, updated_at = NOW()
                  WHERE id = $7
                  RETURNING id, email, name, bio, avatar_key, firebase_uid, tenant_id, role, created_at, updated_at`
		err := tx.QueryRowContext(ctx, query,
			rec.Email, rec.Name, rec.Bio, rec.AvatarKey,
			rec.TenantID, rec.Role, rec.ID,
		).Scan(&rec.ID, &rec.Email, &rec.Name, &rec.Bio, &rec.AvatarKey,
			&rec.FirebaseUID, &rec.TenantID, &rec.Role, &rec.CreatedAt, &rec.UpdatedAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toUser()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) ListByTenant(ctx context.Context) ([]*User, error) {
	var result []*User
	err := tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		var recs []userRecord
		query := `SELECT id, email, name, bio, avatar_key, firebase_uid, tenant_id, role, created_at, updated_at
                  FROM users ORDER BY created_at ASC`
		if err := tx.SelectContext(ctx, &recs, query); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		for _, rec := range recs {
			r := rec
			result = append(result, r.toUser())
		}
		return nil
	})
	return result, err
}

func (r *repositoryImpl) RemoveFromTenant(ctx context.Context, userID string) error {
	return tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE users SET tenant_id = 'default', role = 'tenant_member' WHERE id = $1`,
			userID,
		)
		return err
	})
}

func (r *repositoryImpl) ResetTenantUsers(ctx context.Context, tenantID string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE users SET tenant_id = 'default', role = 'tenant_member'
        WHERE tenant_id = $1
    `, tenantID)
	return err
}
