package handlers

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"markdown-editor/internal/documents"
)

type listResponse struct {
	Documents []documents.Summary `json:"documents"`
}

// parseSort converts "field-direction" (e.g. "title-asc") into list options.
func parseSort(v string) (documents.SortField, bool, bool) {
	if v == "" {
		return documents.SortUpdated, true, true
	}
	field, dir, ok := strings.Cut(v, "-")
	if !ok || (dir != "asc" && dir != "desc") {
		return "", false, false
	}
	switch documents.SortField(field) {
	case documents.SortUpdated, documents.SortCreated, documents.SortTitle:
		return documents.SortField(field), dir == "desc", true
	}
	return "", false, false
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	field, desc, ok := parseSort(r.URL.Query().Get("sort"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid sort parameter", Field: "sort"})
		return
	}
	list, err := s.docs.List(r.Context(), documents.ListOptions{
		Query:      r.URL.Query().Get("q"),
		Sort:       field,
		Descending: desc,
	})
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Documents: list})
}

func (s *Server) handleRecentDocuments(w http.ResponseWriter, r *http.Request) {
	list, err := s.docs.Recent(r.Context())
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Documents: list})
}

type createRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func (s *Server) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	d, err := s.docs.Create(r.Context(), req.Title, req.Content)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/documents/"+d.ID)
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	d, err := s.docs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type saveRequest struct {
	Title   *string `json:"title"`
	Content *string `json:"content"`
	Version *int64  `json:"version"`
}

type saveResponse struct {
	Document documents.Document `json:"document"`
	Changed  bool               `json:"changed"`
}

type conflictResponse struct {
	Error    string             `json:"error"`
	Document documents.Document `json:"document"`
}

// handleSaveDocument is used by autosave and manual save. The client sends
// the version it last saw; a mismatch yields 409 with the current document.
func (s *Server) handleSaveDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !documents.ValidID(id) {
		writeError(w, http.StatusBadRequest, "invalid document id")
		return
	}
	var req saveRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	switch {
	case req.Title == nil:
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "title is required", Field: "title"})
		return
	case req.Content == nil:
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "content is required", Field: "content"})
		return
	case req.Version == nil:
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "version is required", Field: "version"})
		return
	}
	res, err := s.docs.Save(r.Context(), id, *req.Version, *req.Title, *req.Content)
	if errors.Is(err, documents.ErrConflict) {
		writeJSON(w, http.StatusConflict, conflictResponse{
			Error:    "this document was changed elsewhere since you opened it",
			Document: res.Document,
		})
		return
	}
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, saveResponse{Document: res.Document, Changed: res.Changed})
}

type renameRequest struct {
	Title *string `json:"title"`
}

func (s *Server) handleRenameDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !documents.ValidID(id) {
		writeError(w, http.StatusBadRequest, "invalid document id")
		return
	}
	var req renameRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Title == nil {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "title is required", Field: "title"})
		return
	}
	d, err := s.docs.Rename(r.Context(), id, *req.Title)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	if err := s.docs.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDuplicateDocument(w http.ResponseWriter, r *http.Request) {
	d, err := s.docs.Duplicate(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/documents/"+d.ID)
	writeJSON(w, http.StatusCreated, d)
}

// handleImport accepts a multipart upload with a single "file" field.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	max := int64(s.docs.MaxContentBytes())
	// Allow some headroom for multipart headers and boundaries.
	r.Body = http.MaxBytesReader(w, r.Body, max+64<<10)

	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be multipart/form-data")
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart body")
		return
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "no file uploaded", Field: "file"})
			return
		}
		if err != nil {
			s.writeUploadError(w, err)
			return
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		// Read one byte past the limit to detect oversized files.
		data, err := io.ReadAll(io.LimitReader(part, max+1))
		part.Close()
		if err != nil {
			s.writeUploadError(w, err)
			return
		}
		if int64(len(data)) > max {
			s.writeDomainError(w, r, documents.ErrContentTooLarge)
			return
		}
		d, err := s.docs.Import(r.Context(), part.FileName(), data)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		w.Header().Set("Location", "/api/documents/"+d.ID)
		writeJSON(w, http.StatusCreated, d)
		return
	}
}

func (s *Server) writeUploadError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeError(w, http.StatusRequestEntityTooLarge, "uploaded file is too large")
		return
	}
	writeError(w, http.StatusBadRequest, "could not read uploaded file")
}

type renderRequest struct {
	Content string `json:"content"`
}

type renderResponse struct {
	HTML string `json:"html"`
}

// handleRender renders Markdown for the live preview.
func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	var req renderRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Content) > s.docs.MaxContentBytes() {
		s.writeDomainError(w, r, documents.ErrContentTooLarge)
		return
	}
	out, err := s.renderer.Render(req.Content)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, renderResponse{HTML: out})
}
