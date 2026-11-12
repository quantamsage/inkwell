package markdown

import (
	"strings"
	"testing"
)

func render(t *testing.T, src string) string {
	t.Helper()
	out, err := New().Render(src)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRenderCommonMarkAndGFM(t *testing.T) {
	src := strings.Join([]string{
		"# Heading One",
		"## Heading Two",
		"",
		"Some **bold**, *italic*, ~~struck~~ and `inline code`.",
		"",
		"> quoted",
		"",
		"---",
		"",
		"[link](https://example.com) and https://auto.example.com",
		"",
		"- one",
		"  - nested",
		"- two",
		"",
		"1. first",
		"2. second",
		"",
		"- [ ] todo",
		"- [x] done",
		"",
		"| Left | Right |",
		"|:-----|------:|",
		"| a    | b     |",
		"",
		"![alt text](https://example.com/img.png)",
		"",
		"```go",
		"func main() {}",
		"```",
	}, "\n")
	out := render(t, src)

	want := []string{
		`<h1 id="heading-one">Heading One</h1>`,
		`<h2 id="heading-two">Heading Two</h2>`,
		`<strong>bold</strong>`,
		`<em>italic</em>`,
		`<del>struck</del>`,
		`<code>inline code</code>`,
		`<blockquote>`,
		`<hr>`,
		`<a href="https://example.com"`,
		`<a href="https://auto.example.com"`,
		"<ul>\n<li>one\n<ul>\n<li>nested</li>",
		`<ol>`,
		`<input disabled="" type="checkbox"> todo`,
		`<input checked="" disabled="" type="checkbox"> done`,
		`<table>`,
		`<th align="left">Left</th>`,
		`<td align="right">b</td>`,
		`<img src="https://example.com/img.png" alt="alt text">`,
		`<pre class="chroma"><code>`,
		`<span class="kd">func</span>`,
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q\n---\n%s", w, out)
		}
	}
}

func TestExternalLinksOpenSafely(t *testing.T) {
	out := render(t, "[x](https://example.com)")
	if !strings.Contains(out, `target="_blank"`) || !strings.Contains(out, "noopener") {
		t.Fatalf("external link not hardened: %s", out)
	}
}

func TestSanitizationRemovesXSS(t *testing.T) {
	attacks := map[string]string{
		"script tag":         "<script>alert(1)</script>",
		"img onerror":        `<img src="x" onerror="alert(1)">`,
		"javascript link":    "[click](javascript:alert(1))",
		"javascript raw a":   `<a href="javascript:alert(1)">x</a>`,
		"vbscript link":      "[x](vbscript:msgbox(1))",
		"data html link":     "[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)",
		"iframe":             `<iframe src="https://evil.example"></iframe>`,
		"svg onload":         `<svg onload="alert(1)"></svg>`,
		"style tag":          `<style>body{display:none}</style>`,
		"inline style":       `<p style="background:url(javascript:alert(1))">x</p>`,
		"object":             `<object data="evil.swf"></object>`,
		"form":               `<form action="https://evil.example"><button>go</button></form>`,
		"meta refresh":       `<meta http-equiv="refresh" content="0;url=https://evil.example">`,
		"onclick div":        `<div onclick="alert(1)">x</div>`,
		"text input":         `<input type="text" value="phish">`,
		"image js src":       `![x](javascript:alert(1))`,
		"entity obfuscation": `<a href="&#106;avascript:alert(1)">x</a>`,
	}
	forbidden := []string{"<script", "onerror", "javascript:", "vbscript:", "data:text/html", "<iframe", "onload", "<style", "style=", "<object", "<form", "<meta", "onclick", `type="text"`}
	for name, src := range attacks {
		out := strings.ToLower(render(t, src))
		for _, f := range forbidden {
			if strings.Contains(out, f) {
				t.Errorf("%s: output contains %q: %s", name, f, out)
			}
		}
	}
}

func TestSanitizeKeepsSafeHTML(t *testing.T) {
	out := render(t, "<details><summary>More</summary>hidden</details> press <kbd>Ctrl</kbd>")
	for _, w := range []string{"<details>", "<summary>More</summary>", "<kbd>Ctrl</kbd>"} {
		if !strings.Contains(out, w) {
			t.Errorf("expected %q in %s", w, out)
		}
	}
}

func TestRawCheckboxesAreReadOnly(t *testing.T) {
	out := render(t, `<input type="checkbox">`)
	if strings.Contains(out, "<input") && !strings.Contains(out, "disabled") {
		t.Fatalf("checkbox should be disabled: %s", out)
	}
}

func TestClassAttributesAreRestricted(t *testing.T) {
	out := render(t, `<span class="x&quot; onmouseover=&quot;alert(1)">t</span><span class="ok">u</span>`)
	if strings.Contains(out, "onmouseover") {
		t.Fatalf("attribute injection survived: %s", out)
	}
	if !strings.Contains(out, `<span class="ok">u</span>`) {
		t.Fatalf("safe class removed: %s", out)
	}
}

func TestHighlightCSS(t *testing.T) {
	css := HighlightCSS()
	if !strings.Contains(css, ".chroma .k") {
		t.Fatal("light theme rules missing")
	}
	if !strings.Contains(css, `[data-theme="dark"] .chroma .k`) {
		t.Fatal("dark theme rules missing")
	}
	if strings.Contains(LightHighlightCSS(), "data-theme") {
		t.Fatal("light-only CSS must not contain dark rules")
	}
}

func TestRenderEmpty(t *testing.T) {
	if out := render(t, ""); out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}
