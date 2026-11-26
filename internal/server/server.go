package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"rolerelay/internal/config"
	"rolerelay/internal/connectors"
	"rolerelay/internal/credstore"
	"rolerelay/internal/domain"
	"rolerelay/internal/evaluator"
	"rolerelay/internal/mailer"
	"rolerelay/internal/pdfextract"
	"rolerelay/internal/store"
	"rolerelay/internal/webui"
	"rolerelay/internal/worker"
)

type Options struct {
	Version        string
	StoragePaths   config.StoragePaths
	SchemaVersion  int
	Address        string
	Store          *store.Store
	Worker         *worker.Worker
	CredStore      credstore.Store
	Mailer         mailer.Mailer
	Greenhouse     *connectors.GreenhouseClient
	Lever          *connectors.LeverClient
	ModelEvaluator evaluator.ModelEvaluator
	OnExit         func()
}

type Server struct {
	opts   Options
	router http.Handler
}

func New(opts Options) *Server {
	s := &Server{opts: opts}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/profile", s.handleGetProfile)
	mux.HandleFunc("PUT /api/profile", s.handleUpdateProfile)
	mux.HandleFunc("GET /api/cvs", s.handleListCVs)
	mux.HandleFunc("POST /api/cvs", s.handleUploadCV)
	mux.HandleFunc("GET /api/cvs/{id}", s.handleGetCV)
	mux.HandleFunc("GET /api/cvs/{id}/file", s.handleGetCVFile)
	mux.HandleFunc("POST /api/cvs/{id}/activate", s.handleActivateCV)
	mux.HandleFunc("DELETE /api/cvs/{id}", s.handleDeleteCV)
	mux.HandleFunc("GET /api/boards", s.handleListBoards)
	mux.HandleFunc("POST /api/boards", s.handleAddBoard)
	mux.HandleFunc("POST /api/boards/{id}/sync", s.handleSyncBoard)
	mux.HandleFunc("DELETE /api/boards/{id}", s.handleDeleteBoard)
	mux.HandleFunc("GET /api/vacancies", s.handleListVacancies)
	mux.HandleFunc("POST /api/vacancies", s.handleCreateVacancy)
	mux.HandleFunc("GET /api/vacancies/{id}", s.handleGetVacancy)
	mux.HandleFunc("DELETE /api/vacancies/{id}", s.handleDeleteVacancy)
	mux.HandleFunc("POST /api/vacancies/{id}/evaluate", s.handleEvaluateVacancy)
	mux.HandleFunc("GET /api/settings/model", s.handleGetModelSettings)
	mux.HandleFunc("PUT /api/settings/model", s.handleUpdateModelSettings)
	mux.HandleFunc("GET /api/applications", s.handleListApplications)
	mux.HandleFunc("GET /api/applications/{id}", s.handleGetApplication)
	mux.HandleFunc("POST /api/applications/{id}/queue", s.handleQueueApplication)
	mux.HandleFunc("POST /api/applications/{id}/state", s.handleUpdateApplicationState)
	mux.HandleFunc("GET /api/settings/email", s.handleGetEmailSettings)
	mux.HandleFunc("PUT /api/settings/email", s.handleUpdateEmailSettings)
	mux.HandleFunc("DELETE /api/settings/email/credentials", s.handleDeleteEmailCredentials)
	mux.HandleFunc("POST /api/settings/email/test", s.handleTestEmailConnection)
	mux.HandleFunc("POST /api/worker/pause", s.handleWorkerPause)
	mux.HandleFunc("POST /api/worker/resume", s.handleWorkerResume)
	mux.HandleFunc("POST /api/app/exit", s.handleExit)

	mux.Handle("/", webui.Handler())

	s.router = s.withMiddleware(mux)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")

		// Reject cross-site mutating requests targeting the local loopback server.
		if isMutatingMethod(r.Method) {
			if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && !isLoopbackOrigin(origin) {
				writeError(w, http.StatusForbidden, "forbidden_origin", "Cross-origin write requests are not permitted.")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	var workerStatus worker.Status
	if s.opts.Worker != nil {
		workerStatus = s.opts.Worker.Status()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"appName":       config.AppName,
		"version":       s.opts.Version,
		"storageMode":   s.opts.StoragePaths.Mode,
		"dataDir":       s.opts.StoragePaths.DataDir,
		"dbPath":        s.opts.StoragePaths.DBPath,
		"schemaVersion": s.opts.SchemaVersion,
		"address":       s.opts.Address,
		"worker":        workerStatus,
	})
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	profile, err := s.opts.Store.GetOrCreateProfile(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile_load_failed", err.Error())
		return
	}
	cvs, err := s.opts.Store.ListCVs(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cv_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":   profile,
		"cvs":       cvs,
		"readiness": store.ComputeReadiness(profile, cvs),
	})
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload domain.ProfileUpdate
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", fmt.Sprintf("Invalid profile payload: %v", err))
		return
	}

	ctx := r.Context()
	profile, err := s.opts.Store.UpdateProfile(ctx, payload)
	if err != nil {
		var valErr *store.ValidationError
		if errors.As(err, &valErr) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"code":  "validation_error",
				"field": valErr.Field,
				"error": valErr.Error(),
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "profile_update_failed", err.Error())
		return
	}

	cvs, err := s.opts.Store.ListCVs(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cv_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":   profile,
		"cvs":       cvs,
		"readiness": store.ComputeReadiness(profile, cvs),
	})
}

func (s *Server) handleListCVs(w http.ResponseWriter, r *http.Request) {
	cvs, err := s.opts.Store.ListCVs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cv_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cvs": cvs})
}

func (s *Server) handleUploadCV(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, pdfextract.MaxPDFSizeBytes+1<<20)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", "Unable to read uploaded file. Ensure the PDF is under 20 MB.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	lang := domain.CVLanguage(strings.TrimSpace(strings.ToLower(r.FormValue("language"))))
	if !lang.Valid() {
		writeError(w, http.StatusUnprocessableEntity, "invalid_language", "Select a CV language: English (en) or Spanish (es).")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "Choose a PDF file to upload.")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, pdfextract.MaxPDFSizeBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_failed", "Could not read the uploaded file.")
		return
	}
	if len(data) > pdfextract.MaxPDFSizeBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", pdfextract.ErrFileTooLarge.Error())
		return
	}

	filename := sanitizeFilename(header.Filename)
	extracted, err := pdfextract.Extract(data)
	if err != nil {
		switch {
		case errors.Is(err, pdfextract.ErrNoExtractableText):
			writeError(w, http.StatusUnprocessableEntity, "no_extractable_text", "This PDF has no extractable text. Export your CV as a text-based PDF (not a scanned image) and try again.")
		case errors.Is(err, pdfextract.ErrInvalidPDF):
			writeError(w, http.StatusUnprocessableEntity, "invalid_pdf", "The uploaded file is not a valid PDF document.")
		case errors.Is(err, pdfextract.ErrEmptyFile):
			writeError(w, http.StatusUnprocessableEntity, "empty_file", "The uploaded PDF file is empty.")
		default:
			writeError(w, http.StatusUnprocessableEntity, "pdf_extract_failed", err.Error())
		}
		return
	}

	ctx := r.Context()
	saved, err := s.opts.Store.SaveCV(ctx, store.SaveCVParams{
		Language:      lang,
		Filename:      filename,
		ContentType:   "application/pdf",
		PageCount:     extracted.PageCount,
		ExtractedText: extracted.Text,
		Content:       data,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cv_save_failed", err.Error())
		return
	}

	profile, err := s.opts.Store.GetOrCreateProfile(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile_load_failed", err.Error())
		return
	}
	cvs, err := s.opts.Store.ListCVs(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cv_list_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"cv":        saved,
		"cvs":       cvs,
		"readiness": store.ComputeReadiness(profile, cvs),
	})
}

func (s *Server) handleGetCV(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid CV ID.")
		return
	}

	doc, err := s.opts.Store.GetCV(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "CV not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cv_load_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cv": doc})
}

func (s *Server) handleGetCVFile(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid CV ID.")
		return
	}

	file, err := s.opts.Store.GetCVFile(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "CV file not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cv_file_failed", err.Error())
		return
	}

	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	safeName := sanitizeFilename(file.Filename)
	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, safeName))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(file.Content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file.Content)
}

func (s *Server) handleActivateCV(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid CV ID.")
		return
	}

	ctx := r.Context()
	doc, err := s.opts.Store.ActivateCV(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "CV not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cv_activate_failed", err.Error())
		return
	}

	profile, err := s.opts.Store.GetOrCreateProfile(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile_load_failed", err.Error())
		return
	}
	cvs, err := s.opts.Store.ListCVs(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cv_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cv":        doc,
		"cvs":       cvs,
		"readiness": store.ComputeReadiness(profile, cvs),
	})
}

func (s *Server) handleDeleteCV(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid CV ID.")
		return
	}

	ctx := r.Context()
	if err := s.opts.Store.DeleteCV(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "CV not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, "cv_delete_failed", err.Error())
		return
	}

	profile, err := s.opts.Store.GetOrCreateProfile(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile_load_failed", err.Error())
		return
	}
	cvs, err := s.opts.Store.ListCVs(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cv_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cvs":       cvs,
		"readiness": store.ComputeReadiness(profile, cvs),
	})
}

func (s *Server) handleWorkerPause(w http.ResponseWriter, _ *http.Request) {
	if s.opts.Worker == nil {
		writeError(w, http.StatusServiceUnavailable, "worker_unavailable", "Worker is not active.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"worker": s.opts.Worker.Pause()})
}

func (s *Server) handleWorkerResume(w http.ResponseWriter, _ *http.Request) {
	if s.opts.Worker == nil {
		writeError(w, http.StatusServiceUnavailable, "worker_unavailable", "Worker is not active.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"worker": s.opts.Worker.Resume()})
}

func (s *Server) handleExit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]any{
		"exiting": true,
		"message": "RoleRelay is shutting down. Queued work will resume on the next launch.",
	})
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	if s.opts.OnExit != nil {
		go func() {
			time.Sleep(150 * time.Millisecond)
			s.opts.OnExit()
		}()
	}
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func sanitizeFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.NewReplacer(`"`, "", "\r", "", "\n", "", `\`, "_", "/", "_").Replace(base)
	if base == "" || base == "." || base == ".." {
		return "cv.pdf"
	}
	return base
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"code":  code,
		"error": message,
	})
}
