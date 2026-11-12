package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"markdown-editor/internal/preferences"
)

// PreferenceStore is the SQLite implementation of preferences.Store.
type PreferenceStore struct {
	db *sql.DB
}

// NewPreferenceStore returns a store backed by db.
func NewPreferenceStore(db *sql.DB) *PreferenceStore { return &PreferenceStore{db: db} }

var _ preferences.Store = (*PreferenceStore)(nil)

// LoadAll returns every stored preference.
func (s *PreferenceStore) LoadAll(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM preferences`)
	if err != nil {
		return nil, fmt.Errorf("load preferences: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SaveAll upserts the given values in a single transaction.
func (s *PreferenceStore) SaveAll(ctx context.Context, values map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO preferences (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
		 WHERE preferences.value <> excluded.value`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UnixMilli()
	for k, v := range values {
		if _, err := stmt.ExecContext(ctx, k, v, now); err != nil {
			return fmt.Errorf("save preference %q: %w", k, err)
		}
	}
	return tx.Commit()
}
