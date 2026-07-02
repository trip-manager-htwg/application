package trip

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb"
	dbpool "github.com/trip-manager-htwg/application/backend/trips/internal/database"
)

// ── Record ────────────────────────────────────────────────────────────────────

type tripRecord struct {
	ID               uuid.UUID `db:"id"`
	UserID           uuid.UUID `db:"user_id"`
	TenantID         string    `db:"tenant_id"`
	UserName         string    `db:"user_name"`
	UserEmail        string    `db:"user_email"`
	UserAvatarKey    *string   `db:"user_avatar_key"`
	Title            string    `db:"title"`
	ShortDescription string    `db:"short_description"`
	Description      *string   `db:"description"`
	StartDate        time.Time `db:"start_date"`
	EndDate          time.Time `db:"end_date"`
	Status           string    `db:"status"`
	CreatedAt        time.Time `db:"created_at"`
	UpdatedAt        time.Time `db:"updated_at"`
}

// ── Domain Types ──────────────────────────────────────────────────────────────

type UserSummary struct {
	ID        string
	Name      string
	Email     string
	AvatarKey *string
}

type Trip struct {
	ID               string
	TenantID         string
	Title            string
	ShortDescription string
	Description      string
	StartDate        time.Time
	EndDate          time.Time
	Status           string
	CreatedBy        UserSummary
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ── Errors ────────────────────────────────────────────────────────────────────

var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrInternal     = errors.New("internal error")
	ErrInvalidInput = errors.New("invalid input")
	ErrUnauthorized = errors.New("unauthorized")
)

// ── Mapper ────────────────────────────────────────────────────────────────────

func (r *tripRecord) toTrip() *Trip {
	return &Trip{
		ID:               r.ID.String(),
		TenantID:         r.TenantID,
		Title:            r.Title,
		ShortDescription: r.ShortDescription,
		Description:      fromPtr(r.Description),
		StartDate:        r.StartDate,
		EndDate:          r.EndDate,
		Status:           r.Status,
		CreatedBy: UserSummary{
			ID:        r.UserID.String(),
			Name:      r.UserName,
			Email:     r.UserEmail,
			AvatarKey: r.UserAvatarKey,
		},
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func toRecord(t *Trip) (*tripRecord, error) {
	var id uuid.UUID
	var err error
	if t.ID != "" {
		id, err = uuid.Parse(t.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid trip ID: %w", err)
		}
	}
	userID, err := uuid.Parse(t.CreatedBy.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}
	return &tripRecord{
		ID:               id,
		UserID:           userID,
		TenantID:         t.TenantID,
		UserName:         t.CreatedBy.Name,
		UserEmail:        t.CreatedBy.Email,
		UserAvatarKey:    t.CreatedBy.AvatarKey,
		Title:            t.Title,
		ShortDescription: t.ShortDescription,
		Description:      toPtr(t.Description),
		StartDate:        t.StartDate,
		EndDate:          t.EndDate,
		Status:           t.Status,
		CreatedAt:        t.CreatedAt,
		UpdatedAt:        t.UpdatedAt,
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
	GetByID(ctx context.Context, id string) (*Trip, error)
	Create(ctx context.Context, trip *Trip) (*Trip, error)
	Update(ctx context.Context, trip *Trip) (*Trip, error)
	Delete(ctx context.Context, id, userID string) error
	List(ctx context.Context, userID string, limit, offset int) ([]*Trip, int, error)
	ListRecent(ctx context.Context, limit, offset int) ([]*Trip, int, error)
	Search(ctx context.Context, query string, limit, offset int) ([]*Trip, int, error)
	CountActiveByUser(ctx context.Context, userID string) (int, error)
}

type repositoryImpl struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) Repository {
	return &repositoryImpl{db: db}
}

func (r *repositoryImpl) GetByID(ctx context.Context, id string) (*Trip, error) {
	var result *Trip
	err := tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		var rec tripRecord
		if err := tx.GetContext(ctx, &rec, `SELECT * FROM trips WHERE id = $1`, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toTrip()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) Create(ctx context.Context, trip *Trip) (*Trip, error) {
	trip.TenantID = tenantdb.GetTenantID(ctx)
	rec, err := toRecord(trip)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	var result *Trip
	err = tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		query := `
			INSERT INTO trips (user_id, user_name, user_email, user_avatar_key,
			                   title, short_description, description,
			                   start_date, end_date, status, tenant_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id, created_at, updated_at`
		err := tx.QueryRowContext(ctx, query,
			rec.UserID, rec.UserName, rec.UserEmail, rec.UserAvatarKey,
			rec.Title, rec.ShortDescription, rec.Description,
			rec.StartDate, rec.EndDate, rec.Status, rec.TenantID,
		).Scan(&rec.ID, &rec.CreatedAt, &rec.UpdatedAt)
		if err != nil {
			var pgErr *pq.Error
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrConflict
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toTrip()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) Update(ctx context.Context, trip *Trip) (*Trip, error) {
	rec, err := toRecord(trip)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	var result *Trip
	err = tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		query := `
			UPDATE trips
			SET title = $1, short_description = $2, description = $3,
			    start_date = $4, end_date = $5, status = $6, updated_at = NOW()
			WHERE id = $7 AND user_id = $8
			RETURNING updated_at`
		err := tx.QueryRowContext(ctx, query,
			rec.Title, rec.ShortDescription, rec.Description,
			rec.StartDate, rec.EndDate, rec.Status,
			rec.ID, rec.UserID,
		).Scan(&rec.UpdatedAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toTrip()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) Delete(ctx context.Context, id, userID string) error {
	return tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx,
			`DELETE FROM trips WHERE id = $1 AND user_id = $2`, id, userID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		if rows == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *repositoryImpl) List(ctx context.Context, userID string, limit, offset int) ([]*Trip, int, error) {
	var trips []*Trip
	var total int
	err := tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		var results []struct {
			tripRecord
			TotalCount int `db:"total_count"`
		}
		query := `
			SELECT *, COUNT(*) OVER() as total_count
			FROM trips
			WHERE user_id = $1
			ORDER BY start_date ASC, created_at ASC
			LIMIT $2 OFFSET $3`
		if err := tx.SelectContext(ctx, &results, query, userID, limit, offset); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		if len(results) == 0 {
			trips = []*Trip{}
			return nil
		}
		total = results[0].TotalCount
		trips = make([]*Trip, len(results))
		for i, res := range results {
			trips[i] = res.tripRecord.toTrip()
		}
		return nil
	})
	return trips, total, err
}

func (r *repositoryImpl) ListRecent(ctx context.Context, limit, offset int) ([]*Trip, int, error) {
	var trips []*Trip
	var total int
	err := tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		var results []tripRecord
		if err := tx.SelectContext(ctx, &results, `
			SELECT * FROM trips
			ORDER BY created_at DESC
			LIMIT $1 OFFSET $2`, limit, offset); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM trips`).Scan(&total); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		if len(results) == 0 {
			trips = []*Trip{}
			return nil
		}
		trips = make([]*Trip, len(results))
		for i, res := range results {
			trips[i] = res.toTrip()
		}
		return nil
	})
	return trips, total, err
}

func (r *repositoryImpl) Search(ctx context.Context, query string, limit, offset int) ([]*Trip, int, error) {
	var trips []*Trip
	var total int
	err := tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		var results []struct {
			tripRecord
			TotalCount int `db:"total_count"`
		}
		sqlQuery := `
			SELECT *, COUNT(*) OVER() as total_count
			FROM trips
			WHERE title ILIKE $1 OR short_description ILIKE $1
			ORDER BY start_date ASC, created_at ASC
			LIMIT $2 OFFSET $3`
		if err := tx.SelectContext(ctx, &results, sqlQuery, "%"+query+"%", limit, offset); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		if len(results) == 0 {
			trips = []*Trip{}
			return nil
		}
		total = results[0].TotalCount
		trips = make([]*Trip, len(results))
		for i, res := range results {
			trips[i] = res.tripRecord.toTrip()
		}
		return nil
	})
	return trips, total, err
}

func (r *repositoryImpl) CountActiveByUser(ctx context.Context, userID string) (int, error) {
	var count int
	err := tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM trips WHERE user_id = $1 AND status IN ('planned', 'ongoing')`,
			userID,
		).Scan(&count)
	})
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInternal, err)
	}
	return count, nil
}

func (r *repositoryImpl) getDB(ctx context.Context) *sqlx.DB {
	return dbpool.GetDB(ctx, r.db)
}
