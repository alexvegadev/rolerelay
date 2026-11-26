-- Initial schema for RoleRelay covering profile, CVs, job boards, vacancies,
-- evaluations, applications, attached CV versions, submission attempts, state changes, and settings.

CREATE TABLE IF NOT EXISTS profiles (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    full_name TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    target_roles_json TEXT NOT NULL DEFAULT '[]',
    location TEXT NOT NULL DEFAULT '',
    work_arrangement TEXT NOT NULL DEFAULT 'any' CHECK (work_arrangement IN ('remote', 'hybrid', 'onsite', 'any')),
    language_mode TEXT NOT NULL DEFAULT 'en' CHECK (language_mode IN ('en', 'es', 'both')),
    fit_threshold INTEGER NOT NULL DEFAULT 70 CHECK (fit_threshold >= 0 AND fit_threshold <= 100),
    daily_send_limit INTEGER NOT NULL DEFAULT 10 CHECK (daily_send_limit >= 1),
    auto_send_enabled INTEGER NOT NULL DEFAULT 0 CHECK (auto_send_enabled IN (0, 1)),
    sending_paused INTEGER NOT NULL DEFAULT 0 CHECK (sending_paused IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cv_documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    language TEXT NOT NULL CHECK (language IN ('en', 'es')),
    version INTEGER NOT NULL CHECK (version >= 1),
    filename TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'application/pdf',
    sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
    page_count INTEGER NOT NULL DEFAULT 1 CHECK (page_count >= 1),
    extracted_text TEXT NOT NULL,
    content BLOB NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_at TEXT NOT NULL,
    UNIQUE (profile_id, language, version)
);

CREATE INDEX IF NOT EXISTS idx_cv_documents_profile_lang_active
    ON cv_documents (profile_id, language, is_active, version DESC);

CREATE TABLE IF NOT EXISTS job_boards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL CHECK (provider IN ('greenhouse', 'lever')),
    company_board TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    last_synced_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (provider, company_board)
);

CREATE TABLE IF NOT EXISTS vacancies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type TEXT NOT NULL CHECK (source_type IN ('manual', 'greenhouse', 'lever')),
    provider TEXT NOT NULL DEFAULT 'manual',
    company_board TEXT NOT NULL DEFAULT '',
    external_job_id TEXT,
    canonical_url TEXT NOT NULL DEFAULT '',
    company_name TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    location TEXT NOT NULL DEFAULT '',
    work_arrangement TEXT NOT NULL DEFAULT 'unknown',
    description TEXT NOT NULL,
    requested_language TEXT NOT NULL DEFAULT '',
    detected_language TEXT NOT NULL DEFAULT '',
    application_email TEXT NOT NULL DEFAULT '',
    application_url TEXT NOT NULL DEFAULT '',
    raw_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_vacancies_dedupe_external
    ON vacancies (provider, company_board, external_job_id)
    WHERE external_job_id IS NOT NULL AND external_job_id != '';

CREATE INDEX IF NOT EXISTS idx_vacancies_canonical_url
    ON vacancies (canonical_url)
    WHERE canonical_url != '';

CREATE TABLE IF NOT EXISTS evaluations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vacancy_id INTEGER NOT NULL REFERENCES vacancies(id) ON DELETE CASCADE,
    profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    selected_cv_ids_json TEXT NOT NULL DEFAULT '[]',
    model_name TEXT NOT NULL DEFAULT '',
    fit_score INTEGER NOT NULL DEFAULT 0 CHECK (fit_score >= 0 AND fit_score <= 100),
    passed_filters INTEGER NOT NULL DEFAULT 0 CHECK (passed_filters IN (0, 1)),
    passed_threshold INTEGER NOT NULL DEFAULT 0 CHECK (passed_threshold IN (0, 1)),
    language_status TEXT NOT NULL DEFAULT 'ok',
    summary TEXT NOT NULL DEFAULT '',
    findings_json TEXT NOT NULL DEFAULT '[]',
    missing_answers_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_evaluations_vacancy_created
    ON evaluations (vacancy_id, created_at DESC);

CREATE TABLE IF NOT EXISTS applications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    vacancy_id INTEGER NOT NULL REFERENCES vacancies(id) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'sent', 'in_progress', 'interview_pending', 'discarded')),
    latest_evaluation_id INTEGER REFERENCES evaluations(id) ON DELETE SET NULL,
    channel TEXT NOT NULL DEFAULT 'manual',
    recipient TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    pending_reason TEXT NOT NULL DEFAULT '',
    discard_reason TEXT NOT NULL DEFAULT '',
    external_apply_url TEXT NOT NULL DEFAULT '',
    a2ui_answers_json TEXT NOT NULL DEFAULT '{}',
    queued_for_send INTEGER NOT NULL DEFAULT 0 CHECK (queued_for_send IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (profile_id, vacancy_id)
);

CREATE INDEX IF NOT EXISTS idx_applications_state_queued
    ON applications (state, queued_for_send, updated_at DESC);

CREATE TABLE IF NOT EXISTS application_cvs (
    application_id INTEGER NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    cv_document_id INTEGER NOT NULL REFERENCES cv_documents(id) ON DELETE RESTRICT,
    role TEXT NOT NULL DEFAULT 'primary' CHECK (role IN ('primary', 'secondary')),
    attached_at TEXT NOT NULL,
    PRIMARY KEY (application_id, cv_document_id)
);

CREATE TABLE IF NOT EXISTS submission_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    application_id INTEGER NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL CHECK (attempt_number >= 1),
    channel TEXT NOT NULL,
    recipient TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('claimed', 'sending', 'sent', 'retryable_error', 'uncertain', 'failed')),
    error_message TEXT NOT NULL DEFAULT '',
    provider_message_id TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT,
    UNIQUE (application_id, attempt_number)
);

CREATE TABLE IF NOT EXISTS state_changes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    application_id INTEGER NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    from_state TEXT NOT NULL DEFAULT '',
    to_state TEXT NOT NULL CHECK (to_state IN ('pending', 'sent', 'in_progress', 'interview_pending', 'discarded')),
    reason TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'system' CHECK (source IN ('system', 'user', 'worker', 'a2ui')),
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_state_changes_application
    ON state_changes (application_id, created_at ASC);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
