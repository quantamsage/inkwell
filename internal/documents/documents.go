// Package documents contains the document domain model and business rules:
// validation, defaults, optimistic concurrency, duplication and import.
// Persistence is delegated to a Store implementation (see internal/database).
package documents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxTitleLength is the maximum title length in characters (runes).
	MaxTitleLength = 200
	// DefaultTitle is used when a document is created without a title.
	DefaultTitle = "Untitled"
	// DefaultMaxContentBytes is the content size limit used when none is configured.
	DefaultMaxContentBytes = 5 << 20
	// RecentLimit is the number of documents returned as "recently edited".
	RecentLimit = 5
)

var (
	ErrNotFound        = errors.New("document not found")
	ErrInvalidID       = errors.New("invalid document id")
	ErrConflict        = errors.New("document was modified by another save")
	ErrContentTooLarge = errors.New("document content is too large")
	ErrInvalidImport   = errors.New("invalid import file")
)

// ValidationError describes invalid user input.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// Document is a stored Markdown document.
type Document struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Summary is a lightweight listing entry.
type Summary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Snippet   string    `json:"snippet"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SortField selects the listing order.
type SortField string

const (
	SortUpdated SortField = "updated"
	SortCreated SortField = "created"
	SortTitle   SortField = "title"
)

// ListOptions controls listing and searching.
type ListOptions struct {
	Query      string
	Sort       SortField
	Descending bool
	Limit      int
}

// Store persists documents. Implementations must be safe for concurrent use.
type Store interface {
	Insert(ctx context.Context, d Document) error
	Get(ctx context.Context, id string) (Document, error)
	List(ctx context.Context, opts ListOptions) ([]Summary, error)
	// UpdateIfVersion writes title/content/updatedAt and bumps the version only
	// if the stored version equals expectedVersion. It returns ErrConflict
	// when the version does not match and ErrNotFound when the row is missing.
	UpdateIfVersion(ctx context.Context, id string, expectedVersion int64, title, content string, updatedAt time.Time) (Document, error)
	Delete(ctx context.Context, id string) error
}

// Service implements document use cases on top of a Store.
type Service struct {
	store           Store
	maxContentBytes int
	now             func() time.Time
}

// NewService creates a Service. maxContentBytes <= 0 uses DefaultMaxContentBytes.
func NewService(store Store, maxContentBytes int64) *Service {
	if maxContentBytes <= 0 {
		maxContentBytes = DefaultMaxContentBytes
	}
	return &Service{
		store:           store,
		maxContentBytes: int(maxContentBytes),
		// Timestamps are stored with millisecond precision.
		now: func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) },
	}
}

// SetClock overrides the clock (used in tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// MaxContentBytes reports the configured content size limit.
func (s *Service) MaxContentBytes() int { return s.maxContentBytes }

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidID reports whether id has the expected format.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// NewID returns a random 128-bit hex identifier.
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// NormalizeTitle trims and validates a title. An empty title becomes
// DefaultTitle when allowEmpty is true; otherwise it is an error.
func NormalizeTitle(title string, allowEmpty bool) (string, error) {
	if !utf8.ValidString(title) {
		return "", &ValidationError{"title", "must be valid UTF-8"}
	}
	// Collapse control characters (including newlines) into spaces.
	title = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, title)
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		if allowEmpty {
			return DefaultTitle, nil
		}
		return "", &ValidationError{"title", "must not be empty"}
	}
	if utf8.RuneCountInString(title) > MaxTitleLength {
		return "", &ValidationError{"title", fmt.Sprintf("must be at most %d characters", MaxTitleLength)}
	}
	return title, nil
}

func (s *Service) validateContent(content string) error {
	if len(content) > s.maxContentBytes {
		return ErrContentTooLarge
	}
	if !utf8.ValidString(content) {
		return &ValidationError{"content", "must be valid UTF-8"}
	}
	return nil
}

// Create stores a new document.
func (s *Service) Create(ctx context.Context, title, content string) (Document, error) {
	title, err := NormalizeTitle(title, true)
	if err != nil {
		return Document{}, err
	}
	if err := s.validateContent(content); err != nil {
		return Document{}, err
	}
	id, err := NewID()
	if err != nil {
		return Document{}, err
	}
	now := s.now()
	d := Document{ID: id, Title: title, Content: content, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.store.Insert(ctx, d); err != nil {
		return Document{}, err
	}
	return d, nil
}

// Get returns a document by id.
func (s *Service) Get(ctx context.Context, id string) (Document, error) {
	if !ValidID(id) {
		return Document{}, ErrInvalidID
	}
	return s.store.Get(ctx, id)
}

// List returns documents filtered by an optional search query and ordered as requested.
func (s *Service) List(ctx context.Context, opts ListOptions) ([]Summary, error) {
	switch opts.Sort {
	case SortUpdated, SortCreated, SortTitle:
	case "":
		opts.Sort = SortUpdated
		opts.Descending = true
	default:
		return nil, &ValidationError{"sort", "must be one of updated, created, title"}
	}
	opts.Query = strings.TrimSpace(opts.Query)
	if utf8.RuneCountInString(opts.Query) > 200 {
		return nil, &ValidationError{"q", "search query is too long"}
	}
	if opts.Limit <= 0 || opts.Limit > 1000 {
		opts.Limit = 1000
	}
	return s.store.List(ctx, opts)
}

// Recent returns the most recently edited documents.
func (s *Service) Recent(ctx context.Context) ([]Summary, error) {
	return s.store.List(ctx, ListOptions{Sort: SortUpdated, Descending: true, Limit: RecentLimit})
}

// SaveResult is the outcome of Save.
type SaveResult struct {
	Document Document
	// Changed is false when the submitted title and content matched the stored
	// document, in which case nothing was written.
	Changed bool
}

// Save updates title and content using optimistic concurrency: baseVersion
// must equal the stored version, otherwise ErrConflict is returned together
// with the current document so the caller can resolve it. Saving identical
// content performs no database write.
func (s *Service) Save(ctx context.Context, id string, baseVersion int64, title, content string) (SaveResult, error) {
	if !ValidID(id) {
		return SaveResult{}, ErrInvalidID
	}
	if baseVersion < 1 {
		return SaveResult{}, &ValidationError{"version", "must be a positive integer"}
	}
	title, err := NormalizeTitle(title, true)
	if err != nil {
		return SaveResult{}, err
	}
	if err := s.validateContent(content); err != nil {
		return SaveResult{}, err
	}

	current, err := s.store.Get(ctx, id)
	if err != nil {
		return SaveResult{}, err
	}
	if current.Version != baseVersion {
		return SaveResult{Document: current}, ErrConflict
	}
	if current.Title == title && current.Content == content {
		return SaveResult{Document: current, Changed: false}, nil
	}

	updated, err := s.store.UpdateIfVersion(ctx, id, baseVersion, title, content, s.now())
	if errors.Is(err, ErrConflict) {
		// Lost a race between the read above and the write.
		latest, getErr := s.store.Get(ctx, id)
		if getErr != nil {
			return SaveResult{}, getErr
		}
		return SaveResult{Document: latest}, ErrConflict
	}
	if err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Document: updated, Changed: true}, nil
}

// Rename changes only the title. It does not require a version because it
// cannot clobber content; it still bumps the version so that open editors
// detect the change.
func (s *Service) Rename(ctx context.Context, id, title string) (Document, error) {
	if !ValidID(id) {
		return Document{}, ErrInvalidID
	}
	title, err := NormalizeTitle(title, false)
	if err != nil {
		return Document{}, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		current, err := s.store.Get(ctx, id)
		if err != nil {
			return Document{}, err
		}
		if current.Title == title {
			return current, nil
		}
		updated, err := s.store.UpdateIfVersion(ctx, id, current.Version, title, current.Content, s.now())
		if errors.Is(err, ErrConflict) {
			continue
		}
		return updated, err
	}
	return Document{}, ErrConflict
}

// Delete removes a document permanently.
func (s *Service) Delete(ctx context.Context, id string) error {
	if !ValidID(id) {
		return ErrInvalidID
	}
	return s.store.Delete(ctx, id)
}

// Duplicate copies a document under a "(copy)" title.
func (s *Service) Duplicate(ctx context.Context, id string) (Document, error) {
	src, err := s.Get(ctx, id)
	if err != nil {
		return Document{}, err
	}
	return s.Create(ctx, CopyTitle(src.Title), src.Content)
}

// CopyTitle derives the title of a duplicated document, keeping it within limits.
func CopyTitle(title string) string {
	const suffix = " (copy)"
	max := MaxTitleLength - utf8.RuneCountInString(suffix)
	if r := []rune(title); len(r) > max {
		title = strings.TrimSpace(string(r[:max]))
	}
	return title + suffix
}
