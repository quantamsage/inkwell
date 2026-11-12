package preferences

import (
	"context"
	"errors"
	"testing"
)

type memStore struct{ m map[string]string }

func (s *memStore) LoadAll(context.Context) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range s.m {
		out[k] = v
	}
	return out, nil
}

func (s *memStore) SaveAll(_ context.Context, v map[string]string) error {
	for k, val := range v {
		s.m[k] = val
	}
	return nil
}

func TestDefaultsAreValid(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	mut := []func(*Preferences){
		func(p *Preferences) { p.Theme = "neon" },
		func(p *Preferences) { p.FontSize = 2 },
		func(p *Preferences) { p.FontSize = 99 },
		func(p *Preferences) { p.FontFamily = "comic; } body { display:none" },
		func(p *Preferences) { p.LineHeight = 5 },
		func(p *Preferences) { p.ViewMode = "grid" },
		func(p *Preferences) { p.SplitRatio = 0.95 },
		func(p *Preferences) { p.SortBy = "random" },
	}
	for i, m := range mut {
		p := Defaults()
		m(&p)
		var verr *ValidationError
		if err := p.Validate(); !errors.As(err, &verr) {
			t.Errorf("case %d: expected validation error, got %v", i, err)
		}
	}
}

func TestServiceRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&memStore{m: map[string]string{}})

	got, err := svc.Get(ctx)
	if err != nil || got != Defaults() {
		t.Fatalf("expected defaults, got %+v %v", got, err)
	}

	p := Defaults()
	p.Theme, p.FontSize, p.FontFamily, p.LineHeight, p.ViewMode, p.SplitRatio, p.SortBy =
		"dark", 18, "serif", 1.8, "preview", 0.35, "title-asc"
	if _, err := svc.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err = svc.Get(ctx)
	if err != nil || got != p {
		t.Fatalf("round trip mismatch: %+v vs %+v (%v)", got, p, err)
	}

	bad := p
	bad.Theme = "x"
	if _, err := svc.Update(ctx, bad); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestGetIgnoresCorruptStoredValues(t *testing.T) {
	svc := NewService(&memStore{m: map[string]string{
		"theme":      "dark",
		"fontSize":   "huge",
		"lineHeight": "100",
		"fontFamily": "sans",
	}})
	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	def := Defaults()
	if got.Theme != "dark" || got.FontFamily != "sans" || got.FontSize != def.FontSize || got.LineHeight != def.LineHeight {
		t.Fatalf("unexpected merge result %+v", got)
	}
}
