package handlers

import (
	"bytes"
	"errors"
	"html/template"
	"mime"
	"net/http"
	"time"

	"markdown-editor/internal/documents"
	"markdown-editor/internal/markdown"
	"markdown-editor/internal/middleware"
	"markdown-editor/internal/preferences"
)

type indexData struct {
	CSRFToken    string
	Prefs        preferences.Preferences
	FontFamilies []fontOption
	MaxFileBytes int
}

type fontOption struct {
	Key, Label string
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	var buf bytes.Buffer
	if err := s.pages.ExecuteTemplate(&buf, name, data); err != nil {
		s.logger.Error("template", "name", name, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	buf.WriteTo(w)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.prefs.Get(r.Context())
	if err != nil {
		s.logger.Error("load preferences", "err", err)
		prefs = preferences.Defaults()
	}
	s.render(w, r, "index.html", indexData{
		CSRFToken: middleware.CSRFToken(r),
		Prefs:     prefs,
		FontFamilies: []fontOption{
			{"mono", "Monospace"},
			{"sans", "Sans-serif"},
			{"serif", "Serif"},
		},
		MaxFileBytes: s.docs.MaxContentBytes(),
	})
}

type documentPage struct {
	Title       string
	Body        template.HTML
	DocumentCSS template.CSS
	CodeCSS     template.CSS
	Generated   string
}

func (s *Server) renderDocumentHTML(d documents.Document) (documentPage, error) {
	body, err := s.renderer.Render(d.Content)
	if err != nil {
		return documentPage{}, err
	}
	return documentPage{
		Title: d.Title,
		// body has been sanitized by the markdown renderer's allow-list.
		Body:        template.HTML(body),
		DocumentCSS: template.CSS(s.documentCSS),
		CodeCSS:     template.CSS(markdown.LightHighlightCSS()),
		Generated:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// handlePrint serves a print-optimized page of the document. The browser's
// print engine produces the PDF ("Save as PDF"), which faithfully renders
// headings, lists, tables and highlighted code blocks.
func (s *Server) handlePrint(w http.ResponseWriter, r *http.Request) {
	d, err := s.docs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainErrorPage(w, r, err)
		return
	}
	page, err := s.renderDocumentHTML(d)
	if err != nil {
		s.writeDomainErrorPage(w, r, err)
		return
	}
	s.render(w, r, "print.html", page)
}

func (s *Server) writeDomainErrorPage(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, documents.ErrInvalidID):
		http.Error(w, "invalid document id", http.StatusBadRequest)
	case errors.Is(err, documents.ErrNotFound):
		http.Error(w, "document not found", http.StatusNotFound)
	default:
		s.logger.Error("page failed", "path", r.URL.Path, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// handleExport downloads a document as Markdown or standalone HTML.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format != "md" && format != "html" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "format must be md or html", Field: "format"})
		return
	}
	d, err := s.docs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}

	var (
		body        []byte
		contentType string
		ext         string
	)
	switch format {
	case "md":
		body, contentType, ext = []byte(d.Content), "text/markdown; charset=utf-8", ".md"
	case "html":
		page, err := s.renderDocumentHTML(d)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		var buf bytes.Buffer
		if err := s.pages.ExecuteTemplate(&buf, "export.html", page); err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		body, contentType, ext = buf.Bytes(), "text/html; charset=utf-8", ".html"
	}

	filename := documents.ExportFilename(d.Title, ext)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Cache-Control", "no-store")
	// The download is never rendered inline; sandbox it anyway.
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src https: http: data:")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	p, err := s.prefs.Get(r.Context())
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	var p preferences.Preferences
	if !s.decodeJSON(w, r, &p) {
		return
	}
	saved, err := s.prefs.Update(r.Context(), p)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
