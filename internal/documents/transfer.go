package documents

import (
	"bytes"
	"context"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// AllowedImportExtensions lists the accepted file extensions for import.
var AllowedImportExtensions = []string{".md", ".markdown"}

// ParseImport validates an uploaded Markdown file and returns the title and
// normalized content for a new document. The filename is only used to derive
// a title; it is never used as a filesystem path.
func ParseImport(filename string, data []byte, maxBytes int) (title, content string, err error) {
	// Browsers may send full client paths (notably old IE); keep the base name.
	name := path.Base(strings.ReplaceAll(filename, `\`, "/"))
	ext := strings.ToLower(path.Ext(name))
	allowed := false
	for _, e := range AllowedImportExtensions {
		if ext == e {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", "", &ValidationError{"file", "only .md and .markdown files can be imported"}
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return "", "", ErrContentTooLarge
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // UTF-8 BOM
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", "", &ValidationError{"file", "file is not valid UTF-8 text"}
	}
	content = strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	stem := strings.TrimSuffix(name, path.Ext(name))
	title, err = NormalizeTitle(stem, true)
	if err != nil {
		// Over-long or odd file names should not block an import.
		r := []rune(strings.TrimSpace(stem))
		if len(r) > MaxTitleLength {
			r = r[:MaxTitleLength]
		}
		if title, err = NormalizeTitle(string(r), true); err != nil {
			title = DefaultTitle
		}
	}
	return title, content, nil
}

// Import validates an uploaded file and creates a document from it.
func (s *Service) Import(ctx context.Context, filename string, data []byte) (Document, error) {
	title, content, err := ParseImport(filename, data, s.maxContentBytes)
	if err != nil {
		return Document{}, err
	}
	return s.Create(ctx, title, content)
}

// ExportFilename returns a filesystem-safe download name for a title, e.g.
// "My Notes: Draft" + ".md" -> "My Notes- Draft.md".
func ExportFilename(title, ext string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.' || r == '(' || r == ')':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(strings.Join(strings.Fields(b.String()), " "), " .-")
	if r := []rune(name); len(r) > 100 {
		name = strings.TrimRight(string(r[:100]), " .-")
	}
	if name == "" {
		name = "document"
	}
	return name + ext
}
