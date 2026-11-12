// Package handlers implements the HTTP layer: routing, request decoding and
// validation, and mapping domain errors to HTTP status codes. Business rules
// live in the documents and preferences packages.
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"markdown-editor/internal/documents"
	"markdown-editor/internal/markdown"
	"markdown-editor/internal/middleware"
	"markdown-editor/internal/preferences"
	"markdown-editor/web"
)

// Options configures a Server.
type Options struct {
	Documents     *documents.Service
	Preferences   *preferences.Service
	Renderer      *markdown.Renderer
	Logger        *slog.Logger
	SecureCookies bool
}

// Server holds the HTTP handlers' dependencies.
type Server struct {
	docs          *documents.Service
	prefs         *preferences.Service
	renderer      *markdown.Renderer
	logger        *slog.Logger
	secureCookies bool

	pages       *template.Template
	static      fs.FS
	documentCSS string
	// maxJSONBytes bounds JSON request bodies: the content limit plus room
	// for JSON escaping and the other fields.
	maxJSONBytes int64
}

// New builds a Server and parses templates.
func New(opts Options) (*Server, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	pages, err := template.New("").Funcs(template.FuncMap{
		"fontStack": func(key string) template.CSS { return template.CSS(preferences.FontFamilies[key]) },
	}).ParseFS(web.Templates, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	static, err := fs.Sub(web.Static, "static")
	if err != nil {
		return nil, err
	}
	docCSS, err := fs.ReadFile(static, "css/document.css")
	if err != nil {
		return nil, fmt.Errorf("read document.css: %w", err)
	}
	max := int64(opts.Documents.MaxContentBytes())
	return &Server{
		docs:          opts.Documents,
		prefs:         opts.Preferences,
		renderer:      opts.Renderer,
		logger:        opts.Logger,
		secureCookies: opts.SecureCookies,
		pages:         pages,
		static:        static,
		documentCSS:   string(docCSS),
		// JSON escaping can expand content up to 6x for control characters.
		maxJSONBytes: max*6 + 64<<10,
	}, nil
}

// Routes returns the application's HTTP handler with middleware applied.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Pages.
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /documents/{id}/print", s.handlePrint)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok"))
	})

	// Assets.
	staticHandler := http.StripPrefix("/static/", http.FileServerFS(s.static))
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		staticHandler.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /assets/highlight.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		io.WriteString(w, markdown.HighlightCSS())
	})
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, s.static, "images/logo.svg")
	})

	// Documents API.
	mux.HandleFunc("GET /api/documents", s.handleListDocuments)
	mux.HandleFunc("POST /api/documents", s.handleCreateDocument)
	mux.HandleFunc("GET /api/documents/recent", s.handleRecentDocuments)
	mux.HandleFunc("POST /api/documents/import", s.handleImport)
	mux.HandleFunc("GET /api/documents/{id}", s.handleGetDocument)
	mux.HandleFunc("PUT /api/documents/{id}", s.handleSaveDocument)
	mux.HandleFunc("PATCH /api/documents/{id}", s.handleRenameDocument)
	mux.HandleFunc("DELETE /api/documents/{id}", s.handleDeleteDocument)
	mux.HandleFunc("POST /api/documents/{id}/duplicate", s.handleDuplicateDocument)
	mux.HandleFunc("GET /api/documents/{id}/export", s.handleExport)

	// Rendering and preferences.
	mux.HandleFunc("POST /api/render", s.handleRender)
	mux.HandleFunc("GET /api/preferences", s.handleGetPreferences)
	mux.HandleFunc("PUT /api/preferences", s.handleUpdatePreferences)

	return middleware.Chain(mux,
		middleware.Recover(s.logger),
		middleware.Logger(s.logger),
		middleware.SecurityHeaders,
		middleware.CSRF(s.secureCookies),
	)
}

// ---- helpers ----

type errorResponse struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// writeDomainError maps service errors to HTTP responses.
func (s *Server) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		dverr *documents.ValidationError
		pverr *preferences.ValidationError
	)
	switch {
	case errors.Is(err, documents.ErrInvalidID):
		writeError(w, http.StatusBadRequest, "invalid document id")
	case errors.Is(err, documents.ErrNotFound):
		writeError(w, http.StatusNotFound, "document not found")
	case errors.Is(err, documents.ErrContentTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("document exceeds the %s size limit", humanBytes(s.docs.MaxContentBytes())))
	case errors.As(err, &dverr):
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: dverr.Error(), Field: dverr.Field})
	case errors.As(err, &pverr):
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: pverr.Error(), Field: pverr.Field})
	default:
		s.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// decodeJSON decodes a single JSON object from the request body, enforcing
// the content type, a size limit and rejecting unknown fields. It writes the
// error response itself and returns false on failure.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxJSONBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+sanitizeJSONError(err))
		return false
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "invalid JSON body: unexpected data after object")
		return false
	}
	return true
}

func sanitizeJSONError(err error) string {
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return strings.TrimPrefix(msg, "json: ")
}
