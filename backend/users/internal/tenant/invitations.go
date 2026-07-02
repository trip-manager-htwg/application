package tenant

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb"
)

type Invitation struct {
	ID         string
	TenantID   string
	Email      string
	Role       string
	Token      string
	CreatedBy  string
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	CreatedAt  time.Time
}

type invitationRecord struct {
	ID         string     `db:"id"`
	TenantID   string     `db:"tenant_id"`
	Email      string     `db:"email"`
	Role       string     `db:"role"`
	Token      string     `db:"token"`
	CreatedBy  string     `db:"created_by"`
	ExpiresAt  time.Time  `db:"expires_at"`
	AcceptedAt *time.Time `db:"accepted_at"`
	CreatedAt  time.Time  `db:"created_at"`
}

type InvitationRepository interface {
	Create(ctx context.Context, tenantID, email, role, createdBy string) (*Invitation, error)
	GetByToken(ctx context.Context, token string) (*Invitation, error)
	Accept(ctx context.Context, token string) error
	ListByTenant(ctx context.Context) ([]*Invitation, error)
	Delete(ctx context.Context, id string) error
}

type invitationRepo struct {
	db *sqlx.DB
}

func NewInvitationRepository(db *sqlx.DB) InvitationRepository {
	return &invitationRepo{db: db}
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (r *invitationRepo) Create(ctx context.Context, tenantID, email, role, createdBy string) (*Invitation, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	id := fmt.Sprintf("inv-%s", token[:8])
	expiresAt := time.Now().Add(7 * 24 * time.Hour) // 7 Tage

	var rec invitationRecord
	err = tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		return tx.QueryRowContext(ctx, `
            INSERT INTO tenant_invitations (id, tenant_id, email, role, token, created_by, expires_at)
            VALUES ($1, $2, $3, $4, $5, $6, $7)
            RETURNING id, tenant_id, email, role, token, created_by, expires_at, accepted_at, created_at`,
			id, tenantID, email, role, token, createdBy, expiresAt,
		).Scan(&rec.ID, &rec.TenantID, &rec.Email, &rec.Role, &rec.Token,
			&rec.CreatedBy, &rec.ExpiresAt, &rec.AcceptedAt, &rec.CreatedAt)
	})
	if err != nil {
		return nil, err
	}

	return &Invitation{
		ID: rec.ID, TenantID: rec.TenantID, Email: rec.Email,
		Role: rec.Role, Token: rec.Token, CreatedBy: rec.CreatedBy,
		ExpiresAt: rec.ExpiresAt, CreatedAt: rec.CreatedAt,
	}, nil
}

func (r *invitationRepo) GetByToken(ctx context.Context, token string) (*Invitation, error) {
	var rec invitationRecord
	// Kein RLS hier – Token-Lookup braucht keinen Tenant-Context
	err := r.db.GetContext(ctx, &rec,
		`SELECT * FROM tenant_invitations WHERE token = $1 AND accepted_at IS NULL AND expires_at > NOW()`,
		token,
	)
	if err != nil {
		return nil, err
	}
	return &Invitation{
		ID: rec.ID, TenantID: rec.TenantID, Email: rec.Email,
		Role: rec.Role, Token: rec.Token, CreatedBy: rec.CreatedBy,
		ExpiresAt: rec.ExpiresAt, CreatedAt: rec.CreatedAt,
	}, nil
}

func (r *invitationRepo) Accept(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tenant_invitations SET accepted_at = NOW() WHERE token = $1`,
		token,
	)
	return err
}

func (r *invitationRepo) ListByTenant(ctx context.Context) ([]*Invitation, error) {
	var recs []invitationRecord
	err := tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		return tx.SelectContext(ctx, &recs,
			`SELECT * FROM tenant_invitations WHERE accepted_at IS NULL AND expires_at > NOW() ORDER BY created_at DESC`,
		)
	})
	if err != nil {
		return nil, err
	}
	var result []*Invitation
	for _, rec := range recs {
		r := rec
		result = append(result, &Invitation{
			ID: r.ID, TenantID: r.TenantID, Email: r.Email,
			Role: r.Role, Token: r.Token, ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt,
		})
	}
	return result, nil
}

func (r *invitationRepo) Delete(ctx context.Context, id string) error {
	return tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM tenant_invitations WHERE id = $1`, id)
		return err
	})
}
