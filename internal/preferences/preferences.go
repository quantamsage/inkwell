// Package preferences validates and persists editor/user preferences.
package preferences

import (
	"context"
	"fmt"
	"strconv"
)

// Preferences are the user-adjustable settings of the editor.
type Preferences struct {
	Theme      string  `json:"theme"`      // "light" or "dark"
	FontSize   int     `json:"fontSize"`   // editor font size in px
	FontFamily string  `json:"fontFamily"` // one of FontFamilies keys
	LineHeight float64 `json:"lineHeight"` // editor line spacing multiplier
	ViewMode   string  `json:"viewMode"`   // "editor", "split" or "preview"
	SplitRatio float64 `json:"splitRatio"` // editor share of split view (0.2–0.8)
	SortBy     string  `json:"sortBy"`     // sidebar sort, see SortOptions
}

// FontFamilies maps the allowed font family keys to CSS font stacks.
var FontFamilies = map[string]string{
	"mono":  `ui-monospace, "SFMono-Regular", "Cascadia Code", "JetBrains Mono", Menlo, Consolas, "Liberation Mono", monospace`,
	"sans":  `system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`,
	"serif": `"Iowan Old Style", "Palatino Linotype", Palatino, Georgia, "Times New Roman", serif`,
}

// SortOptions lists the allowed sidebar sort values.
var SortOptions = []string{"updated-desc", "updated-asc", "created-desc", "created-asc", "title-asc", "title-desc"}

// Limits for numeric settings.
const (
	MinFontSize   = 12
	MaxFontSize   = 28
	MinLineHeight = 1.2
	MaxLineHeight = 2.4
	MinSplitRatio = 0.2
	MaxSplitRatio = 0.8
)

// Defaults returns the default preferences.
func Defaults() Preferences {
	return Preferences{
		Theme:      "light",
		FontSize:   15,
		FontFamily: "mono",
		LineHeight: 1.6,
		ViewMode:   "split",
		SplitRatio: 0.5,
		SortBy:     "updated-desc",
	}
}

// ValidationError reports an invalid preference value.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// Validate checks all fields.
func (p Preferences) Validate() error {
	switch p.Theme {
	case "light", "dark":
	default:
		return &ValidationError{"theme", "must be light or dark"}
	}
	if p.FontSize < MinFontSize || p.FontSize > MaxFontSize {
		return &ValidationError{"fontSize", fmt.Sprintf("must be between %d and %d", MinFontSize, MaxFontSize)}
	}
	if _, ok := FontFamilies[p.FontFamily]; !ok {
		return &ValidationError{"fontFamily", "must be mono, sans or serif"}
	}
	if p.LineHeight < MinLineHeight || p.LineHeight > MaxLineHeight {
		return &ValidationError{"lineHeight", fmt.Sprintf("must be between %.1f and %.1f", MinLineHeight, MaxLineHeight)}
	}
	switch p.ViewMode {
	case "editor", "split", "preview":
	default:
		return &ValidationError{"viewMode", "must be editor, split or preview"}
	}
	if p.SplitRatio < MinSplitRatio || p.SplitRatio > MaxSplitRatio {
		return &ValidationError{"splitRatio", fmt.Sprintf("must be between %.1f and %.1f", MinSplitRatio, MaxSplitRatio)}
	}
	if !contains(SortOptions, p.SortBy) {
		return &ValidationError{"sortBy", "is not a supported sort order"}
	}
	return nil
}

// FontStack returns the CSS font stack for the selected family.
func (p Preferences) FontStack() string { return FontFamilies[p.FontFamily] }

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// Store persists preferences as string key/value pairs.
type Store interface {
	LoadAll(ctx context.Context) (map[string]string, error)
	SaveAll(ctx context.Context, values map[string]string) error
}

// Service loads and saves preferences.
type Service struct{ store Store }

// NewService returns a Service.
func NewService(store Store) *Service { return &Service{store: store} }

// Get returns stored preferences merged over defaults. Stored values that are
// unparsable or invalid (for example after a limit change) fall back to the
// default for that field.
func (s *Service) Get(ctx context.Context) (Preferences, error) {
	values, err := s.store.LoadAll(ctx)
	if err != nil {
		return Preferences{}, err
	}
	return fromMap(values), nil
}

// Update validates and stores all preferences.
func (s *Service) Update(ctx context.Context, p Preferences) (Preferences, error) {
	if err := p.Validate(); err != nil {
		return Preferences{}, err
	}
	if err := s.store.SaveAll(ctx, toMap(p)); err != nil {
		return Preferences{}, err
	}
	return p, nil
}

func toMap(p Preferences) map[string]string {
	return map[string]string{
		"theme":      p.Theme,
		"fontSize":   strconv.Itoa(p.FontSize),
		"fontFamily": p.FontFamily,
		"lineHeight": strconv.FormatFloat(p.LineHeight, 'f', -1, 64),
		"viewMode":   p.ViewMode,
		"splitRatio": strconv.FormatFloat(p.SplitRatio, 'f', -1, 64),
		"sortBy":     p.SortBy,
	}
}

func fromMap(m map[string]string) Preferences {
	def := Defaults()
	p := def

	// Apply each field independently and keep the default if it is invalid.
	try := func(apply func(*Preferences)) {
		candidate := p
		apply(&candidate)
		if candidate.Validate() == nil {
			p = candidate
		}
	}
	if v, ok := m["theme"]; ok {
		try(func(c *Preferences) { c.Theme = v })
	}
	if v, ok := m["fontSize"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			try(func(c *Preferences) { c.FontSize = n })
		}
	}
	if v, ok := m["fontFamily"]; ok {
		try(func(c *Preferences) { c.FontFamily = v })
	}
	if v, ok := m["lineHeight"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			try(func(c *Preferences) { c.LineHeight = f })
		}
	}
	if v, ok := m["viewMode"]; ok {
		try(func(c *Preferences) { c.ViewMode = v })
	}
	if v, ok := m["splitRatio"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			try(func(c *Preferences) { c.SplitRatio = f })
		}
	}
	if v, ok := m["sortBy"]; ok {
		try(func(c *Preferences) { c.SortBy = v })
	}
	return p
}
