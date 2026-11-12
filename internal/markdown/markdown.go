// Package markdown converts GitHub Flavored Markdown into sanitized HTML.
//
// Rendering happens in two stages: goldmark converts Markdown to HTML (raw
// HTML in the source is passed through so that harmless inline HTML such as
// <kbd> or <details> keeps working), then bluemonday sanitizes the result
// against a strict allow-list. Markdown content is therefore never trusted.
package markdown

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// Renderer renders Markdown to safe HTML. It is safe for concurrent use.
type Renderer struct {
	md     goldmark.Markdown
	policy *bluemonday.Policy
}

// New returns a configured Renderer.
func New() *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			// GitHub Flavored Markdown: tables, strikethrough, autolinks, task lists.
			extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
			extension.Footnote,
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(true),
					chromahtml.WithLineNumbers(false),
				),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(
			html.WithUnsafe(), // raw HTML is sanitized afterwards
		),
	)
	return &Renderer{md: md, policy: newPolicy()}
}

var classPattern = regexp.MustCompile(`^[a-zA-Z0-9_\- ]+$`)

func newPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()

	// Syntax highlighting emits class names on these elements.
	p.AllowAttrs("class").Matching(classPattern).OnElements("pre", "code", "span", "div")

	// Task list checkboxes. Only read-only checkboxes are permitted.
	p.AllowElements("input")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").Matching(regexp.MustCompile(`^(|checked|disabled)$`)).OnElements("input")

	// Harmless inline formatting that GitHub also allows in Markdown.
	p.AllowElements("kbd", "mark", "ins", "abbr")

	// Table cell alignment and footnote back-references.
	p.AllowAttrs("align").Matching(regexp.MustCompile(`^(left|right|center)$`)).OnElements("th", "td")
	p.AllowAttrs("role").Matching(regexp.MustCompile(`^doc-(noteref|backlink|endnotes)$`)).OnElements("a", "div", "sup")

	// Links: external links open safely in a new tab.
	p.RequireNoFollowOnLinks(false)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)

	// Images may use http(s) or relative URLs (UGCPolicy default); data URIs
	// are limited to raster image types.
	p.AllowDataURIImages()
	return p
}

// Render converts Markdown to sanitized HTML.
func (r *Renderer) Render(source string) (string, error) {
	var buf bytes.Buffer
	if err := r.md.Convert([]byte(source), &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return r.Sanitize(buf.String()), nil
}

// Sanitize applies the HTML allow-list to arbitrary HTML.
func (r *Renderer) Sanitize(htmlText string) string {
	out := r.policy.Sanitize(htmlText)
	// Unchecked boxes lose their (empty) checked attribute value; make every
	// remaining checkbox read-only so the preview never looks editable.
	return strings.ReplaceAll(out, `<input type="checkbox"`, `<input disabled="" type="checkbox"`)
}

var (
	cssOnce sync.Once
	cssText string
)

// HighlightCSS returns the stylesheet for highlighted code blocks: a light
// theme by default and a dark theme scoped to [data-theme="dark"].
func HighlightCSS() string {
	cssOnce.Do(func() {
		cssText = buildHighlightCSS()
	})
	return cssText
}

// LightHighlightCSS returns only the light theme (used for standalone exports).
func LightHighlightCSS() string {
	return styleCSS(styles.Get("github"))
}

func buildHighlightCSS() string {
	light := styleCSS(styles.Get("github"))
	dark := styleCSS(styles.Get("github-dark"))
	dark = strings.ReplaceAll(dark, ".chroma", `[data-theme="dark"] .chroma`)
	dark = strings.ReplaceAll(dark, ".bg ", `[data-theme="dark"] .bg `)
	// Tokens the dark theme leaves unstyled would otherwise keep the light
	// theme's dark text colors (".chroma .nx") and be unreadable. This reset
	// (specificity 0,2,1) beats the light rules (0,2,0) but loses to the dark
	// token rules (0,3,0), so those tokens inherit the dark base color.
	reset := `/* Dark reset */ [data-theme="dark"] .chroma span { color: inherit; background-color: transparent; }`
	return light + "\n" + reset + "\n" + dark
}

func styleCSS(style *chroma.Style) string {
	var buf bytes.Buffer
	f := chromahtml.New(chromahtml.WithClasses(true))
	if err := f.WriteCSS(&buf, style); err != nil {
		return ""
	}
	return buf.String()
}
