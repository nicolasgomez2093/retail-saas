package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var scripts embed.FS

func Apply(ctx context.Context, db *pgxpool.Pool) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(40912001)"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			name text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
			return err
		}
		names, err := fs.Glob(scripts, "*.sql")
		if err != nil {
			return err
		}
		for _, name := range names {
			statement, err := scripts.ReadFile(name)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(statement)
			checksum := hex.EncodeToString(sum[:])
			var previous string
			err = tx.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE name=$1", name).Scan(&previous)
			switch {
			case err == nil && previous != checksum:
				return fmt.Errorf("cambió una migración aplicada: %s", name)
			case err == nil:
				continue
			case !errors.Is(err, pgx.ErrNoRows):
				return err
			}
			if _, err := tx.Exec(ctx, string(statement)); err != nil {
				return fmt.Errorf("migración %s: %w", name, err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations(name, checksum) VALUES ($1,$2)", name, checksum); err != nil {
				return err
			}
		}
		return nil
	})
}
