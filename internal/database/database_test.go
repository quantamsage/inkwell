package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"markdown-editor/internal/documents"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func doc(id, title, content string, created time.Time) documents.Document {
	return documents.Document{ID: id, Title: title, Content: content, Version: 1, CreatedAt: created, UpdatedAt: created}
}

func id(n int) string { return strings.Repeat("0", 31) + string(rune('0'+n)) }

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 applied migrations, got %d", n)
	}
	for _, table := range []string{"documents", "preferences"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

func TestMigrateRollsBackFailedMigration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	fsys := fstest.MapFS{
		"0001_ok.sql":  {Data: []byte(`CREATE TABLE a (x INTEGER);`)},
		"0002_bad.sql": {Data: []byte(`CREATE TABLE b (y INTEGER); THIS IS NOT SQL;`)},
	}
	if err := migrate(context.Background(), db, fsys); err == nil {
		t.Fatal("expected error from bad migration")
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n)
	if n != 1 {
		t.Fatalf("expected only first migration recorded, got %d", n)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='b'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("table b should have been rolled back (n=%d, err=%v)", n, err)
	}
}

func TestSchemaConstraints(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UnixMilli()
	cases := map[string][]any{
		"bad id":          {"nothex", "t", "", 1, now, now},
		"empty title":     {id(1), "", "", 1, now, now},
		"zero version":    {id(2), "t", "", 0, now, now},
		"updated<created": {id(3), "t", "", 1, now, now - 1},
	}
	for name, args := range cases {
		_, err := db.Exec(`INSERT INTO documents (id, title, content, version, created_at, updated_at) VALUES (?,?,?,?,?,?)`, args...)
		if err == nil {
			t.Errorf("%s: expected constraint violation", name)
		}
	}
}

func TestDocumentStoreCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewDocumentStore(openTestDB(t))
	created := time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.UTC)

	d := doc(id(1), "Hello", "# Hello\nworld", created)
	if err := s.Insert(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := s.Insert(ctx, d); err == nil {
		t.Fatal("duplicate insert should fail")
	}
	got, err := s.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Hello" || got.Content != d.Content || got.Version != 1 || !got.CreatedAt.Equal(created) {
		t.Fatalf("unexpected doc: %+v", got)
	}

	later := created.Add(time.Minute)
	upd, err := s.UpdateIfVersion(ctx, d.ID, 1, "Hello 2", "new", later)
	if err != nil {
		t.Fatal(err)
	}
	if upd.Version != 2 || upd.Title != "Hello 2" || upd.Content != "new" || !upd.UpdatedAt.Equal(later) {
		t.Fatalf("unexpected update: %+v", upd)
	}

	// Stale version is rejected and leaves the row untouched.
	if _, err := s.UpdateIfVersion(ctx, d.ID, 1, "stale", "stale", later); !errors.Is(err, documents.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if got, _ := s.Get(ctx, d.ID); got.Content != "new" {
		t.Fatalf("stale write overwrote content: %q", got.Content)
	}
	if _, err := s.UpdateIfVersion(ctx, id(9), 1, "x", "x", later); !errors.Is(err, documents.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	if err := s.Delete(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, d.ID); !errors.Is(err, documents.ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
	if err := s.Delete(ctx, d.ID); !errors.Is(err, documents.ErrNotFound) {
		t.Fatalf("expected not found on second delete, got %v", err)
	}
}

func TestDocumentStoreListSearchAndSort(t *testing.T) {
	ctx := context.Background()
	s := NewDocumentStore(openTestDB(t))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	docs := []documents.Document{
		doc(id(1), "banana", "yellow fruit", base),
		doc(id(2), "Apple", "red fruit 100% tasty", base.Add(time.Hour)),
		doc(id(3), "cherry", "small_red", base.Add(2*time.Hour)),
	}
	for _, d := range docs {
		if err := s.Insert(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	// Touch banana so it becomes most recently updated.
	if _, err := s.UpdateIfVersion(ctx, id(1), 1, "banana", "yellow fruit!", base.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	titles := func(list []documents.Summary) string {
		var out []string
		for _, sm := range list {
			out = append(out, sm.Title)
		}
		return strings.Join(out, ",")
	}

	tests := []struct {
		opts documents.ListOptions
		want string
	}{
		{documents.ListOptions{Sort: documents.SortTitle}, "Apple,banana,cherry"},
		{documents.ListOptions{Sort: documents.SortTitle, Descending: true}, "cherry,banana,Apple"},
		{documents.ListOptions{Sort: documents.SortCreated, Descending: true}, "cherry,Apple,banana"},
		{documents.ListOptions{Sort: documents.SortUpdated, Descending: true}, "banana,cherry,Apple"},
		{documents.ListOptions{Sort: documents.SortUpdated, Descending: true, Limit: 2}, "banana,cherry"},
		{documents.ListOptions{Sort: documents.SortTitle, Query: "FRUIT"}, "Apple,banana"},
		{documents.ListOptions{Sort: documents.SortTitle, Query: "cher"}, "cherry"},
		// LIKE wildcards in the query are matched literally.
		{documents.ListOptions{Sort: documents.SortTitle, Query: "%"}, "Apple"},
		{documents.ListOptions{Sort: documents.SortTitle, Query: "_"}, "cherry"},
		{documents.ListOptions{Sort: documents.SortTitle, Query: "nomatch"}, ""},
	}
	for _, tc := range tests {
		got, err := s.List(ctx, tc.opts)
		if err != nil {
			t.Fatal(err)
		}
		if titles(got) != tc.want {
			t.Errorf("List(%+v) = %q, want %q", tc.opts, titles(got), tc.want)
		}
	}
}

func TestListSnippetIsTruncatedAndCollapsed(t *testing.T) {
	ctx := context.Background()
	s := NewDocumentStore(openTestDB(t))
	content := "line one\n\nline   two " + strings.Repeat("x", 500)
	if err := s.Insert(ctx, doc(id(1), "t", content, time.Now())); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, documents.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !strings.HasPrefix(list[0].Snippet, "line one line two x") || len([]rune(list[0].Snippet)) > snippetLen {
		t.Fatalf("unexpected snippet %q", list[0].Snippet)
	}
}

func TestPreferenceStore(t *testing.T) {
	ctx := context.Background()
	s := NewPreferenceStore(openTestDB(t))
	m, err := s.LoadAll(ctx)
	if err != nil || len(m) != 0 {
		t.Fatalf("expected empty prefs, got %v %v", m, err)
	}
	if err := s.SaveAll(ctx, map[string]string{"theme": "dark", "fontSize": "16"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAll(ctx, map[string]string{"theme": "light"}); err != nil {
		t.Fatal(err)
	}
	m, err = s.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if m["theme"] != "light" || m["fontSize"] != "16" {
		t.Fatalf("unexpected prefs %v", m)
	}
}
