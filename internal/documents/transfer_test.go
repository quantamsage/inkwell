package documents_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"markdown-editor/internal/documents"
)

func TestParseImport(t *testing.T) {
	title, content, err := documents.ParseImport("notes.md", []byte("\xef\xbb\xbf# Hi\r\nthere\rnow"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if title != "notes" || content != "# Hi\nthere\nnow" {
		t.Fatalf("got title=%q content=%q", title, content)
	}

	// Path components in the filename are ignored.
	title, _, err = documents.ParseImport(`C:\Users\me\..\Secret Plan.MARKDOWN`, []byte("x"), 1024)
	if err != nil || title != "Secret Plan" {
		t.Fatalf("got %q, %v", title, err)
	}
	title, _, err = documents.ParseImport("../../etc/passwd.md", []byte("x"), 1024)
	if err != nil || title != "passwd" {
		t.Fatalf("got %q, %v", title, err)
	}
	// Empty stem gets the default title.
	title, _, err = documents.ParseImport(".md", []byte("x"), 1024)
	if err != nil || title != documents.DefaultTitle {
		t.Fatalf("got %q, %v", title, err)
	}
}

func TestParseImportRejects(t *testing.T) {
	var verr *documents.ValidationError
	cases := []struct {
		name string
		data []byte
	}{
		{"evil.html", []byte("<script>")},
		{"image.png", []byte("x")},
		{"noext", []byte("x")},
		{"binary.md", []byte("abc\x00def")},
		{"latin1.md", []byte("caf\xe9")},
	}
	for _, c := range cases {
		if _, _, err := documents.ParseImport(c.name, c.data, 1024); !errors.As(err, &verr) {
			t.Errorf("%s: expected validation error, got %v", c.name, err)
		}
	}
	if _, _, err := documents.ParseImport("big.md", []byte(strings.Repeat("a", 2048)), 1024); !errors.Is(err, documents.ErrContentTooLarge) {
		t.Errorf("expected too large, got %v", err)
	}
}

func TestServiceImport(t *testing.T) {
	svc := newService(t)
	d, err := svc.Import(context.Background(), "Readme.md", []byte("# Readme"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), d.ID)
	if err != nil || got.Title != "Readme" || got.Content != "# Readme" {
		t.Fatalf("import not persisted: %+v %v", got, err)
	}
}

func TestExportFilename(t *testing.T) {
	tests := map[string]string{
		"My Notes":                 "My Notes.md",
		`a/b\c:d*e?f"g<h>i|j`:      "a-b-c-d-e-f-g-h-i-j.md",
		"   ":                      "document.md",
		"...":                      "document.md",
		"Résumé 2026":              "Résumé 2026.md",
		"\r\nheader-injection\r\n": "header-injection.md",
		strings.Repeat("x", 300):   strings.Repeat("x", 100) + ".md",
	}
	for in, want := range tests {
		if got := documents.ExportFilename(in, ".md"); got != want {
			t.Errorf("ExportFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
