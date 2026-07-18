package database

import (
	"context"

	"github.com/jmoiron/sqlx"
)

type contextKey string

const dbKey contextKey = "db"

func WithDB(ctx context.Context, db *sqlx.DB) context.Context {
	return context.WithValue(ctx, dbKey, db)
}

func GetDB(ctx context.Context, defaultDB *sqlx.DB) *sqlx.DB {
	if db, ok := ctx.Value(dbKey).(*sqlx.DB); ok && db != nil {
		return db
	}
	return defaultDB
}
