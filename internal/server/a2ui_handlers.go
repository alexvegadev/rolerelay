package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"rolerelay/internal/a2ui"
	"rolerelay/internal/domain"
	"rolerelay/internal/evaluator"
	"rolerelay/internal/store"
)

func (s *Server) handleGetApplicationA2UI(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid application ID.")
		return
	}

	ctx := r.Context()
	detail, err := s.opts.Store.GetApplicationDetailed(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Application not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "application_load_failed", err.Error())
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

	panel := a2ui.BuildPendingReviewPanel(detail, profile, cvs)
	writeJSON(w, http.StatusOK, map[string]any{
		"panel":       panel,
		"application": detail,
	})
}

func (s *Server) handlePostApplicationA2UIEvent(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid application ID.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var env a2ui.ActionEnvelope
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&env); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_a2ui_event", fmt.Sprintf("Invalid A2UI event payload: %v", err))
		return
	}
	if env.Version != "" && env.Version != a2ui.ProtocolVersion {
		writeError(w, http.StatusBadRequest, "unsupported_a2ui_version", fmt.Sprintf("Unsupported A2UI protocol version %q; expected %q.", env.Version, a2ui.ProtocolVersion))
		return
	}

	ctx := r.Context()
	detail, err := s.opts.Store.GetApplicationDetailed(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Application not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "application_load_failed", err.Error())
		return
	}
	if detail.Application.State != domain.StatePending {
		writeError(w, http.StatusConflict, "not_pending", "Only Pending applications can accept A2UI review answers.")
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

	// 1. Strictly validate submitted A2UI answers in Go.
	answers, summary, err := a2ui.ValidateAndExtractAnswers(env.Action, detail, profile, cvs)
	if err != nil {
		var valErr *a2ui.ValidationError
		if errors.As(err, &valErr) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"code":  "a2ui_validation_error",
				"field": valErr.Field,
				"error": valErr.Error(),
			})
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "a2ui_validation_error", err.Error())
		return
	}

	// 2. Persist validated answers to SQLite.
	if err := s.opts.Store.SaveApplicationA2UIAnswers(ctx, detail.Application.ID, answers, summary); err != nil {
		writeError(w, http.StatusInternalServerError, "a2ui_save_failed", err.Error())
		return
	}

	// 3. Reevaluate the application in Go before it can enter the submission queue.
	modelEval := s.resolveModelEvaluator(ctx)
	saveParams, err := evaluator.EvaluateVacancy(ctx, profile, cvs, detail.Vacancy, answers, modelEval)
	if err != nil {
		writeError(w, http.StatusBadGateway, "reevaluation_failed", err.Error())
		return
	}
	saveParams.StateSource = "a2ui"

	_, updatedApp, err := s.opts.Store.SaveEvaluationAndApplication(ctx, saveParams)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reevaluation_save_failed", err.Error())
		return
	}
	if updatedApp.QueuedForSend && s.opts.Worker != nil {
		s.opts.Worker.Notify()
	}

	refreshed, err := s.opts.Store.GetApplicationDetailed(ctx, detail.Application.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "application_load_failed", err.Error())
		return
	}
	panel := a2ui.BuildPendingReviewPanel(refreshed, profile, cvs)

	writeJSON(w, http.StatusOK, map[string]any{
		"message":     summary + " Go reevaluated the application.",
		"application": refreshed,
		"panel":       panel,
	})
}
