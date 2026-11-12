package documents_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"markdown-editor/internal/database"
	"markdown-editor/internal/documents"
)

func newService(t *testing.T) *documents.Service {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "docs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := documents.NewService(database.NewDocumentStore(db), 1024)
	clock := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	svc.SetClock(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(time.Second)
		return clock
	})
	return svc
}

func TestCreateDefaultsAndValidation(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)

	d, err := svc.Create(ctx, "   ", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != documents.DefaultTitle || d.Version != 1 || !documents.ValidID(d.ID) {
		t.Fatalf("unexpected doc %+v", d)
	}
	if !d.CreatedAt.Equal(d.UpdatedAt) {
		t.Fatal("created and updated should match on creation")
	}

	d, err = svc.Create(ctx, "  My\n  Title\t ", "body")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "My Title" {
		t.Fatalf("title not normalized: %q", d.Title)
	}

	var verr *documents.ValidationError
	if _, err := svc.Create(ctx, strings.Repeat("a", documents.MaxTitleLength+1), ""); !errors.As(err, &verr) {
		t.Fatalf("expected validation error for long title, got %v", err)
	}
	if _, err := svc.Create(ctx, "t", strings.Repeat("a", 1025)); !errors.Is(err, documents.ErrContentTooLarge) {
		t.Fatalf("expected too large, got %v", err)
	}
	if _, err := svc.Create(ctx, "t", "bad \xff utf8"); !errors.As(err, &verr) {
		t.Fatalf("expected utf8 validation error, got %v", err)
	}
}

func TestGetValidatesID(t *testing.T) {
	svc := newService(t)
	for _, id := range []string{"", "abc", "../../etc/passwd", strings.Repeat("Z", 32)} {
		if _, err := svc.Get(context.Background(), id); !errors.Is(err, documents.ErrInvalidID) {
			t.Errorf("Get(%q) = %v, want ErrInvalidID", id, err)
		}
	}
	if _, err := svc.Get(context.Background(), strings.Repeat("a", 32)); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestSaveUpdatesAndBumpsVersion(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	d, _ := svc.Create(ctx, "Draft", "one")

	res, err := svc.Save(ctx, d.ID, 1, "Draft", "two")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Document.Version != 2 || res.Document.Content != "two" {
		t.Fatalf("unexpected result %+v", res)
	}
	if !res.Document.UpdatedAt.After(d.UpdatedAt) {
		t.Fatal("updated_at should advance")
	}
	if !res.Document.CreatedAt.Equal(d.CreatedAt) {
		t.Fatal("created_at must not change")
	}
}

// Autosave: an unchanged payload must not write to the database.
func TestSaveUnchangedSkipsWrite(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	d, _ := svc.Create(ctx, "Same", "content")

	res, err := svc.Save(ctx, d.ID, 1, "Same", "content")
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("expected no change")
	}
	got, _ := svc.Get(ctx, d.ID)
	if got.Version != 1 || !got.UpdatedAt.Equal(d.UpdatedAt) {
		t.Fatalf("unchanged save wrote to database: %+v", got)
	}
}

// Autosave: a save based on an old version must never overwrite newer content.
func TestSaveRejectsStaleVersion(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	d, _ := svc.Create(ctx, "Doc", "v1")

	if _, err := svc.Save(ctx, d.ID, 1, "Doc", "v2 newer"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Save(ctx, d.ID, 1, "Doc", "v1 older edit")
	if !errors.Is(err, documents.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if res.Document.Content != "v2 newer" || res.Document.Version != 2 {
		t.Fatalf("conflict should return current document, got %+v", res.Document)
	}
	got, _ := svc.Get(ctx, d.ID)
	if got.Content != "v2 newer" {
		t.Fatalf("stale save overwrote content: %q", got.Content)
	}
}

// Autosave: concurrent saves from the same base version — exactly one wins.
func TestConcurrentSavesOnlyOneWins(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	d, _ := svc.Create(ctx, "Doc", "base")

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Save(ctx, d.ID, 1, "Doc", "edit "+string(rune('a'+i)))
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, documents.ErrConflict):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly one winning save, got %d", wins)
	}
	got, _ := svc.Get(ctx, d.ID)
	if got.Version != 2 {
		t.Fatalf("expected version 2, got %d", got.Version)
	}
}

func TestSaveValidation(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	d, _ := svc.Create(ctx, "Doc", "")
	var verr *documents.ValidationError
	if _, err := svc.Save(ctx, d.ID, 0, "Doc", "x"); !errors.As(err, &verr) {
		t.Fatalf("expected version validation error, got %v", err)
	}
	if _, err := svc.Save(ctx, "bad", 1, "Doc", "x"); !errors.Is(err, documents.ErrInvalidID) {
		t.Fatalf("expected invalid id, got %v", err)
	}
	if _, err := svc.Save(ctx, strings.Repeat("b", 32), 1, "Doc", "x"); !errors.Is(err, documents.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	if _, err := svc.Save(ctx, d.ID, 1, "Doc", strings.Repeat("x", 2000)); !errors.Is(err, documents.ErrContentTooLarge) {
		t.Fatalf("expected too large, got %v", err)
	}
}

func TestRename(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	d, _ := svc.Create(ctx, "Old", "keep me")

	r, err := svc.Rename(ctx, d.ID, "  New name ")
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "New name" || r.Content != "keep me" || r.Version != 2 {
		t.Fatalf("unexpected rename result %+v", r)
	}
	var verr *documents.ValidationError
	if _, err := svc.Rename(ctx, d.ID, "   "); !errors.As(err, &verr) {
		t.Fatalf("empty rename should fail, got %v", err)
	}
	// Renaming to the same title is a no-op.
	r2, err := svc.Rename(ctx, d.ID, "New name")
	if err != nil || r2.Version != 2 {
		t.Fatalf("same-title rename should not write: %+v %v", r2, err)
	}
}

func TestDeleteAndDuplicate(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	d, _ := svc.Create(ctx, "Original", "text")

	dup, err := svc.Duplicate(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dup.ID == d.ID || dup.Title != "Original (copy)" || dup.Content != "text" || dup.Version != 1 {
		t.Fatalf("unexpected duplicate %+v", dup)
	}

	if err := svc.Delete(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, d.ID); !errors.Is(err, documents.ErrNotFound) {
		t.Fatalf("expected deleted, got %v", err)
	}
	if _, err := svc.Get(ctx, dup.ID); err != nil {
		t.Fatalf("duplicate should survive deleting the original: %v", err)
	}
	if err := svc.Delete(ctx, "nope"); !errors.Is(err, documents.ErrInvalidID) {
		t.Fatalf("expected invalid id, got %v", err)
	}
}

func TestCopyTitleStaysWithinLimit(t *testing.T) {
	long := strings.Repeat("x", documents.MaxTitleLength)
	got := documents.CopyTitle(long)
	if n := len([]rune(got)); n > documents.MaxTitleLength || !strings.HasSuffix(got, " (copy)") {
		t.Fatalf("bad copy title (len %d): %q", n, got)
	}
}

func TestListAndRecent(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	for _, title := range []string{"a", "b", "c", "d", "e", "f"} {
		if _, err := svc.Create(ctx, title, "content "+title); err != nil {
			t.Fatal(err)
		}
	}
	recent, err := svc.Recent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != documents.RecentLimit || recent[0].Title != "f" {
		t.Fatalf("unexpected recent: %+v", recent)
	}
	list, err := svc.List(ctx, documents.ListOptions{Query: "content c"})
	if err != nil || len(list) != 1 || list[0].Title != "c" {
		t.Fatalf("search failed: %+v %v", list, err)
	}
	var verr *documents.ValidationError
	if _, err := svc.List(ctx, documents.ListOptions{Sort: "bogus"}); !errors.As(err, &verr) {
		t.Fatalf("expected sort validation error, got %v", err)
	}
}
