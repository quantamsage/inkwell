package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"markdown-editor/internal/documents"
)

// DocumentStore is the SQLite implementation of documents.Store.
type DocumentStore struct {
	db *sql.DB
}

// NewDocumentStore returns a store backed by db.
func NewDocumentStore(db *sql.DB) *DocumentStore { return &DocumentStore{db: db} }

var _ documents.Store = (*DocumentStore)(nil)

func toMillis(t time.Time) int64 { return t.UnixMilli() }
func fromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

// Insert stores a new document.
func (s *DocumentStore) Insert(ctx context.Context, d documents.Document) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO documents (id, title, content, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.Title, d.Content, d.Version, toMillis(d.CreatedAt), toMillis(d.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

// Get loads one document.
func (s *DocumentStore) Get(ctx context.Context, id string) (documents.Document, error) {
	var d documents.Document
	var created, updated int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, content, version, created_at, updated_at FROM documents WHERE id = ?`, id).
		Scan(&d.ID, &d.Title, &d.Content, &d.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return documents.Document{}, documents.ErrNotFound
	}
	if err != nil {
		return documents.Document{}, fmt.Errorf("get document: %w", err)
	}
	d.CreatedAt, d.UpdatedAt = fromMillis(created), fromMillis(updated)
	return d, nil
}

// escapeLike escapes LIKE wildcards so user input is matched literally.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// snippetLen is the number of characters of content returned in listings.
const snippetLen = 160

// List returns document summaries. The ORDER BY clause is chosen from a fixed
// whitelist; user input only ever reaches the query as bound parameters.
func (s *DocumentStore) List(ctx context.Context, opts documents.ListOptions) ([]documents.Summary, error) {
	var order string
	switch opts.Sort {
	case documents.SortTitle:
		order = "title COLLATE NOCASE"
	case documents.SortCreated:
		order = "created_at"
	default:
		order = "updated_at"
	}
	if opts.Descending {
		order += " DESC"
	} else {
		order += " ASC"
	}
	order += ", id ASC"

	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}

	var (
		where string
		args  []any
	)
	if opts.Query != "" {
		pattern := "%" + escapeLike(opts.Query) + "%"
		where = `WHERE title LIKE ? ESCAPE '\' OR content LIKE ? ESCAPE '\'`
		args = append(args, pattern, pattern)
	}
	args = append(args, limit)

	query := `SELECT id, title, substr(content, 1, ` + fmt.Sprint(snippetLen) + `), version, created_at, updated_at
		FROM documents ` + where + ` ORDER BY ` + order + ` LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()

	out := []documents.Summary{}
	for rows.Next() {
		var sm documents.Summary
		var created, updated int64
		if err := rows.Scan(&sm.ID, &sm.Title, &sm.Snippet, &sm.Version, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		sm.Snippet = strings.Join(strings.Fields(sm.Snippet), " ")
		sm.CreatedAt, sm.UpdatedAt = fromMillis(created), fromMillis(updated)
		out = append(out, sm)
	}
	return out, rows.Err()
}

// UpdateIfVersion performs a compare-and-swap update on the version column.
func (s *DocumentStore) UpdateIfVersion(ctx context.Context, id string, expectedVersion int64, title, content string, updatedAt time.Time) (documents.Document, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return documents.Document{}, err
	}
	defer tx.Rollback()

	// max() keeps updated_at monotonic even if the clock moves backwards.
	res, err := tx.ExecContext(ctx,
		`UPDATE documents
		    SET title = ?, content = ?, version = version + 1, updated_at = max(?, created_at)
		  WHERE id = ? AND version = ?`,
		title, content, toMillis(updatedAt), id, expectedVersion)
	if err != nil {
		return documents.Document{}, fmt.Errorf("update document: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return documents.Document{}, err
	}
	if n == 0 {
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM documents WHERE id = ?`, id).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return documents.Document{}, documents.ErrNotFound
		}
		if err != nil {
			return documents.Document{}, err
		}
		return documents.Document{}, documents.ErrConflict
	}

	var d documents.Document
	var created, updated int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id, title, content, version, created_at, updated_at FROM documents WHERE id = ?`, id).
		Scan(&d.ID, &d.Title, &d.Content, &d.Version, &created, &updated); err != nil {
		return documents.Document{}, err
	}
	if err := tx.Commit(); err != nil {
		return documents.Document{}, err
	}
	d.CreatedAt, d.UpdatedAt = fromMillis(created), fromMillis(updated)
	return d, nil
}

// Delete removes a document.
func (s *DocumentStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return documents.ErrNotFound
	}
	return nil
}
