package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"rolerelay/internal/domain"
	"rolerelay/internal/mailer"
)

const DefaultMaxSendAttempts = 3

var (
	ErrAlreadySent        = errors.New("application has already been sent")
	ErrUncertainPending   = errors.New("previous submission attempt left delivery uncertain; verify your Sent folder before requeuing")
	ErrNotEligibleToQueue = errors.New("application is not eligible for automatic email submission")
)

type storedEmailConfig struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	FromName  string `json:"fromName"`
	FromEmail string `json:"fromEmail"`
	TLSMode   string `json:"tlsMode"`
}

func (s *Store) GetEmailSettings(ctx context.Context) (domain.EmailSettings, error) {
	profile, err := s.GetOrCreateProfile(ctx)
	if err != nil {
		return domain.EmailSettings{}, err
	}
	sentToday, err := s.CountSentToday(ctx)
	if err != nil {
		return domain.EmailSettings{}, err
	}

	out := domain.EmailSettings{
		Port:            587,
		TLSMode:         "starttls",
		AutoSendEnabled: profile.AutoSendEnabled,
		SendingPaused:   profile.SendingPaused,
		DailySendLimit:  profile.DailySendLimit,
		SentToday:       sentToday,
	}

	raw, err := s.GetSetting(ctx, "smtp_email_settings")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return domain.EmailSettings{}, err
	}
	if raw != "" {
		var cfg storedEmailConfig
		if err := json.Unmarshal([]byte(raw), &cfg); err == nil {
			out.Host = cfg.Host
			if cfg.Port > 0 {
				out.Port = cfg.Port
			}
			out.Username = cfg.Username
			out.FromName = cfg.FromName
			out.FromEmail = cfg.FromEmail
			if cfg.TLSMode != "" {
				out.TLSMode = cfg.TLSMode
			}
		}
	}
	return out, nil
}

func (s *Store) SaveEmailSettings(ctx context.Context, input domain.EmailSettings) (domain.EmailSettings, error) {
	if _, err := s.GetOrCreateProfile(ctx); err != nil {
		return domain.EmailSettings{}, err
	}

	host := strings.TrimSpace(input.Host)
	port := input.Port
	if port == 0 {
		port = 587
	}
	if port < 1 || port > 65535 {
		return domain.EmailSettings{}, &ValidationError{Field: "port", Message: "SMTP port must be between 1 and 65535"}
	}
	tlsMode := strings.ToLower(strings.TrimSpace(input.TLSMode))
	if tlsMode == "" {
		tlsMode = "starttls"
	}
	if tlsMode != "starttls" && tlsMode != "tls" && tlsMode != "none" {
		return domain.EmailSettings{}, &ValidationError{Field: "tlsMode", Message: "TLS mode must be starttls, tls, or none"}
	}
	if input.DailySendLimit > 0 && input.DailySendLimit <= 500 {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := s.db.ExecContext(ctx, `UPDATE profiles SET daily_send_limit = ?, updated_at = ? WHERE id = ?`, input.DailySendLimit, now, DefaultProfileID); err != nil {
			return domain.EmailSettings{}, fmt.Errorf("update daily send limit: %w", err)
		}
	}

	cfg := storedEmailConfig{
		Host:      host,
		Port:      port,
		Username:  strings.TrimSpace(input.Username),
		FromName:  strings.TrimSpace(input.FromName),
		FromEmail: strings.TrimSpace(input.FromEmail),
		TLSMode:   tlsMode,
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return domain.EmailSettings{}, err
	}
	if err := s.SetSetting(ctx, "smtp_email_settings", string(raw)); err != nil {
		return domain.EmailSettings{}, fmt.Errorf("save smtp settings: %w", err)
	}

	if err := s.SetSendingPreferences(ctx, input.AutoSendEnabled, input.SendingPaused); err != nil {
		return domain.EmailSettings{}, err
	}
	return s.GetEmailSettings(ctx)
}

func (s *Store) SetSendingPreferences(ctx context.Context, autoSendEnabled, sendingPaused bool) error {
	if _, err := s.GetOrCreateProfile(ctx); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		UPDATE profiles
		SET auto_send_enabled = ?, sending_paused = ?, updated_at = ?
		WHERE id = ?
	`, boolToInt(autoSendEnabled), boolToInt(sendingPaused), now, DefaultProfileID)
	if err != nil {
		return fmt.Errorf("update sending preferences: %w", err)
	}
	return nil
}

func (s *Store) SetSendingPaused(ctx context.Context, paused bool) error {
	if _, err := s.GetOrCreateProfile(ctx); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE profiles SET sending_paused = ?, updated_at = ? WHERE id = ?`, boolToInt(paused), now, DefaultProfileID)
	return err
}

func (s *Store) CountSentToday(ctx context.Context) (int, error) {
	startOfDay := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339Nano)
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM submission_attempts
		WHERE status = 'sent' AND COALESCE(finished_at, started_at) >= ?
	`, startOfDay).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count sent today: %w", err)
	}
	return count, nil
}

func (s *Store) ListSubmissionAttempts(ctx context.Context, appID int64) ([]domain.SubmissionAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, application_id, attempt_number, channel, recipient, status, error_message, provider_message_id, started_at, finished_at
		FROM submission_attempts
		WHERE application_id = ?
		ORDER BY attempt_number DESC
	`, appID)
	if err != nil {
		return nil, fmt.Errorf("list submission attempts: %w", err)
	}
	defer rows.Close()

	out := make([]domain.SubmissionAttempt, 0)
	for rows.Next() {
		var (
			att         domain.SubmissionAttempt
			startedRaw  string
			finishedRaw sql.NullString
		)
		if err := rows.Scan(
			&att.ID, &att.ApplicationID, &att.AttemptNumber, &att.Channel, &att.Recipient,
			&att.Status, &att.ErrorMessage, &att.ProviderMessageID, &startedRaw, &finishedRaw,
		); err != nil {
			return nil, err
		}
		att.StartedAt = parseTime(startedRaw)
		if finishedRaw.Valid && finishedRaw.String != "" {
			t := parseTime(finishedRaw.String)
			att.FinishedAt = &t
		}
		out = append(out, att)
	}
	return out, rows.Err()
}

func (s *Store) ListApplicationsDetailed(ctx context.Context) ([]domain.ApplicationDetail, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, vacancy_id
		FROM applications
		WHERE profile_id = ?
		ORDER BY updated_at DESC, id DESC
	`, DefaultProfileID)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	defer rows.Close()

	type pair struct{ appID, vacID int64 }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.appID, &p.vacID); err != nil {
			return nil, err
		}
		pairs = append(pairs, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]domain.ApplicationDetail, 0, len(pairs))
	for _, p := range pairs {
		detail, err := s.GetApplicationDetailed(ctx, p.appID)
		if err != nil {
			return nil, err
		}
		out = append(out, detail)
	}
	return out, nil
}

func (s *Store) GetApplicationDetailed(ctx context.Context, appID int64) (domain.ApplicationDetail, error) {
	var vacancyID int64
	err := s.db.QueryRowContext(ctx, `SELECT vacancy_id FROM applications WHERE id = ? AND profile_id = ?`, appID, DefaultProfileID).Scan(&vacancyID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ApplicationDetail{}, ErrNotFound
	}
	if err != nil {
		return domain.ApplicationDetail{}, err
	}

	app, err := s.GetApplicationByVacancy(ctx, vacancyID)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}
	vac, err := s.GetVacancy(ctx, vacancyID)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}
	attempts, err := s.ListSubmissionAttempts(ctx, app.ID)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}
	history, err := s.ListStateChanges(ctx, app.ID)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}

	var latestEval *domain.Evaluation
	if ev, err := s.GetLatestEvaluationByVacancy(ctx, vacancyID); err == nil {
		latestEval = &ev
	}

	hasUncertain := false
	for _, a := range attempts {
		if a.Status == "uncertain" {
			hasUncertain = true
			break
		}
	}

	canQueue := isEligibleForEmailQueue(app, latestEval)

	return domain.ApplicationDetail{
		Application:         app,
		Vacancy:             vac,
		LatestEvaluation:    latestEval,
		SubmissionAttempts:  attempts,
		StateHistory:        history,
		HasUncertainAttempt: hasUncertain,
		CanQueue:            canQueue,
	}, nil
}

func isEligibleForEmailQueue(app domain.Application, eval *domain.Evaluation) bool {
	if app.State != domain.StatePending {
		return false
	}
	if app.Channel != "email" || strings.TrimSpace(app.Recipient) == "" {
		return false
	}
	if len(app.AttachedCVs) == 0 {
		return false
	}
	if eval == nil {
		return false
	}
	if !eval.PassedFilters || !eval.PassedThreshold || eval.LanguageStatus != domain.LanguageStatusOK {
		return false
	}
	if len(eval.MissingAnswers) > 0 {
		return false
	}
	for _, f := range eval.Findings {
		if f.Kind == domain.RequirementMandatory && (!f.Met || !f.EvidenceVerified) {
			return false
		}
	}
	return true
}

func (s *Store) QueueApplication(ctx context.Context, appID int64, confirmNotSentAfterUncertain bool) (domain.ApplicationDetail, error) {
	detail, err := s.GetApplicationDetailed(ctx, appID)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}
	if detail.Application.State == domain.StateSent {
		return domain.ApplicationDetail{}, ErrAlreadySent
	}
	if !detail.CanQueue {
		return domain.ApplicationDetail{}, ErrNotEligibleToQueue
	}
	if detail.HasUncertainAttempt && !confirmNotSentAfterUncertain {
		return domain.ApplicationDetail{}, ErrUncertainPending
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if detail.HasUncertainAttempt && confirmNotSentAfterUncertain {
		// Mark prior uncertain attempt as failed (user confirmed it was not sent) so the queue can safely retry.
		if _, err := tx.ExecContext(ctx, `
			UPDATE submission_attempts
			SET status = 'failed',
				error_message = COALESCE(error_message, '') || ' (User confirmed email was not sent; requeued.)',
				finished_at = COALESCE(finished_at, ?)
			WHERE application_id = ? AND status = 'uncertain'
		`, now, appID); err != nil {
			return domain.ApplicationDetail{}, err
		}
	}

	reason := fmt.Sprintf("Queued for email submission to %s.", detail.Application.Recipient)
	if confirmNotSentAfterUncertain {
		reason = fmt.Sprintf("User confirmed prior uncertain attempt did not send; requeued for email submission to %s.", detail.Application.Recipient)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE applications
		SET queued_for_send = 1, pending_reason = ?, updated_at = ?
		WHERE id = ?
	`, reason, now, appID); err != nil {
		return domain.ApplicationDetail{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO state_changes (application_id, from_state, to_state, reason, source, created_at)
		VALUES (?, ?, 'pending', ?, 'user', ?)
	`, appID, string(detail.Application.State), reason, now); err != nil {
		return domain.ApplicationDetail{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.ApplicationDetail{}, err
	}
	return s.GetApplicationDetailed(ctx, appID)
}

func (s *Store) RecordApplicationState(ctx context.Context, appID int64, toState domain.ApplicationState, reason, source string) (domain.ApplicationDetail, error) {
	switch toState {
	case domain.StatePending, domain.StateSent, domain.StateInProgress, domain.StateInterviewPending, domain.StateDiscarded:
	default:
		return domain.ApplicationDetail{}, &ValidationError{Field: "state", Message: "invalid application state"}
	}

	detail, err := s.GetApplicationDetailed(ctx, appID)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}
	if source == "" {
		source = "user"
	}
	trimmedReason := strings.TrimSpace(reason)
	if trimmedReason == "" {
		trimmedReason = fmt.Sprintf("Status updated to %s by %s.", toState, source)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ApplicationDetail{}, err
	}
	defer func() { _ = tx.Rollback() }()

	pendingReason := detail.Application.PendingReason
	discardReason := detail.Application.DiscardReason
	queued := 0
	if toState == domain.StateDiscarded {
		discardReason = trimmedReason
		pendingReason = ""
	} else if toState == domain.StatePending {
		pendingReason = trimmedReason
		discardReason = ""
	} else {
		pendingReason = ""
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE applications
		SET state = ?, pending_reason = ?, discard_reason = ?, queued_for_send = ?, updated_at = ?
		WHERE id = ?
	`, string(toState), pendingReason, discardReason, queued, now, appID); err != nil {
		return domain.ApplicationDetail{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO state_changes (application_id, from_state, to_state, reason, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, appID, string(detail.Application.State), string(toState), trimmedReason, source, now); err != nil {
		return domain.ApplicationDetail{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.ApplicationDetail{}, err
	}
	return s.GetApplicationDetailed(ctx, appID)
}

func (s *Store) SaveApplicationA2UIAnswers(ctx context.Context, appID int64, answers map[string]any, reason string) error {
	raw, err := json.Marshal(answers)
	if err != nil {
		return fmt.Errorf("marshal a2ui answers: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var currentState string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM applications WHERE id = ? AND profile_id = ?`, appID, DefaultProfileID).Scan(&currentState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE applications
		SET a2ui_answers_json = ?, updated_at = ?
		WHERE id = ? AND profile_id = ?
	`, string(raw), now, appID, DefaultProfileID); err != nil {
		return fmt.Errorf("save a2ui answers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO state_changes (application_id, from_state, to_state, reason, source, created_at)
		VALUES (?, ?, ?, ?, 'a2ui', ?)
	`, appID, currentState, currentState, reason, now); err != nil {
		return fmt.Errorf("record a2ui state change: %w", err)
	}
	return tx.Commit()
}

type RecoverySummary struct {
	MarkedUncertain int
	MarkedRetryable int
}

// RecoverInterruptedAttempts inspects unfinished submission_attempts after a restart.
// Any attempt already in 'sending' (DATA started) becomes 'uncertain' and keeps the application Pending.
// Any attempt still in 'claimed' (before SMTP DATA began) becomes 'retryable_error' and may safely retry.
func (s *Store) RecoverInterruptedAttempts(ctx context.Context) (RecoverySummary, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RecoverySummary{}, fmt.Errorf("begin recovery tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var summary RecoverySummary

	// 1. Interrupted during 'sending' -> uncertain delivery!
	sendingRows, err := tx.QueryContext(ctx, `SELECT id, application_id FROM submission_attempts WHERE status = 'sending'`)
	if err != nil {
		return RecoverySummary{}, err
	}
	type attRef struct{ id, appID int64 }
	var sendingRefs []attRef
	for sendingRows.Next() {
		var r attRef
		if err := sendingRows.Scan(&r.id, &r.appID); err != nil {
			_ = sendingRows.Close()
			return RecoverySummary{}, err
		}
		sendingRefs = append(sendingRefs, r)
	}
	_ = sendingRows.Close()

	for _, ref := range sendingRefs {
		errMsg := "Process stopped during email dispatch; delivery is uncertain."
		pendingMsg := "Delivery uncertain after restart during dispatch. Check your Sent folder before confirming Sent or requeuing."
		if _, err := tx.ExecContext(ctx, `
			UPDATE submission_attempts
			SET status = 'uncertain', error_message = ?, finished_at = ?
			WHERE id = ?
		`, errMsg, now, ref.id); err != nil {
			return RecoverySummary{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE applications
			SET state = 'pending', queued_for_send = 0, pending_reason = ?, updated_at = ?
			WHERE id = ? AND state = 'pending'
		`, pendingMsg, now, ref.appID); err != nil {
			return RecoverySummary{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO state_changes (application_id, from_state, to_state, reason, source, created_at)
			VALUES (?, 'pending', 'pending', ?, 'worker', ?)
		`, ref.appID, pendingMsg, now); err != nil {
			return RecoverySummary{}, err
		}
		summary.MarkedUncertain++
	}

	// 2. Interrupted while still in 'claimed' (before SMTP DATA) -> safe to retry.
	res, err := tx.ExecContext(ctx, `
		UPDATE submission_attempts
		SET status = 'retryable_error',
			error_message = 'Process stopped before SMTP transmission started; safe to retry.',
			finished_at = ?
		WHERE status = 'claimed'
	`, now)
	if err != nil {
		return RecoverySummary{}, err
	}
	if n, err := res.RowsAffected(); err == nil {
		summary.MarkedRetryable = int(n)
	}

	if err := tx.Commit(); err != nil {
		return RecoverySummary{}, err
	}
	return summary, nil
}

type ClaimedSubmission struct {
	AttemptID     int64
	AttemptNumber int
	Application   domain.Application
	Attachments   []mailer.Attachment
}

func (s *Store) ClaimNextQueuedApplication(ctx context.Context, maxAttempts int) (*ClaimedSubmission, error) {
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxSendAttempts
	}

	profile, err := s.GetOrCreateProfile(ctx)
	if err != nil {
		return nil, err
	}
	if !profile.AutoSendEnabled || profile.SendingPaused {
		return nil, nil
	}
	sentToday, err := s.CountSentToday(ctx)
	if err != nil {
		return nil, err
	}
	if sentToday >= profile.DailySendLimit {
		return nil, nil
	}

	// Candidate queued applications with no active/sent/uncertain attempt.
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.vacancy_id
		FROM applications a
		WHERE a.profile_id = ?
		  AND a.state = 'pending'
		  AND a.queued_for_send = 1
		  AND a.channel = 'email'
		  AND a.recipient != ''
		  AND NOT EXISTS (
		      SELECT 1 FROM submission_attempts sa
		      WHERE sa.application_id = a.id
		        AND sa.status IN ('claimed', 'sending', 'sent', 'uncertain')
		  )
		ORDER BY a.updated_at ASC, a.id ASC
		LIMIT 10
	`, DefaultProfileID)
	if err != nil {
		return nil, fmt.Errorf("query queued applications: %w", err)
	}
	type cand struct{ appID, vacID int64 }
	var candidates []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.appID, &c.vacID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	_ = rows.Close()

	for _, c := range candidates {
		detail, err := s.GetApplicationDetailed(ctx, c.appID)
		if err != nil {
			continue
		}
		if !isEligibleForEmailQueue(detail.Application, detail.LatestEvaluation) {
			// Eligibility changed since queueing; unqueue cleanly.
			_, _ = s.db.ExecContext(ctx, `UPDATE applications SET queued_for_send = 0 WHERE id = ?`, c.appID)
			continue
		}
		if len(detail.SubmissionAttempts) >= maxAttempts {
			_, _ = s.db.ExecContext(ctx, `UPDATE applications SET queued_for_send = 0, pending_reason = 'Maximum automatic retry attempts reached; review attempt log before retrying.' WHERE id = ?`, c.appID)
			continue
		}

		// Load exact attached CV PDF bytes.
		var attachments []mailer.Attachment
		for _, cvSum := range detail.Application.AttachedCVs {
			file, err := s.GetCVFile(ctx, cvSum.ID)
			if err != nil {
				attachments = nil
				break
			}
			attachments = append(attachments, mailer.Attachment{
				Filename:    file.Filename,
				ContentType: file.ContentType,
				Content:     file.Content,
			})
		}
		if len(attachments) == 0 {
			continue
		}

		// Atomically record claimed attempt in SQLite.
		now := time.Now().UTC().Format(time.RFC3339Nano)
		attemptNum := len(detail.SubmissionAttempts) + 1
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO submission_attempts (
				application_id, attempt_number, channel, recipient, status, error_message, provider_message_id, started_at
			) VALUES (?, ?, ?, ?, 'claimed', '', '', ?)
		`, detail.Application.ID, attemptNum, detail.Application.Channel, detail.Application.Recipient, now)
		if err != nil {
			continue
		}
		attemptID, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}

		return &ClaimedSubmission{
			AttemptID:     attemptID,
			AttemptNumber: attemptNum,
			Application:   detail.Application,
			Attachments:   attachments,
		}, nil
	}

	return nil, nil
}

func (s *Store) MarkAttemptSending(ctx context.Context, attemptID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE submission_attempts SET status = 'sending' WHERE id = ?`, attemptID)
	return err
}

func (s *Store) CompleteAttemptSent(ctx context.Context, attemptID, appID int64, providerMsgID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE submission_attempts
		SET status = 'sent', provider_message_id = ?, finished_at = ?
		WHERE id = ?
	`, providerMsgID, now, attemptID); err != nil {
		return err
	}

	reason := "Application accepted by SMTP sending channel."
	if providerMsgID != "" {
		reason = fmt.Sprintf("Application sent via email (Message-ID %s).", providerMsgID)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE applications
		SET state = 'sent', queued_for_send = 0, pending_reason = '', updated_at = ?
		WHERE id = ?
	`, now, appID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO state_changes (application_id, from_state, to_state, reason, source, created_at)
		VALUES (?, 'pending', 'sent', ?, 'worker', ?)
	`, appID, reason, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CompleteAttemptRetryable(ctx context.Context, attemptID, appID int64, errMsg string, attemptNum, maxAttempts int) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE submission_attempts
		SET status = 'retryable_error', error_message = ?, finished_at = ?
		WHERE id = ?
	`, errMsg, now, attemptID); err != nil {
		return err
	}

	queued := 1
	pendingMsg := fmt.Sprintf("Temporary pre-send error (attempt %d/%d): %s — will retry.", attemptNum, maxAttempts, errMsg)
	if attemptNum >= maxAttempts {
		queued = 0
		pendingMsg = fmt.Sprintf("Failed after %d attempts before acceptance: %s — review and requeue when resolved.", attemptNum, errMsg)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE applications
		SET queued_for_send = ?, pending_reason = ?, updated_at = ?
		WHERE id = ?
	`, queued, pendingMsg, now, appID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CompleteAttemptFailed(ctx context.Context, attemptID, appID int64, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE submission_attempts
		SET status = 'failed', error_message = ?, finished_at = ?
		WHERE id = ?
	`, errMsg, now, attemptID); err != nil {
		return err
	}
	pendingMsg := fmt.Sprintf("Email rejected before acceptance: %s", errMsg)
	if _, err := tx.ExecContext(ctx, `
		UPDATE applications
		SET queued_for_send = 0, pending_reason = ?, updated_at = ?
		WHERE id = ?
	`, pendingMsg, now, appID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CompleteAttemptUncertain(ctx context.Context, attemptID, appID int64, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE submission_attempts
		SET status = 'uncertain', error_message = ?, finished_at = ?
		WHERE id = ?
	`, errMsg, now, attemptID); err != nil {
		return err
	}
	pendingMsg := fmt.Sprintf("Delivery uncertain (%s). Check your Sent folder before confirming Sent or requeuing.", errMsg)
	if _, err := tx.ExecContext(ctx, `
		UPDATE applications
		SET state = 'pending', queued_for_send = 0, pending_reason = ?, updated_at = ?
		WHERE id = ?
	`, pendingMsg, now, appID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO state_changes (application_id, from_state, to_state, reason, source, created_at)
		VALUES (?, 'pending', 'pending', ?, 'worker', ?)
	`, appID, pendingMsg, now); err != nil {
		return err
	}
	return tx.Commit()
}
