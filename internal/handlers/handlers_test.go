package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"markdown-editor/internal/database"
	"markdown-editor/internal/documents"
	"markdown-editor/internal/markdown"
	"markdown-editor/internal/middleware"
	"markdown-editor/internal/preferences"
)

const testMaxBytes = 4096

type testApp struct {
	t       *testing.T
	handler http.Handler
	token   string
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv, err := New(Options{
		Documents:   documents.NewService(database.NewDocumentStore(db), testMaxBytes),
		Preferences: preferences.NewService(database.NewPreferenceStore(db)),
		Renderer:    markdown.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &testApp{t: t, handler: srv.Routes(), token: strings.Repeat("t", 43)}
}

// do performs a request with a valid CSRF cookie and header.
func (a *testApp) do(method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: a.token})
	req.Header.Set(middleware.CSRFHeaderName, a.token)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func (a *testApp) json(method, path string, payload any) *httptest.ResponseRecorder {
	a.t.Helper()
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			a.t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	return a.do(method, path, body, "application/json")
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
}

func (a *testApp) create(title, content string) documents.Document {
	a.t.Helper()
	rec := a.json(http.MethodPost, "/api/documents", map[string]string{"title": title, "content": content})
	expectStatus(a.t, rec, http.StatusCreated)
	return decode[documents.Document](a.t, rec)
}

func TestIndexPage(t *testing.T) {
	app := newTestApp(t)
	rec := httptest.NewRecorder()
	app.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	expectStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, `<meta name="csrf-token" content="`) || !strings.Contains(body, `data-theme="light"`) {
		t.Fatal("index page missing csrf token or theme")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP header")
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("csrf cookie not issued")
	}
}

func TestStaticAssets(t *testing.T) {
	app := newTestApp(t)
	for path, ctype := range map[string]string{
		"/static/css/app.css":      "text/css",
		"/static/js/app.js":        "javascript",
		"/static/images/logo.svg":  "image/svg+xml",
		"/assets/highlight.css":    "text/css",
		"/static/css/document.css": "text/css",
	} {
		rec := app.do(http.MethodGet, path, nil, "")
		expectStatus(t, rec, http.StatusOK)
		if !strings.Contains(rec.Header().Get("Content-Type"), ctype) {
			t.Errorf("%s: content type %q", path, rec.Header().Get("Content-Type"))
		}
	}
	// Path traversal never reaches files outside the embedded static tree.
	for _, p := range []string{"/static/../go.mod", "/static/%2e%2e/go.mod", "/static/..%2f..%2fgo.mod"} {
		rec := app.do(http.MethodGet, p, nil, "")
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "module markdown-editor") {
			t.Errorf("%s: traversal served content (status %d)", p, rec.Code)
		}
	}
	expectStatus(t, app.do(http.MethodGet, "/nope", nil, ""), http.StatusNotFound)
}

func TestDocumentCRUD(t *testing.T) {
	app := newTestApp(t)

	d := app.create("First", "# Hello")
	if d.Title != "First" || d.Version != 1 || !documents.ValidID(d.ID) {
		t.Fatalf("unexpected created doc %+v", d)
	}

	rec := app.do(http.MethodGet, "/api/documents/"+d.ID, nil, "")
	expectStatus(t, rec, http.StatusOK)
	if got := decode[documents.Document](t, rec); got.Content != "# Hello" {
		t.Fatalf("unexpected doc %+v", got)
	}

	// Update.
	rec = app.json(http.MethodPut, "/api/documents/"+d.ID, map[string]any{"title": "First", "content": "# Changed", "version": 1})
	expectStatus(t, rec, http.StatusOK)
	saved := decode[saveResponse](t, rec)
	if !saved.Changed || saved.Document.Version != 2 || saved.Document.Content != "# Changed" {
		t.Fatalf("unexpected save %+v", saved)
	}

	// Rename.
	rec = app.json(http.MethodPatch, "/api/documents/"+d.ID, map[string]any{"title": "Renamed"})
	expectStatus(t, rec, http.StatusOK)
	if r := decode[documents.Document](t, rec); r.Title != "Renamed" || r.Content != "# Changed" {
		t.Fatalf("unexpected rename %+v", r)
	}
	expectStatus(t, app.json(http.MethodPatch, "/api/documents/"+d.ID, map[string]any{"title": "  "}), http.StatusUnprocessableEntity)
	expectStatus(t, app.json(http.MethodPatch, "/api/documents/"+d.ID, map[string]any{}), http.StatusUnprocessableEntity)

	// Duplicate.
	rec = app.do(http.MethodPost, "/api/documents/"+d.ID+"/duplicate", nil, "")
	expectStatus(t, rec, http.StatusCreated)
	dup := decode[documents.Document](t, rec)
	if dup.Title != "Renamed (copy)" || dup.Content != "# Changed" || dup.ID == d.ID {
		t.Fatalf("unexpected duplicate %+v", dup)
	}

	// Delete.
	expectStatus(t, app.do(http.MethodDelete, "/api/documents/"+d.ID, nil, ""), http.StatusNoContent)
	expectStatus(t, app.do(http.MethodGet, "/api/documents/"+d.ID, nil, ""), http.StatusNotFound)
	expectStatus(t, app.do(http.MethodDelete, "/api/documents/"+d.ID, nil, ""), http.StatusNotFound)
}

func TestListSearchSortAndRecent(t *testing.T) {
	app := newTestApp(t)
	banana := app.create("Banana", "yellow")
	app.create("apple", "red fruit")
	app.create("Cherry", "red too")
	time.Sleep(5 * time.Millisecond)
	// Editing Banana makes it the most recently edited document.
	expectStatus(t, app.json(http.MethodPut, "/api/documents/"+banana.ID, map[string]any{"title": "Banana", "content": "yellow!", "version": 1}), http.StatusOK)

	rec := app.do(http.MethodGet, "/api/documents?sort=title-asc", nil, "")
	expectStatus(t, rec, http.StatusOK)
	list := decode[listResponse](t, rec).Documents
	if len(list) != 3 || list[0].Title != "apple" || list[2].Title != "Cherry" {
		t.Fatalf("unexpected order %+v", list)
	}

	rec = app.do(http.MethodGet, "/api/documents?q=red&sort=title-desc", nil, "")
	list = decode[listResponse](t, rec).Documents
	if len(list) != 2 || list[0].Title != "Cherry" {
		t.Fatalf("unexpected search result %+v", list)
	}

	rec = app.do(http.MethodGet, "/api/documents/recent", nil, "")
	expectStatus(t, rec, http.StatusOK)
	if recent := decode[listResponse](t, rec).Documents; len(recent) != 3 || recent[0].Title != "Banana" {
		t.Fatalf("unexpected recent %+v", recent)
	}

	expectStatus(t, app.do(http.MethodGet, "/api/documents?sort=bogus", nil, ""), http.StatusBadRequest)
	expectStatus(t, app.do(http.MethodGet, "/api/documents?sort=title-sideways", nil, ""), http.StatusBadRequest)
}

func TestValidation(t *testing.T) {
	app := newTestApp(t)
	d := app.create("Doc", "")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		ctype  string
		want   int
	}{
		{"invalid id get", http.MethodGet, "/api/documents/not-an-id", "", "", http.StatusBadRequest},
		{"invalid id put", http.MethodPut, "/api/documents/xyz", `{}`, "application/json", http.StatusBadRequest},
		{"invalid id delete", http.MethodDelete, "/api/documents/xyz", "", "", http.StatusBadRequest},
		{"unknown id", http.MethodGet, "/api/documents/" + strings.Repeat("a", 32), "", "", http.StatusNotFound},
		{"wrong content type", http.MethodPost, "/api/documents", `{"title":"x"}`, "text/plain", http.StatusUnsupportedMediaType},
		{"malformed json", http.MethodPost, "/api/documents", `{"title":`, "application/json", http.StatusBadRequest},
		{"unknown field", http.MethodPost, "/api/documents", `{"title":"x","admin":true}`, "application/json", http.StatusBadRequest},
		{"trailing data", http.MethodPost, "/api/documents", `{"title":"x"}{}`, "application/json", http.StatusBadRequest},
		{"wrong type", http.MethodPost, "/api/documents", `{"title":5}`, "application/json", http.StatusBadRequest},
		{"long title", http.MethodPost, "/api/documents", `{"title":"` + strings.Repeat("a", 201) + `"}`, "application/json", http.StatusUnprocessableEntity},
		{"too large", http.MethodPost, "/api/documents", `{"content":"` + strings.Repeat("a", testMaxBytes+1) + `"}`, "application/json", http.StatusRequestEntityTooLarge},
		{"save missing version", http.MethodPut, "/api/documents/" + d.ID, `{"title":"a","content":"b"}`, "application/json", http.StatusUnprocessableEntity},
		{"save missing content", http.MethodPut, "/api/documents/" + d.ID, `{"title":"a","version":1}`, "application/json", http.StatusUnprocessableEntity},
		{"save bad version", http.MethodPut, "/api/documents/" + d.ID, `{"title":"a","content":"b","version":0}`, "application/json", http.StatusUnprocessableEntity},
		{"method not allowed", http.MethodPost, "/api/documents/" + d.ID, "", "", http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		var body io.Reader
		if c.body != "" {
			body = strings.NewReader(c.body)
		}
		rec := app.do(c.method, c.path, body, c.ctype)
		if rec.Code != c.want {
			t.Errorf("%s: status %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}
}

func TestCSRFRequiredForMutations(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", strings.NewReader(`{"title":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.handler.ServeHTTP(rec, req)
	expectStatus(t, rec, http.StatusForbidden)

	rec = app.do(http.MethodGet, "/api/documents", nil, "")
	if list := decode[listResponse](t, rec).Documents; len(list) != 0 {
		t.Fatal("document was created without a CSRF token")
	}
}

// Autosave: identical content is acknowledged without a write.
func TestAutosaveUnchangedContent(t *testing.T) {
	app := newTestApp(t)
	d := app.create("Doc", "same")
	rec := app.json(http.MethodPut, "/api/documents/"+d.ID, map[string]any{"title": "Doc", "content": "same", "version": 1})
	expectStatus(t, rec, http.StatusOK)
	res := decode[saveResponse](t, rec)
	if res.Changed || res.Document.Version != 1 || !res.Document.UpdatedAt.Equal(d.UpdatedAt) {
		t.Fatalf("unchanged save should not write: %+v", res)
	}
}

// Autosave: a save based on a stale version is rejected with 409 and the
// current document, so older content never overwrites newer content.
func TestAutosaveStaleVersionConflict(t *testing.T) {
	app := newTestApp(t)
	d := app.create("Doc", "v1")

	expectStatus(t, app.json(http.MethodPut, "/api/documents/"+d.ID, map[string]any{"title": "Doc", "content": "v2", "version": 1}), http.StatusOK)
	expectStatus(t, app.json(http.MethodPut, "/api/documents/"+d.ID, map[string]any{"title": "Doc", "content": "v3", "version": 2}), http.StatusOK)

	rec := app.json(http.MethodPut, "/api/documents/"+d.ID, map[string]any{"title": "Doc", "content": "stale v2 edit", "version": 2})
	expectStatus(t, rec, http.StatusConflict)
	conflict := decode[conflictResponse](t, rec)
	if conflict.Document.Content != "v3" || conflict.Document.Version != 3 {
		t.Fatalf("conflict should carry the current document: %+v", conflict)
	}

	rec = app.do(http.MethodGet, "/api/documents/"+d.ID, nil, "")
	if got := decode[documents.Document](t, rec); got.Content != "v3" {
		t.Fatalf("stale save overwrote newer content: %q", got.Content)
	}
}

func TestRenderEndpoint(t *testing.T) {
	app := newTestApp(t)
	rec := app.json(http.MethodPost, "/api/render", map[string]string{"content": "# Hi\n\n<script>alert(1)</script>\n\n- [x] done"})
	expectStatus(t, rec, http.StatusOK)
	html := decode[renderResponse](t, rec).HTML
	if !strings.Contains(html, `<h1 id="hi">Hi</h1>`) || !strings.Contains(html, `checkbox`) {
		t.Fatalf("unexpected render: %s", html)
	}
	if strings.Contains(html, "<script") {
		t.Fatalf("render output not sanitized: %s", html)
	}
	expectStatus(t, app.json(http.MethodPost, "/api/render", map[string]string{"content": strings.Repeat("a", testMaxBytes+1)}), http.StatusRequestEntityTooLarge)
}

func multipartBody(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(content)
	w.Close()
	return &buf, w.FormDataContentType()
}

func TestImport(t *testing.T) {
	app := newTestApp(t)

	body, ctype := multipartBody(t, "file", "My Notes.md", []byte("# Notes\r\n\r\ntext"))
	rec := app.do(http.MethodPost, "/api/documents/import", body, ctype)
	expectStatus(t, rec, http.StatusCreated)
	d := decode[documents.Document](t, rec)
	if d.Title != "My Notes" || d.Content != "# Notes\n\ntext" {
		t.Fatalf("unexpected import %+v", d)
	}

	cases := []struct {
		name     string
		field    string
		filename string
		content  []byte
		want     int
	}{
		{"wrong extension", "file", "script.js", []byte("alert(1)"), http.StatusUnprocessableEntity},
		{"html file", "file", "page.html", []byte("<p>x</p>"), http.StatusUnprocessableEntity},
		{"binary", "file", "bin.md", []byte{0x89, 'P', 'N', 'G', 0, 0}, http.StatusUnprocessableEntity},
		{"too large", "file", "big.md", bytes.Repeat([]byte("a"), testMaxBytes+1), http.StatusRequestEntityTooLarge},
		{"way too large", "file", "huge.md", bytes.Repeat([]byte("a"), testMaxBytes*40), http.StatusRequestEntityTooLarge},
		{"missing file field", "other", "x.md", []byte("x"), http.StatusBadRequest},
	}
	for _, c := range cases {
		body, ctype := multipartBody(t, c.field, c.filename, c.content)
		rec := app.do(http.MethodPost, "/api/documents/import", body, ctype)
		if rec.Code != c.want {
			t.Errorf("%s: status %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}

	rec = app.do(http.MethodPost, "/api/documents/import", strings.NewReader("{}"), "application/json")
	expectStatus(t, rec, http.StatusUnsupportedMediaType)
}

func TestExportMarkdown(t *testing.T) {
	app := newTestApp(t)
	d := app.create(`Report: Q1/Q2 "final"`, "# Report\n\nBody")

	rec := app.do(http.MethodGet, "/api/documents/"+d.ID+"/export?format=md", nil, "")
	expectStatus(t, rec, http.StatusOK)
	if rec.Body.String() != "# Report\n\nBody" {
		t.Fatalf("unexpected body %q", rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}
	disp, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
	if err != nil || disp != "attachment" {
		t.Fatalf("bad disposition %q (%v)", rec.Header().Get("Content-Disposition"), err)
	}
	if params["filename"] != "Report- Q1-Q2 -final.md" {
		t.Fatalf("unexpected filename %q", params["filename"])
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("nosniff missing on download")
	}
}

func TestExportHTMLIsStandaloneAndSanitized(t *testing.T) {
	app := newTestApp(t)
	d := app.create("Page <Title>", "# Heading\n\n<img src=x onerror=alert(1)>\n\n```go\nfunc x() {}\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |")

	rec := app.do(http.MethodGet, "/api/documents/"+d.ID+"/export?format=html", nil, "")
	expectStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	for _, want := range []string{"<!doctype html>", "<title>Page &lt;Title&gt;</title>", "<style>", ".markdown-body", `<h1 id="heading">Heading</h1>`, "<table>", `class="chroma"`, ".chroma .k"} {
		if !strings.Contains(body, want) {
			t.Errorf("export missing %q", want)
		}
	}
	if strings.Contains(body, "onerror") {
		t.Fatal("export not sanitized")
	}
	_, params, _ := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
	if params["filename"] != "Page -Title.html" {
		t.Fatalf("unexpected filename %q", params["filename"])
	}

	expectStatus(t, app.do(http.MethodGet, "/api/documents/"+d.ID+"/export?format=exe", nil, ""), http.StatusBadRequest)
	expectStatus(t, app.do(http.MethodGet, "/api/documents/"+strings.Repeat("b", 32)+"/export?format=md", nil, ""), http.StatusNotFound)
}

func TestExportUnicodeFilename(t *testing.T) {
	app := newTestApp(t)
	d := app.create("Résumé", "x")
	rec := app.do(http.MethodGet, "/api/documents/"+d.ID+"/export?format=md", nil, "")
	_, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
	if err != nil || params["filename"] != "Résumé.md" {
		t.Fatalf("unexpected filename %q (%v) header=%q", params["filename"], err, rec.Header().Get("Content-Disposition"))
	}
}

func TestPrintPage(t *testing.T) {
	app := newTestApp(t)
	d := app.create("Printable", "# Title\n\n- item")
	rec := app.do(http.MethodGet, "/documents/"+d.ID+"/print", nil, "")
	expectStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, `<h1 id="title">Title</h1>`) || !strings.Contains(body, "/static/css/document.css") || !strings.Contains(body, "/static/js/print.js") {
		t.Fatalf("unexpected print page: %s", body)
	}
	expectStatus(t, app.do(http.MethodGet, "/documents/bad/print", nil, ""), http.StatusBadRequest)
	expectStatus(t, app.do(http.MethodGet, "/documents/"+strings.Repeat("c", 32)+"/print", nil, ""), http.StatusNotFound)
}

func TestPreferencesEndpoints(t *testing.T) {
	app := newTestApp(t)
	rec := app.do(http.MethodGet, "/api/preferences", nil, "")
	expectStatus(t, rec, http.StatusOK)
	if p := decode[preferences.Preferences](t, rec); p != preferences.Defaults() {
		t.Fatalf("expected defaults, got %+v", p)
	}

	p := preferences.Defaults()
	p.Theme, p.FontSize = "dark", 20
	expectStatus(t, app.json(http.MethodPut, "/api/preferences", p), http.StatusOK)
	rec = app.do(http.MethodGet, "/api/preferences", nil, "")
	if got := decode[preferences.Preferences](t, rec); got != p {
		t.Fatalf("preferences not persisted: %+v", got)
	}

	// The theme is applied server-side to avoid a flash on load.
	rec = app.do(http.MethodGet, "/", nil, "")
	if !strings.Contains(rec.Body.String(), `data-theme="dark"`) {
		t.Fatal("theme preference not applied to page")
	}

	p.FontSize = 500
	rec = app.json(http.MethodPut, "/api/preferences", p)
	expectStatus(t, rec, http.StatusUnprocessableEntity)
	if e := decode[errorResponse](t, rec); e.Field != "fontSize" {
		t.Fatalf("unexpected error %+v", e)
	}
}

func TestHealthz(t *testing.T) {
	app := newTestApp(t)
	rec := app.do(http.MethodGet, "/healthz", nil, "")
	expectStatus(t, rec, http.StatusOK)
}
