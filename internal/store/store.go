package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"rolerelay/internal/domain"
)

const DefaultProfileID int64 = 1

var (
	ErrNotFound   = errors.New("record not found")
	ErrValidation = errors.New("validation failed")
)

type ValidationError struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

func (e *ValidationError) Unwrap() error {
	return ErrValidation
}

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) GetOrCreateProfile(ctx context.Context) (domain.Profile, error) {
	p, err := s.getProfile(ctx)
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return domain.Profile{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO profiles (
			id, full_name, email, target_roles_json, location,
			work_arrangement, language_mode, fit_threshold, daily_send_limit,
			auto_send_enabled, sending_paused, created_at, updated_at
		) VALUES (1, '', '', '[]', '', 'any', 'en', 70, 10, 0, 0, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, now, now)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("create default profile: %w", err)
	}
	return s.getProfile(ctx)
}

func (s *Store) getProfile(ctx context.Context) (domain.Profile, error) {
	var (
		p               domain.Profile
		rolesJSON       string
		workArrangement string
		languageMode    string
		autoSend        int
		paused          int
		createdAtRaw    string
		updatedAtRaw    string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			id, full_name, email, target_roles_json, location,
			work_arrangement, language_mode, fit_threshold, daily_send_limit,
			auto_send_enabled, sending_paused, created_at, updated_at
		FROM profiles
		WHERE id = ?
	`, DefaultProfileID).Scan(
		&p.ID,
		&p.FullName,
		&p.Email,
		&rolesJSON,
		&p.Location,
		&workArrangement,
		&languageMode,
		&p.FitThreshold,
		&p.DailySendLimit,
		&autoSend,
		&paused,
		&createdAtRaw,
		&updatedAtRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Profile{}, ErrNotFound
	}
	if err != nil {
		return domain.Profile{}, fmt.Errorf("query profile: %w", err)
	}

	if err := json.Unmarshal([]byte(rolesJSON), &p.TargetRoles); err != nil {
		p.TargetRoles = []string{}
	}
	if p.TargetRoles == nil {
		p.TargetRoles = []string{}
	}
	p.WorkArrangement = domain.WorkArrangement(workArrangement)
	p.LanguageMode = domain.LanguageMode(languageMode)
	p.AutoSendEnabled = autoSend == 1
	p.SendingPaused = paused == 1
	p.CreatedAt = parseTime(createdAtRaw)
	p.UpdatedAt = parseTime(updatedAtRaw)
	return p, nil
}

func (s *Store) UpdateProfile(ctx context.Context, input domain.ProfileUpdate) (domain.Profile, error) {
	if _, err := s.GetOrCreateProfile(ctx); err != nil {
		return domain.Profile{}, err
	}

	if !input.WorkArrangement.Valid() {
		return domain.Profile{}, &ValidationError{
			Field:   "workArrangement",
			Message: "must be one of remote, hybrid, onsite, or any",
		}
	}
	if !input.LanguageMode.Valid() {
		return domain.Profile{}, &ValidationError{
			Field:   "languageMode",
			Message: "must be one of en, es, or both",
		}
	}
	if input.FitThreshold < 0 || input.FitThreshold > 100 {
		return domain.Profile{}, &ValidationError{
			Field:   "fitThreshold",
			Message: "must be between 0 and 100",
		}
	}
	if input.DailySendLimit < 1 || input.DailySendLimit > 500 {
		return domain.Profile{}, &ValidationError{
			Field:   "dailySendLimit",
			Message: "must be between 1 and 500",
		}
	}

	roles := normalizeRoles(input.TargetRoles)
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("marshal target roles: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
		UPDATE profiles
		SET
			full_name = ?,
			email = ?,
			target_roles_json = ?,
			location = ?,
			work_arrangement = ?,
			language_mode = ?,
			fit_threshold = ?,
			daily_send_limit = ?,
			updated_at = ?
		WHERE id = ?
	`,
		strings.TrimSpace(input.FullName),
		strings.TrimSpace(input.Email),
		string(rolesJSON),
		strings.TrimSpace(input.Location),
		string(input.WorkArrangement),
		string(input.LanguageMode),
		input.FitThreshold,
		input.DailySendLimit,
		now,
		DefaultProfileID,
	)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("update profile: %w", err)
	}

	return s.getProfile(ctx)
}

type SaveCVParams struct {
	Language      domain.CVLanguage
	Filename      string
	ContentType   string
	PageCount     int
	ExtractedText string
	Content       []byte
}

func (s *Store) SaveCV(ctx context.Context, params SaveCVParams) (domain.CVDocument, error) {
	if _, err := s.GetOrCreateProfile(ctx); err != nil {
		return domain.CVDocument{}, err
	}
	if !params.Language.Valid() {
		return domain.CVDocument{}, &ValidationError{
			Field:   "language",
			Message: "CV language must be 'en' (English) or 'es' (Spanish)",
		}
	}
	filename := strings.TrimSpace(params.Filename)
	if filename == "" {
		return domain.CVDocument{}, &ValidationError{
			Field:   "file",
			Message: "CV filename is required",
		}
	}
	if len(params.Content) == 0 {
		return domain.CVDocument{}, &ValidationError{
			Field:   "file",
			Message: "uploaded PDF file is empty",
		}
	}
	extracted := strings.TrimSpace(params.ExtractedText)
	if extracted == "" {
		return domain.CVDocument{}, &ValidationError{
			Field:   "file",
			Message: "the PDF has no extractable text",
		}
	}
	contentType := strings.TrimSpace(params.ContentType)
	if contentType == "" {
		contentType = "application/pdf"
	}
	pageCount := params.PageCount
	if pageCount < 1 {
		pageCount = 1
	}

	sum := sha256.Sum256(params.Content)
	hashHex := hex.EncodeToString(sum[:])
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.CVDocument{}, fmt.Errorf("begin save cv tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var maxVersion sql.NullInt64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT MAX(version) FROM cv_documents WHERE profile_id = ? AND language = ?`,
		DefaultProfileID,
		string(params.Language),
	).Scan(&maxVersion); err != nil {
		return domain.CVDocument{}, fmt.Errorf("query max cv version: %w", err)
	}
	nextVersion := 1
	if maxVersion.Valid {
		nextVersion = int(maxVersion.Int64) + 1
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE cv_documents SET is_active = 0 WHERE profile_id = ? AND language = ?`,
		DefaultProfileID,
		string(params.Language),
	); err != nil {
		return domain.CVDocument{}, fmt.Errorf("deactivate prior cv versions: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO cv_documents (
			profile_id, language, version, filename, content_type,
			sha256, size_bytes, page_count, extracted_text, content, is_active, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
	`,
		DefaultProfileID,
		string(params.Language),
		nextVersion,
		filename,
		contentType,
		hashHex,
		int64(len(params.Content)),
		pageCount,
		extracted,
		params.Content,
		nowStr,
	)
	if err != nil {
		return domain.CVDocument{}, fmt.Errorf("insert cv document: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.CVDocument{}, fmt.Errorf("read inserted cv id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return domain.CVDocument{}, fmt.Errorf("commit save cv tx: %w", err)
	}

	return domain.CVDocument{
		ID:             id,
		ProfileID:      DefaultProfileID,
		Language:       params.Language,
		Version:        nextVersion,
		Filename:       filename,
		ContentType:    contentType,
		SHA256:         hashHex,
		SizeBytes:      int64(len(params.Content)),
		PageCount:      pageCount,
		ExtractedText:  extracted,
		ExtractedChars: len([]rune(extracted)),
		IsActive:       true,
		CreatedAt:      now,
	}, nil
}

func (s *Store) ListCVs(ctx context.Context) ([]domain.CVDocument, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, profile_id, language, version, filename, content_type,
			sha256, size_bytes, page_count, extracted_text, is_active, created_at
		FROM cv_documents
		WHERE profile_id = ?
		ORDER BY language ASC, is_active DESC, version DESC
	`, DefaultProfileID)
	if err != nil {
		return nil, fmt.Errorf("list cv documents: %w", err)
	}
	defer rows.Close()

	out := make([]domain.CVDocument, 0)
	for rows.Next() {
		doc, err := scanCVDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cv documents: %w", err)
	}
	return out, nil
}

func (s *Store) GetCV(ctx context.Context, id int64) (domain.CVDocument, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id, profile_id, language, version, filename, content_type,
			sha256, size_bytes, page_count, extracted_text, is_active, created_at
		FROM cv_documents
		WHERE id = ? AND profile_id = ?
	`, id, DefaultProfileID)

	doc, err := scanCVDocument(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CVDocument{}, ErrNotFound
	}
	if err != nil {
		return domain.CVDocument{}, fmt.Errorf("get cv document %d: %w", id, err)
	}
	return doc, nil
}

func (s *Store) GetCVFile(ctx context.Context, id int64) (domain.CVFile, error) {
	var file domain.CVFile
	err := s.db.QueryRowContext(ctx, `
		SELECT id, filename, content_type, content
		FROM cv_documents
		WHERE id = ? AND profile_id = ?
	`, id, DefaultProfileID).Scan(&file.ID, &file.Filename, &file.ContentType, &file.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CVFile{}, ErrNotFound
	}
	if err != nil {
		return domain.CVFile{}, fmt.Errorf("get cv file %d: %w", id, err)
	}
	return file, nil
}

func (s *Store) ActivateCV(ctx context.Context, id int64) (domain.CVDocument, error) {
	doc, err := s.GetCV(ctx, id)
	if err != nil {
		return domain.CVDocument{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.CVDocument{}, fmt.Errorf("begin activate cv tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE cv_documents SET is_active = 0 WHERE profile_id = ? AND language = ?`,
		DefaultProfileID,
		string(doc.Language),
	); err != nil {
		return domain.CVDocument{}, fmt.Errorf("deactivate cvs for language %s: %w", doc.Language, err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE cv_documents SET is_active = 1 WHERE id = ? AND profile_id = ?`,
		id,
		DefaultProfileID,
	); err != nil {
		return domain.CVDocument{}, fmt.Errorf("activate cv %d: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return domain.CVDocument{}, fmt.Errorf("commit activate cv tx: %w", err)
	}

	doc.IsActive = true
	return doc, nil
}

func (s *Store) DeleteCV(ctx context.Context, id int64) error {
	doc, err := s.GetCV(ctx, id)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete cv tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM cv_documents WHERE id = ? AND profile_id = ?`, id, DefaultProfileID); err != nil {
		return fmt.Errorf("delete cv %d: %w", id, err)
	}

	if doc.IsActive {
		var latestID sql.NullInt64
		if err := tx.QueryRowContext(
			ctx,
			`SELECT id FROM cv_documents WHERE profile_id = ? AND language = ? ORDER BY version DESC LIMIT 1`,
			DefaultProfileID,
			string(doc.Language),
		).Scan(&latestID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("find fallback active cv: %w", err)
		}
		if latestID.Valid {
			if _, err := tx.ExecContext(ctx, `UPDATE cv_documents SET is_active = 1 WHERE id = ?`, latestID.Int64); err != nil {
				return fmt.Errorf("promote fallback active cv: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete cv tx: %w", err)
	}
	return nil
}

func ComputeReadiness(profile domain.Profile, cvs []domain.CVDocument) domain.ProfileReadiness {
	activeByLang := map[string]*domain.CVDocument{}
	for i := range cvs {
		doc := cvs[i]
		if doc.IsActive {
			copyDoc := doc
			activeByLang[string(doc.Language)] = &copyDoc
		}
	}

	required := profile.LanguageMode.RequiredCVLanguages()
	missing := make([]domain.CVLanguage, 0)
	issues := make([]string, 0)

	for _, lang := range required {
		if activeByLang[string(lang)] == nil {
			missing = append(missing, lang)
			if lang == domain.CVLanguageEN {
				issues = append(issues, "Upload an English PDF CV to satisfy your selected application language mode.")
			} else {
				issues = append(issues, "Upload a Spanish PDF CV to satisfy your selected application language mode.")
			}
		}
	}
	if len(profile.TargetRoles) == 0 {
		issues = append(issues, "Add at least one target role in your profile.")
	}

	return domain.ProfileReadiness{
		Ready:             len(issues) == 0,
		RequiredLanguages: required,
		MissingLanguages:  missing,
		ActiveByLanguage:  activeByLang,
		Issues:            issues,
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCVDocument(scanner rowScanner) (domain.CVDocument, error) {
	var (
		doc          domain.CVDocument
		lang         string
		isActive     int
		createdAtRaw string
	)
	err := scanner.Scan(
		&doc.ID,
		&doc.ProfileID,
		&lang,
		&doc.Version,
		&doc.Filename,
		&doc.ContentType,
		&doc.SHA256,
		&doc.SizeBytes,
		&doc.PageCount,
		&doc.ExtractedText,
		&isActive,
		&createdAtRaw,
	)
	if err != nil {
		return domain.CVDocument{}, err
	}
	doc.Language = domain.CVLanguage(lang)
	doc.IsActive = isActive == 1
	doc.ExtractedChars = len([]rune(doc.ExtractedText))
	doc.CreatedAt = parseTime(createdAtRaw)
	return doc, nil
}

func normalizeRoles(roles []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		trimmed := strings.TrimSpace(r)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, trimmed)
	}
	return out
}

func parseTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}
