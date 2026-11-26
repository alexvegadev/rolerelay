package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"rolerelay/internal/connectors"
	"rolerelay/internal/domain"
	"rolerelay/internal/evaluator"
	"rolerelay/internal/store"
)

func (s *Server) handleListBoards(w http.ResponseWriter, r *http.Request) {
	boards, err := s.opts.Store.ListJobBoards(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "boards_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": boards})
}

func (s *Server) handleAddBoard(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload struct {
		Provider     domain.JobBoardProvider `json:"provider"`
		CompanyBoard string                  `json:"companyBoard"`
		DisplayName  string                  `json:"displayName"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", fmt.Sprintf("Invalid job board payload: %v", err))
		return
	}

	board, err := s.opts.Store.AddJobBoard(r.Context(), payload.Provider, payload.CompanyBoard, payload.DisplayName)
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
		writeError(w, http.StatusInternalServerError, "board_save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"board": board})
}

func (s *Server) handleDeleteBoard(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid board ID.")
		return
	}
	if err := s.opts.Store.DeleteJobBoard(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Job board not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, "board_delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleSyncBoard(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid board ID.")
		return
	}

	ctx := r.Context()
	board, err := s.opts.Store.GetJobBoard(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Job board not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "board_load_failed", err.Error())
		return
	}

	var imported []connectors.ImportedVacancy
	switch board.Provider {
	case domain.ProviderGreenhouse:
		client := s.opts.Greenhouse
		if client == nil {
			client = connectors.NewGreenhouseClient()
		}
		imported, err = client.FetchJobs(ctx, board.CompanyBoard, board.DisplayName)
	case domain.ProviderLever:
		client := s.opts.Lever
		if client == nil {
			client = connectors.NewLeverClient()
		}
		imported, err = client.FetchJobs(ctx, board.CompanyBoard, board.DisplayName)
	default:
		writeError(w, http.StatusBadRequest, "unsupported_provider", "Unsupported job board provider.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "board_sync_failed", err.Error())
		return
	}

	createdCount := 0
	dedupCount := 0
	for _, item := range imported {
		res, err := s.opts.Store.UpsertImportedVacancy(ctx, item)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "vacancy_import_failed", err.Error())
			return
		}
		if res.Created {
			createdCount++
		} else if res.Deduplicated {
			dedupCount++
		}
	}

	_ = s.opts.Store.MarkJobBoardSynced(ctx, board.ID)
	updatedBoard, _ := s.opts.Store.GetJobBoard(ctx, board.ID)
	vacancies, err := s.opts.Store.ListVacanciesWithEvaluations(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "vacancies_list_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"board":        updatedBoard,
		"fetched":      len(imported),
		"created":      createdCount,
		"deduplicated": dedupCount,
		"vacancies":    vacancies,
	})
}

func (s *Server) handleListVacancies(w http.ResponseWriter, r *http.Request) {
	items, err := s.opts.Store.ListVacanciesWithEvaluations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "vacancies_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vacancies": items})
}

func (s *Server) handleCreateVacancy(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var input domain.ManualVacancyInput
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", fmt.Sprintf("Invalid vacancy payload: %v", err))
		return
	}

	ctx := r.Context()
	res, err := s.opts.Store.CreateManualVacancy(ctx, input)
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
		writeError(w, http.StatusInternalServerError, "vacancy_create_failed", err.Error())
		return
	}

	hydrated, err := s.opts.Store.HydrateVacancy(ctx, res.Vacancy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "vacancy_load_failed", err.Error())
		return
	}
	status := http.StatusCreated
	if res.Deduplicated {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"item":         hydrated,
		"created":      res.Created,
		"deduplicated": res.Deduplicated,
		"matchedBy":    res.MatchedBy,
	})
}

func (s *Server) handleGetVacancy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid vacancy ID.")
		return
	}
	item, err := s.opts.Store.GetVacancyWithEvaluation(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Vacancy not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "vacancy_load_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) handleDeleteVacancy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid vacancy ID.")
		return
	}
	if err := s.opts.Store.DeleteVacancy(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Vacancy not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, "vacancy_delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleEvaluateVacancy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid vacancy ID.")
		return
	}

	ctx := r.Context()
	vacancy, err := s.opts.Store.GetVacancy(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Vacancy not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "vacancy_load_failed", err.Error())
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

	var existingAnswers map[string]any
	if existingApp, err := s.opts.Store.GetApplicationByVacancy(ctx, vacancy.ID); err == nil {
		existingAnswers = existingApp.A2UIAnswers
	}

	modelEval := s.resolveModelEvaluator(ctx)
	saveParams, err := evaluator.EvaluateVacancy(ctx, profile, cvs, vacancy, existingAnswers, modelEval)
	if err != nil {
		writeError(w, http.StatusBadGateway, "evaluation_failed", err.Error())
		return
	}

	if _, app, err := s.opts.Store.SaveEvaluationAndApplication(ctx, saveParams); err != nil {
		writeError(w, http.StatusInternalServerError, "evaluation_save_failed", err.Error())
		return
	} else if app.QueuedForSend && s.opts.Worker != nil {
		s.opts.Worker.Notify()
	}

	item, err := s.opts.Store.GetVacancyWithEvaluation(ctx, vacancy.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "vacancy_load_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) resolveModelEvaluator(ctx context.Context) evaluator.ModelEvaluator {
	if s.opts.ModelEvaluator != nil {
		return s.opts.ModelEvaluator
	}
	cfg, err := s.opts.Store.GetModelSettings(ctx)
	if err == nil && cfg.Provider == "openai_compatible" && cfg.EndpointURL != "" && cfg.Model != "" {
		return evaluator.NewOpenAICompatibleEvaluator(cfg)
	}
	return evaluator.NewLocalHeuristicEvaluator()
}

func (s *Server) handleGetModelSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.opts.Store.GetModelSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings_load_failed", err.Error())
		return
	}
	// Never return stored API key in plaintext over GET.
	cfg.APIKey = ""
	writeJSON(w, http.StatusOK, map[string]any{"settings": cfg})
}

func (s *Server) handleUpdateModelSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload domain.ModelSettings
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", fmt.Sprintf("Invalid model settings payload: %v", err))
		return
	}

	saved, err := s.opts.Store.SaveModelSettings(r.Context(), payload)
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
		writeError(w, http.StatusInternalServerError, "settings_save_failed", err.Error())
		return
	}
	saved.APIKey = ""
	writeJSON(w, http.StatusOK, map[string]any{"settings": saved})
}
