package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"rolerelay/internal/connectors"
	"rolerelay/internal/domain"
	"rolerelay/internal/langselect"
)

func (s *Store) AddJobBoard(ctx context.Context, provider domain.JobBoardProvider, companyBoard, displayName string) (domain.JobBoard, error) {
	if !provider.ValidBoard() {
		return domain.JobBoard{}, &ValidationError{
			Field:   "provider",
			Message: "provider must be 'greenhouse' or 'lever'",
		}
	}
	board := strings.ToLower(strings.TrimSpace(companyBoard))
	if board == "" {
		return domain.JobBoard{}, &ValidationError{
			Field:   "companyBoard",
			Message: "company board token is required",
		}
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = board
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_boards (provider, company_board, display_name, enabled, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(provider, company_board) DO UPDATE SET
			display_name = excluded.display_name,
			enabled = 1,
			updated_at = excluded.updated_at
	`, string(provider), board, name, now, now)
	if err != nil {
		return domain.JobBoard{}, fmt.Errorf("upsert job board: %w", err)
	}

	return s.getJobBoardByKey(ctx, provider, board)
}

func (s *Store) ListJobBoards(ctx context.Context) ([]domain.JobBoard, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider, company_board, display_name, enabled, last_synced_at, created_at, updated_at
		FROM job_boards
		ORDER BY provider ASC, company_board ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list job boards: %w", err)
	}
	defer rows.Close()

	out := make([]domain.JobBoard, 0)
	for rows.Next() {
		b, err := scanJobBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetJobBoard(ctx context.Context, id int64) (domain.JobBoard, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, provider, company_board, display_name, enabled, last_synced_at, created_at, updated_at
		FROM job_boards
		WHERE id = ?
	`, id)
	b, err := scanJobBoard(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.JobBoard{}, ErrNotFound
	}
	return b, err
}

func (s *Store) getJobBoardByKey(ctx context.Context, provider domain.JobBoardProvider, board string) (domain.JobBoard, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, provider, company_board, display_name, enabled, last_synced_at, created_at, updated_at
		FROM job_boards
		WHERE provider = ? AND company_board = ?
	`, string(provider), board)
	return scanJobBoard(row)
}

func (s *Store) MarkJobBoardSynced(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE job_boards SET last_synced_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

func (s *Store) DeleteJobBoard(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM job_boards WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete job board %d: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type UpsertVacancyResult struct {
	Vacancy      domain.Vacancy `json:"vacancy"`
	Created      bool           `json:"created"`
	Deduplicated bool           `json:"deduplicated"`
	MatchedBy    string         `json:"matchedBy,omitempty"` // "external_id" | "canonical_url"
}

func (s *Store) CreateManualVacancy(ctx context.Context, input domain.ManualVacancyInput) (UpsertVacancyResult, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return UpsertVacancyResult{}, &ValidationError{Field: "title", Message: "job title is required"}
	}
	desc := strings.TrimSpace(input.Description)
	if desc == "" {
		return UpsertVacancyResult{}, &ValidationError{Field: "description", Message: "job description is required"}
	}

	arrangement := input.WorkArrangement
	if arrangement == "" || !arrangement.ValidVacancy() {
		arrangement = connectors.InferWorkArrangement(input.Location, desc)
	}

	appURL := strings.TrimSpace(input.ApplicationURL)
	canonicalURL := connectors.CanonicalizeURL(appURL)

	email := strings.ToLower(strings.TrimSpace(input.ApplicationEmail))
	if email == "" {
		email = connectors.ExtractApplicationEmail(desc)
	}

	reqLang := strings.ToLower(strings.TrimSpace(input.RequestedLanguage))
	if reqLang != "" && reqLang != "en" && reqLang != "es" && reqLang != "both" {
		return UpsertVacancyResult{}, &ValidationError{Field: "requestedLanguage", Message: "must be empty, en, es, or both"}
	}
	if reqLang == "" {
		reqLang = langselect.DetectRequestedLanguage(title + "\n" + desc)
	}
	detectedLang := langselect.DetectListingLanguage(title, desc)

	return s.UpsertVacancy(ctx, domain.Vacancy{
		SourceType:        domain.ProviderManual,
		Provider:          string(domain.ProviderManual),
		CompanyBoard:      "",
		ExternalJobID:     "",
		CanonicalURL:      canonicalURL,
		CompanyName:       strings.TrimSpace(input.CompanyName),
		Title:             title,
		Location:          strings.TrimSpace(input.Location),
		WorkArrangement:   arrangement,
		Description:       desc,
		RequestedLanguage: reqLang,
		DetectedLanguage:  detectedLang,
		ApplicationEmail:  email,
		ApplicationURL:    appURL,
		RawJSON:           "{}",
	})
}

func (s *Store) UpsertImportedVacancy(ctx context.Context, imp connectors.ImportedVacancy) (UpsertVacancyResult, error) {
	reqLang := langselect.DetectRequestedLanguage(imp.Title + "\n" + imp.Description)
	detectedLang := langselect.DetectListingLanguage(imp.Title, imp.Description)

	return s.UpsertVacancy(ctx, domain.Vacancy{
		SourceType:        imp.SourceType,
		Provider:          imp.Provider,
		CompanyBoard:      imp.CompanyBoard,
		ExternalJobID:     imp.ExternalJobID,
		CanonicalURL:      imp.CanonicalURL,
		CompanyName:       imp.CompanyName,
		Title:             imp.Title,
		Location:          imp.Location,
		WorkArrangement:   imp.WorkArrangement,
		Description:       imp.Description,
		RequestedLanguage: reqLang,
		DetectedLanguage:  detectedLang,
		ApplicationEmail:  imp.ApplicationEmail,
		ApplicationURL:    imp.ApplicationURL,
		RawJSON:           imp.RawJSON,
	})
}

func (s *Store) UpsertVacancy(ctx context.Context, v domain.Vacancy) (UpsertVacancyResult, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// 1. Check deduplication by (provider, company_board, external_job_id).
	var existingID int64
	matchedBy := ""
	if v.ExternalJobID != "" {
		err := s.db.QueryRowContext(ctx, `
			SELECT id FROM vacancies
			WHERE provider = ? AND company_board = ? AND external_job_id = ?
			LIMIT 1
		`, v.Provider, v.CompanyBoard, v.ExternalJobID).Scan(&existingID)
		if err == nil {
			matchedBy = "external_id"
		} else if !errors.Is(err, sql.ErrNoRows) {
			return UpsertVacancyResult{}, fmt.Errorf("check external id dedupe: %w", err)
		}
	}

	// 2. Link duplicates across sources using canonical_url when available.
	if existingID == 0 && v.CanonicalURL != "" {
		err := s.db.QueryRowContext(ctx, `
			SELECT id FROM vacancies
			WHERE canonical_url = ?
			LIMIT 1
		`, v.CanonicalURL).Scan(&existingID)
		if err == nil {
			matchedBy = "canonical_url"
		} else if !errors.Is(err, sql.ErrNoRows) {
			return UpsertVacancyResult{}, fmt.Errorf("check canonical url dedupe: %w", err)
		}
	}

	if existingID > 0 {
		// Merge fresh metadata without losing existing email/external IDs.
		_, err := s.db.ExecContext(ctx, `
			UPDATE vacancies
			SET
				company_name = CASE WHEN ? != '' THEN ? ELSE company_name END,
				title = CASE WHEN ? != '' THEN ? ELSE title END,
				location = CASE WHEN ? != '' THEN ? ELSE location END,
				work_arrangement = CASE WHEN ? != 'unknown' AND ? != '' THEN ? ELSE work_arrangement END,
				description = CASE WHEN ? != '' THEN ? ELSE description END,
				requested_language = CASE WHEN ? != '' THEN ? ELSE requested_language END,
				detected_language = CASE WHEN ? != '' THEN ? ELSE detected_language END,
				application_email = CASE WHEN ? != '' THEN ? ELSE application_email END,
				application_url = CASE WHEN ? != '' THEN ? ELSE application_url END,
				canonical_url = CASE WHEN ? != '' THEN ? ELSE canonical_url END,
				updated_at = ?
			WHERE id = ?
		`,
			v.CompanyName, v.CompanyName,
			v.Title, v.Title,
			v.Location, v.Location,
			string(v.WorkArrangement), string(v.WorkArrangement), string(v.WorkArrangement),
			v.Description, v.Description,
			v.RequestedLanguage, v.RequestedLanguage,
			v.DetectedLanguage, v.DetectedLanguage,
			v.ApplicationEmail, v.ApplicationEmail,
			v.ApplicationURL, v.ApplicationURL,
			v.CanonicalURL, v.CanonicalURL,
			now, existingID,
		)
		if err != nil {
			return UpsertVacancyResult{}, fmt.Errorf("update deduplicated vacancy %d: %w", existingID, err)
		}
		updated, err := s.GetVacancy(ctx, existingID)
		if err != nil {
			return UpsertVacancyResult{}, err
		}
		return UpsertVacancyResult{
			Vacancy:      updated,
			Created:      false,
			Deduplicated: true,
			MatchedBy:    matchedBy,
		}, nil
	}

	var extID any
	if v.ExternalJobID != "" {
		extID = v.ExternalJobID
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO vacancies (
			source_type, provider, company_board, external_job_id, canonical_url,
			company_name, title, location, work_arrangement, description,
			requested_language, detected_language, application_email, application_url,
			raw_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		string(v.SourceType), v.Provider, v.CompanyBoard, extID, v.CanonicalURL,
		v.CompanyName, v.Title, v.Location, string(v.WorkArrangement), v.Description,
		v.RequestedLanguage, v.DetectedLanguage, v.ApplicationEmail, v.ApplicationURL,
		v.RawJSON, now, now,
	)
	if err != nil {
		return UpsertVacancyResult{}, fmt.Errorf("insert vacancy: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return UpsertVacancyResult{}, fmt.Errorf("read vacancy id: %w", err)
	}
	created, err := s.GetVacancy(ctx, id)
	if err != nil {
		return UpsertVacancyResult{}, err
	}
	return UpsertVacancyResult{
		Vacancy: created,
		Created: true,
	}, nil
}

func (s *Store) GetVacancy(ctx context.Context, id int64) (domain.Vacancy, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id, source_type, provider, company_board, COALESCE(external_job_id, ''), canonical_url,
			company_name, title, location, work_arrangement, description,
			requested_language, detected_language, application_email, application_url,
			raw_json, created_at, updated_at
		FROM vacancies
		WHERE id = ?
	`, id)
	v, err := scanVacancy(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Vacancy{}, ErrNotFound
	}
	return v, err
}

func (s *Store) DeleteVacancy(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM vacancies WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete vacancy %d: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListVacanciesWithEvaluations(ctx context.Context) ([]domain.VacancyWithEvaluation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, source_type, provider, company_board, COALESCE(external_job_id, ''), canonical_url,
			company_name, title, location, work_arrangement, description,
			requested_language, detected_language, application_email, application_url,
			raw_json, created_at, updated_at
		FROM vacancies
		ORDER BY updated_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list vacancies: %w", err)
	}
	defer rows.Close()

	vacancies := make([]domain.Vacancy, 0)
	for rows.Next() {
		v, err := scanVacancy(rows)
		if err != nil {
			return nil, err
		}
		vacancies = append(vacancies, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]domain.VacancyWithEvaluation, 0, len(vacancies))
	for _, v := range vacancies {
		item, err := s.HydrateVacancy(ctx, v)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Store) GetVacancyWithEvaluation(ctx context.Context, id int64) (domain.VacancyWithEvaluation, error) {
	v, err := s.GetVacancy(ctx, id)
	if err != nil {
		return domain.VacancyWithEvaluation{}, err
	}
	return s.HydrateVacancy(ctx, v)
}

func (s *Store) HydrateVacancy(ctx context.Context, v domain.Vacancy) (domain.VacancyWithEvaluation, error) {
	item := domain.VacancyWithEvaluation{Vacancy: v}

	app, err := s.GetApplicationByVacancy(ctx, v.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return domain.VacancyWithEvaluation{}, err
	}
	if err == nil {
		item.Application = &app
		history, err := s.ListStateChanges(ctx, app.ID)
		if err != nil {
			return domain.VacancyWithEvaluation{}, err
		}
		item.StateHistory = history
	}

	eval, err := s.GetLatestEvaluationByVacancy(ctx, v.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return domain.VacancyWithEvaluation{}, err
	}
	if err == nil {
		item.LatestEvaluation = &eval
	}

	return item, nil
}

type SaveEvaluationParams struct {
	Evaluation       domain.Evaluation
	SelectedCVRoles  []string
	ApplicationState domain.ApplicationState
	Channel          string
	Recipient        string
	Subject          string
	Body             string
	PendingReason    string
	DiscardReason    string
	ExternalApplyURL string
	QueuedForSend    bool
	StateReason      string
	StateSource      string
}

func (s *Store) SaveEvaluationAndApplication(ctx context.Context, params SaveEvaluationParams) (domain.Evaluation, domain.Application, error) {
	if _, err := s.GetOrCreateProfile(ctx); err != nil {
		return domain.Evaluation{}, domain.Application{}, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	selectedIDsJSON, _ := json.Marshal(params.Evaluation.SelectedCVIDs)
	findingsJSON, _ := json.Marshal(params.Evaluation.Findings)
	missingJSON, _ := json.Marshal(params.Evaluation.MissingAnswers)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Evaluation{}, domain.Application{}, fmt.Errorf("begin evaluation tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO evaluations (
			vacancy_id, profile_id, selected_cv_ids_json, model_name,
			fit_score, passed_filters, passed_threshold, language_status,
			summary, findings_json, missing_answers_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		params.Evaluation.VacancyID,
		DefaultProfileID,
		string(selectedIDsJSON),
		params.Evaluation.ModelName,
		params.Evaluation.FitScore,
		boolToInt(params.Evaluation.PassedFilters),
		boolToInt(params.Evaluation.PassedThreshold),
		string(params.Evaluation.LanguageStatus),
		params.Evaluation.Summary,
		string(findingsJSON),
		string(missingJSON),
		nowStr,
	)
	if err != nil {
		return domain.Evaluation{}, domain.Application{}, fmt.Errorf("insert evaluation: %w", err)
	}
	evalID, err := res.LastInsertId()
	if err != nil {
		return domain.Evaluation{}, domain.Application{}, err
	}

	// Upsert application (enforcing UNIQUE(profile_id, vacancy_id)).
	var existingAppID int64
	var prevState string
	err = tx.QueryRowContext(ctx, `
		SELECT id, state FROM applications
		WHERE profile_id = ? AND vacancy_id = ?
	`, DefaultProfileID, params.Evaluation.VacancyID).Scan(&existingAppID, &prevState)

	var appID int64
	if errors.Is(err, sql.ErrNoRows) {
		appRes, err := tx.ExecContext(ctx, `
			INSERT INTO applications (
				profile_id, vacancy_id, state, latest_evaluation_id,
				channel, recipient, subject, body, pending_reason, discard_reason,
				external_apply_url, a2ui_answers_json, queued_for_send, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', ?, ?, ?)
		`,
			DefaultProfileID,
			params.Evaluation.VacancyID,
			string(params.ApplicationState),
			evalID,
			params.Channel,
			params.Recipient,
			params.Subject,
			params.Body,
			params.PendingReason,
			params.DiscardReason,
			params.ExternalApplyURL,
			boolToInt(params.QueuedForSend),
			nowStr,
			nowStr,
		)
		if err != nil {
			return domain.Evaluation{}, domain.Application{}, fmt.Errorf("insert application: %w", err)
		}
		appID, err = appRes.LastInsertId()
		if err != nil {
			return domain.Evaluation{}, domain.Application{}, err
		}
	} else if err != nil {
		return domain.Evaluation{}, domain.Application{}, fmt.Errorf("query existing application: %w", err)
	} else {
		appID = existingAppID

		// Protect already-sent / active-interview applications and uncertain attempts from being overwritten or auto-requeued.
		var activeOrUncertain int
		_ = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM submission_attempts
			WHERE application_id = ? AND status IN ('sending', 'sent', 'uncertain')
		`, appID).Scan(&activeOrUncertain)

		preserveState := prevState == string(domain.StateSent) ||
			prevState == string(domain.StateInProgress) ||
			prevState == string(domain.StateInterviewPending) ||
			activeOrUncertain > 0

		nextState := string(params.ApplicationState)
		nextQueued := boolToInt(params.QueuedForSend)
		nextPendingReason := params.PendingReason
		nextDiscardReason := params.DiscardReason
		if preserveState {
			nextState = prevState
			nextQueued = 0
			if activeOrUncertain > 0 && prevState == string(domain.StatePending) {
				nextPendingReason = "Previous submission attempt is still uncertain or in progress; verify before resending."
				nextDiscardReason = ""
			}
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE applications
			SET
				state = ?,
				latest_evaluation_id = ?,
				channel = ?,
				recipient = ?,
				subject = ?,
				body = ?,
				pending_reason = ?,
				discard_reason = ?,
				external_apply_url = ?,
				queued_for_send = ?,
				updated_at = ?
			WHERE id = ?
		`,
			nextState,
			evalID,
			params.Channel,
			params.Recipient,
			params.Subject,
			params.Body,
			nextPendingReason,
			nextDiscardReason,
			params.ExternalApplyURL,
			nextQueued,
			nowStr,
			appID,
		)
		if err != nil {
			return domain.Evaluation{}, domain.Application{}, fmt.Errorf("update application %d: %w", appID, err)
		}
	}

	// Persist exact attached CV versions.
	if _, err := tx.ExecContext(ctx, `DELETE FROM application_cvs WHERE application_id = ?`, appID); err != nil {
		return domain.Evaluation{}, domain.Application{}, fmt.Errorf("clear application_cvs: %w", err)
	}
	for i, cvID := range params.Evaluation.SelectedCVIDs {
		role := "primary"
		if i < len(params.SelectedCVRoles) && params.SelectedCVRoles[i] != "" {
			role = params.SelectedCVRoles[i]
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO application_cvs (application_id, cv_document_id, role, attached_at)
			VALUES (?, ?, ?, ?)
		`, appID, cvID, role, nowStr); err != nil {
			return domain.Evaluation{}, domain.Application{}, fmt.Errorf("attach cv %d to application %d: %w", cvID, appID, err)
		}
	}

	// Record state change audit row whenever initial state or state/reason updates.
	source := params.StateSource
	if source == "" {
		source = "system"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO state_changes (application_id, from_state, to_state, reason, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, appID, prevState, string(params.ApplicationState), params.StateReason, source, nowStr); err != nil {
		return domain.Evaluation{}, domain.Application{}, fmt.Errorf("record state change: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return domain.Evaluation{}, domain.Application{}, fmt.Errorf("commit evaluation tx: %w", err)
	}

	savedEval, err := s.GetLatestEvaluationByVacancy(ctx, params.Evaluation.VacancyID)
	if err != nil {
		return domain.Evaluation{}, domain.Application{}, err
	}
	savedApp, err := s.GetApplicationByVacancy(ctx, params.Evaluation.VacancyID)
	if err != nil {
		return domain.Evaluation{}, domain.Application{}, err
	}
	return savedEval, savedApp, nil
}

func (s *Store) GetLatestEvaluationByVacancy(ctx context.Context, vacancyID int64) (domain.Evaluation, error) {
	var (
		e               domain.Evaluation
		selectedIDsJSON string
		passedFilters   int
		passedThreshold int
		langStatus      string
		findingsJSON    string
		missingJSON     string
		createdRaw      string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			id, vacancy_id, profile_id, selected_cv_ids_json, model_name,
			fit_score, passed_filters, passed_threshold, language_status,
			summary, findings_json, missing_answers_json, created_at
		FROM evaluations
		WHERE vacancy_id = ? AND profile_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, vacancyID, DefaultProfileID).Scan(
		&e.ID, &e.VacancyID, &e.ProfileID, &selectedIDsJSON, &e.ModelName,
		&e.FitScore, &passedFilters, &passedThreshold, &langStatus,
		&e.Summary, &findingsJSON, &missingJSON, &createdRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Evaluation{}, ErrNotFound
	}
	if err != nil {
		return domain.Evaluation{}, fmt.Errorf("query latest evaluation: %w", err)
	}

	_ = json.Unmarshal([]byte(selectedIDsJSON), &e.SelectedCVIDs)
	if e.SelectedCVIDs == nil {
		e.SelectedCVIDs = []int64{}
	}
	_ = json.Unmarshal([]byte(findingsJSON), &e.Findings)
	if e.Findings == nil {
		e.Findings = []domain.RequirementFinding{}
	}
	_ = json.Unmarshal([]byte(missingJSON), &e.MissingAnswers)
	if e.MissingAnswers == nil {
		e.MissingAnswers = []domain.MissingAnswer{}
	}
	e.PassedFilters = passedFilters == 1
	e.PassedThreshold = passedThreshold == 1
	e.LanguageStatus = domain.LanguageStatus(langStatus)
	e.CreatedAt = parseTime(createdRaw)

	// Hydrate selected CV summaries.
	for i, cvID := range e.SelectedCVIDs {
		doc, err := s.GetCV(ctx, cvID)
		if err != nil {
			continue
		}
		role := "primary"
		if i > 0 {
			role = "secondary"
		}
		e.SelectedCVs = append(e.SelectedCVs, domain.CVSummary{
			ID:       doc.ID,
			Language: doc.Language,
			Version:  doc.Version,
			Filename: doc.Filename,
			Role:     role,
		})
	}
	return e, nil
}

func (s *Store) GetApplicationByVacancy(ctx context.Context, vacancyID int64) (domain.Application, error) {
	var (
		app        domain.Application
		stateRaw   string
		evalID     sql.NullInt64
		answersRaw string
		queued     int
		createdRaw string
		updatedRaw string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			id, profile_id, vacancy_id, state, latest_evaluation_id,
			channel, recipient, subject, body, pending_reason, discard_reason,
			external_apply_url, a2ui_answers_json, queued_for_send, created_at, updated_at
		FROM applications
		WHERE profile_id = ? AND vacancy_id = ?
	`, DefaultProfileID, vacancyID).Scan(
		&app.ID, &app.ProfileID, &app.VacancyID, &stateRaw, &evalID,
		&app.Channel, &app.Recipient, &app.Subject, &app.Body, &app.PendingReason, &app.DiscardReason,
		&app.ExternalApplyURL, &answersRaw, &queued, &createdRaw, &updatedRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Application{}, ErrNotFound
	}
	if err != nil {
		return domain.Application{}, fmt.Errorf("query application: %w", err)
	}
	app.State = domain.ApplicationState(stateRaw)
	if evalID.Valid {
		v := evalID.Int64
		app.LatestEvaluationID = &v
	}
	_ = json.Unmarshal([]byte(answersRaw), &app.A2UIAnswers)
	if app.A2UIAnswers == nil {
		app.A2UIAnswers = map[string]any{}
	}
	app.QueuedForSend = queued == 1
	app.CreatedAt = parseTime(createdRaw)
	app.UpdatedAt = parseTime(updatedRaw)

	// Hydrate attached CVs.
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.language, c.version, c.filename, ac.role
		FROM application_cvs ac
		JOIN cv_documents c ON c.id = ac.cv_document_id
		WHERE ac.application_id = ?
		ORDER BY CASE ac.role WHEN 'primary' THEN 0 ELSE 1 END, c.id ASC
	`, app.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var sum domain.CVSummary
			var lang string
			if err := rows.Scan(&sum.ID, &lang, &sum.Version, &sum.Filename, &sum.Role); err == nil {
				sum.Language = domain.CVLanguage(lang)
				app.AttachedCVs = append(app.AttachedCVs, sum)
			}
		}
	}
	return app, nil
}

func (s *Store) ListStateChanges(ctx context.Context, applicationID int64) ([]domain.StateChange, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, application_id, from_state, to_state, reason, source, created_at
		FROM state_changes
		WHERE application_id = ?
		ORDER BY id ASC
	`, applicationID)
	if err != nil {
		return nil, fmt.Errorf("list state changes: %w", err)
	}
	defer rows.Close()

	out := make([]domain.StateChange, 0)
	for rows.Next() {
		var (
			sc         domain.StateChange
			fromRaw    string
			toRaw      string
			createdRaw string
		)
		if err := rows.Scan(&sc.ID, &sc.ApplicationID, &fromRaw, &toRaw, &sc.Reason, &sc.Source, &createdRaw); err != nil {
			return nil, err
		}
		sc.FromState = domain.ApplicationState(fromRaw)
		sc.ToState = domain.ApplicationState(toRaw)
		sc.CreatedAt = parseTime(createdRaw)
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Store) GetModelSettings(ctx context.Context) (domain.ModelSettings, error) {
	defaults := domain.ModelSettings{
		Provider:       "local_heuristic",
		EndpointURL:    "",
		Model:          "",
		TimeoutSeconds: 45,
	}
	raw, err := s.GetSetting(ctx, "ai_model_settings")
	if errors.Is(err, ErrNotFound) || raw == "" {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	var cfg domain.ModelSettings
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaults, nil
	}
	if cfg.Provider == "" {
		cfg.Provider = "local_heuristic"
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 45
	}
	cfg.HasAPIKey = strings.TrimSpace(cfg.APIKey) != ""
	return cfg, nil
}

func (s *Store) SaveModelSettings(ctx context.Context, input domain.ModelSettings) (domain.ModelSettings, error) {
	if input.Provider != "local_heuristic" && input.Provider != "openai_compatible" {
		return domain.ModelSettings{}, &ValidationError{
			Field:   "provider",
			Message: "provider must be 'local_heuristic' or 'openai_compatible'",
		}
	}
	if input.Provider == "openai_compatible" {
		if strings.TrimSpace(input.EndpointURL) == "" {
			return domain.ModelSettings{}, &ValidationError{
				Field:   "endpointUrl",
				Message: "endpoint URL is required for openai_compatible mode",
			}
		}
		if strings.TrimSpace(input.Model) == "" {
			return domain.ModelSettings{}, &ValidationError{
				Field:   "model",
				Message: "model name is required for openai_compatible mode",
			}
		}
	}
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = 45
	}

	existing, _ := s.GetModelSettings(ctx)
	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" && input.HasAPIKey {
		apiKey = existing.APIKey
	}

	toStore := domain.ModelSettings{
		Provider:       input.Provider,
		EndpointURL:    strings.TrimRight(strings.TrimSpace(input.EndpointURL), "/"),
		Model:          strings.TrimSpace(input.Model),
		APIKey:         apiKey,
		HasAPIKey:      apiKey != "",
		TimeoutSeconds: input.TimeoutSeconds,
	}
	raw, err := json.Marshal(toStore)
	if err != nil {
		return domain.ModelSettings{}, err
	}
	if err := s.SetSetting(ctx, "ai_model_settings", string(raw)); err != nil {
		return domain.ModelSettings{}, err
	}
	return toStore, nil
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return val, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, now)
	return err
}

func scanJobBoard(scanner rowScanner) (domain.JobBoard, error) {
	var (
		b          domain.JobBoard
		provider   string
		enabled    int
		lastSynced sql.NullString
		createdRaw string
		updatedRaw string
	)
	err := scanner.Scan(&b.ID, &provider, &b.CompanyBoard, &b.DisplayName, &enabled, &lastSynced, &createdRaw, &updatedRaw)
	if err != nil {
		return domain.JobBoard{}, err
	}
	b.Provider = domain.JobBoardProvider(provider)
	b.Enabled = enabled == 1
	if lastSynced.Valid && lastSynced.String != "" {
		t := parseTime(lastSynced.String)
		b.LastSyncedAt = &t
	}
	b.CreatedAt = parseTime(createdRaw)
	b.UpdatedAt = parseTime(updatedRaw)
	return b, nil
}

func scanVacancy(scanner rowScanner) (domain.Vacancy, error) {
	var (
		v          domain.Vacancy
		sourceType string
		workArr    string
		createdRaw string
		updatedRaw string
	)
	err := scanner.Scan(
		&v.ID, &sourceType, &v.Provider, &v.CompanyBoard, &v.ExternalJobID, &v.CanonicalURL,
		&v.CompanyName, &v.Title, &v.Location, &workArr, &v.Description,
		&v.RequestedLanguage, &v.DetectedLanguage, &v.ApplicationEmail, &v.ApplicationURL,
		&v.RawJSON, &createdRaw, &updatedRaw,
	)
	if err != nil {
		return domain.Vacancy{}, err
	}
	v.SourceType = domain.JobBoardProvider(sourceType)
	v.WorkArrangement = domain.WorkArrangement(workArr)
	v.CreatedAt = parseTime(createdRaw)
	v.UpdatedAt = parseTime(updatedRaw)
	return v, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
