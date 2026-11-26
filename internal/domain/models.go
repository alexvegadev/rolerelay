package domain

import "time"

type LanguageMode string

const (
	LanguageModeEN   LanguageMode = "en"
	LanguageModeES   LanguageMode = "es"
	LanguageModeBoth LanguageMode = "both"
)

func (m LanguageMode) Valid() bool {
	return m == LanguageModeEN || m == LanguageModeES || m == LanguageModeBoth
}

func (m LanguageMode) RequiredCVLanguages() []CVLanguage {
	switch m {
	case LanguageModeEN:
		return []CVLanguage{CVLanguageEN}
	case LanguageModeES:
		return []CVLanguage{CVLanguageES}
	case LanguageModeBoth:
		return []CVLanguage{CVLanguageEN, CVLanguageES}
	default:
		return nil
	}
}

type CVLanguage string

const (
	CVLanguageEN CVLanguage = "en"
	CVLanguageES CVLanguage = "es"
)

func (l CVLanguage) Valid() bool {
	return l == CVLanguageEN || l == CVLanguageES
}

type WorkArrangement string

const (
	WorkArrangementRemote  WorkArrangement = "remote"
	WorkArrangementHybrid  WorkArrangement = "hybrid"
	WorkArrangementOnsite  WorkArrangement = "onsite"
	WorkArrangementAny     WorkArrangement = "any"
	WorkArrangementUnknown WorkArrangement = "unknown"
)

func (w WorkArrangement) Valid() bool {
	return w == WorkArrangementRemote || w == WorkArrangementHybrid || w == WorkArrangementOnsite || w == WorkArrangementAny
}

func (w WorkArrangement) ValidVacancy() bool {
	return w == WorkArrangementRemote || w == WorkArrangementHybrid || w == WorkArrangementOnsite || w == WorkArrangementUnknown
}

type ApplicationState string

const (
	StatePending          ApplicationState = "pending"
	StateSent             ApplicationState = "sent"
	StateInProgress       ApplicationState = "in_progress"
	StateInterviewPending ApplicationState = "interview_pending"
	StateDiscarded        ApplicationState = "discarded"
)

type Profile struct {
	ID              int64           `json:"id"`
	FullName        string          `json:"fullName"`
	Email           string          `json:"email"`
	TargetRoles     []string        `json:"targetRoles"`
	Location        string          `json:"location"`
	WorkArrangement WorkArrangement `json:"workArrangement"`
	LanguageMode    LanguageMode    `json:"languageMode"`
	FitThreshold    int             `json:"fitThreshold"`
	DailySendLimit  int             `json:"dailySendLimit"`
	AutoSendEnabled bool            `json:"autoSendEnabled"`
	SendingPaused   bool            `json:"sendingPaused"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

type ProfileUpdate struct {
	FullName        string          `json:"fullName"`
	Email           string          `json:"email"`
	TargetRoles     []string        `json:"targetRoles"`
	Location        string          `json:"location"`
	WorkArrangement WorkArrangement `json:"workArrangement"`
	LanguageMode    LanguageMode    `json:"languageMode"`
	FitThreshold    int             `json:"fitThreshold"`
	DailySendLimit  int             `json:"dailySendLimit"`
}

type CVDocument struct {
	ID             int64      `json:"id"`
	ProfileID      int64      `json:"profileId"`
	Language       CVLanguage `json:"language"`
	Version        int        `json:"version"`
	Filename       string     `json:"filename"`
	ContentType    string     `json:"contentType"`
	SHA256         string     `json:"sha256"`
	SizeBytes      int64      `json:"sizeBytes"`
	PageCount      int        `json:"pageCount"`
	ExtractedText  string     `json:"extractedText"`
	ExtractedChars int        `json:"extractedChars"`
	IsActive       bool       `json:"isActive"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type CVFile struct {
	ID          int64
	Filename    string
	ContentType string
	Content     []byte
}

type ProfileReadiness struct {
	Ready             bool                   `json:"ready"`
	RequiredLanguages []CVLanguage           `json:"requiredLanguages"`
	MissingLanguages  []CVLanguage           `json:"missingLanguages"`
	ActiveByLanguage  map[string]*CVDocument `json:"activeByLanguage"`
	Issues            []string               `json:"issues"`
}

type JobBoardProvider string

const (
	ProviderManual     JobBoardProvider = "manual"
	ProviderGreenhouse JobBoardProvider = "greenhouse"
	ProviderLever      JobBoardProvider = "lever"
)

func (p JobBoardProvider) ValidBoard() bool {
	return p == ProviderGreenhouse || p == ProviderLever
}

type JobBoard struct {
	ID           int64            `json:"id"`
	Provider     JobBoardProvider `json:"provider"`
	CompanyBoard string           `json:"companyBoard"`
	DisplayName  string           `json:"displayName"`
	Enabled      bool             `json:"enabled"`
	LastSyncedAt *time.Time       `json:"lastSyncedAt,omitempty"`
	CreatedAt    time.Time        `json:"createdAt"`
	UpdatedAt    time.Time        `json:"updatedAt"`
}

type Vacancy struct {
	ID                int64            `json:"id"`
	SourceType        JobBoardProvider `json:"sourceType"`
	Provider          string           `json:"provider"`
	CompanyBoard      string           `json:"companyBoard"`
	ExternalJobID     string           `json:"externalJobId,omitempty"`
	CanonicalURL      string           `json:"canonicalUrl,omitempty"`
	CompanyName       string           `json:"companyName"`
	Title             string           `json:"title"`
	Location          string           `json:"location"`
	WorkArrangement   WorkArrangement  `json:"workArrangement"`
	Description       string           `json:"description"`
	RequestedLanguage string           `json:"requestedLanguage"` // "", "en", "es", "both"
	DetectedLanguage  string           `json:"detectedLanguage"`  // "en", "es", "ambiguous"
	ApplicationEmail  string           `json:"applicationEmail,omitempty"`
	ApplicationURL    string           `json:"applicationUrl,omitempty"`
	RawJSON           string           `json:"-"`
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`
}

type ManualVacancyInput struct {
	CompanyName       string          `json:"companyName"`
	Title             string          `json:"title"`
	Location          string          `json:"location"`
	WorkArrangement   WorkArrangement `json:"workArrangement"`
	Description       string          `json:"description"`
	RequestedLanguage string          `json:"requestedLanguage"`
	ApplicationEmail  string          `json:"applicationEmail"`
	ApplicationURL    string          `json:"applicationUrl"`
}

type RequirementKind string

const (
	RequirementMandatory  RequirementKind = "mandatory"
	RequirementPreference RequirementKind = "preference"
)

type RequirementFinding struct {
	Requirement      string          `json:"requirement"`
	Kind             RequirementKind `json:"kind"`
	Met              bool            `json:"met"`
	EvidenceExcerpt  string          `json:"evidenceExcerpt,omitempty"`
	CVLanguage       CVLanguage      `json:"cvLanguage,omitempty"`
	EvidenceVerified bool            `json:"evidenceVerified"`
	Reason           string          `json:"reason"`
}

type MissingAnswer struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"` // "salary", "cv_language", "text", "select"
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

type LanguageStatus string

const (
	LanguageStatusOK          LanguageStatus = "ok"
	LanguageStatusAmbiguous   LanguageStatus = "ambiguous"
	LanguageStatusMismatch    LanguageStatus = "mismatch"
	LanguageStatusMissingCV   LanguageStatus = "missing_required_cv"
	LanguageStatusChannelNote LanguageStatus = "both_requested_single_channel"
)

type Evaluation struct {
	ID                int64                `json:"id"`
	VacancyID         int64                `json:"vacancyId"`
	ProfileID         int64                `json:"profileId"`
	SelectedCVIDs     []int64              `json:"selectedCvIds"`
	SelectedCVs       []CVSummary          `json:"selectedCvs,omitempty"`
	ModelName         string               `json:"modelName"`
	FitScore          int                  `json:"fitScore"`
	PassedFilters     bool                 `json:"passedFilters"`
	PassedThreshold   bool                 `json:"passedThreshold"`
	LanguageStatus    LanguageStatus       `json:"languageStatus"`
	Summary           string               `json:"summary"`
	Findings          []RequirementFinding `json:"findings"`
	MissingAnswers    []MissingAnswer      `json:"missingAnswers"`
	CreatedAt         time.Time            `json:"createdAt"`
}

type CVSummary struct {
	ID       int64      `json:"id"`
	Language CVLanguage `json:"language"`
	Version  int        `json:"version"`
	Filename string     `json:"filename"`
	Role     string     `json:"role"` // "primary" | "secondary"
}

type Application struct {
	ID                 int64             `json:"id"`
	ProfileID          int64             `json:"profileId"`
	VacancyID          int64             `json:"vacancyId"`
	State              ApplicationState  `json:"state"`
	LatestEvaluationID *int64            `json:"latestEvaluationId,omitempty"`
	Channel            string            `json:"channel"`
	Recipient          string            `json:"recipient,omitempty"`
	Subject            string            `json:"subject,omitempty"`
	Body               string            `json:"body,omitempty"`
	PendingReason      string            `json:"pendingReason,omitempty"`
	DiscardReason      string            `json:"discardReason,omitempty"`
	ExternalApplyURL   string            `json:"externalApplyUrl,omitempty"`
	A2UIAnswers        map[string]any    `json:"a2uiAnswers"`
	QueuedForSend      bool              `json:"queuedForSend"`
	AttachedCVs        []CVSummary       `json:"attachedCvs,omitempty"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
}

type StateChange struct {
	ID            int64            `json:"id"`
	ApplicationID int64            `json:"applicationId"`
	FromState     ApplicationState `json:"fromState"`
	ToState       ApplicationState `json:"toState"`
	Reason        string           `json:"reason"`
	Source        string           `json:"source"`
	CreatedAt     time.Time        `json:"createdAt"`
}

type VacancyWithEvaluation struct {
	Vacancy          Vacancy       `json:"vacancy"`
	Application      *Application  `json:"application,omitempty"`
	LatestEvaluation *Evaluation   `json:"latestEvaluation,omitempty"`
	StateHistory     []StateChange `json:"stateHistory,omitempty"`
}

type ModelSettings struct {
	Provider       string `json:"provider"` // "local_heuristic" | "openai_compatible"
	EndpointURL    string `json:"endpointUrl"`
	Model          string `json:"model"`
	APIKey         string `json:"apiKey,omitempty"`
	HasAPIKey      bool   `json:"hasApiKey"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

type EmailSettings struct {
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Username          string `json:"username"`
	Password          string `json:"password,omitempty"`
	FromName          string `json:"fromName"`
	FromEmail         string `json:"fromEmail"`
	TLSMode           string `json:"tlsMode"` // "starttls" | "tls" | "none"
	CredentialBackend string `json:"credentialBackend,omitempty"`
	HasCredentials    bool   `json:"hasCredentials"`
	NeedsReconnect    bool   `json:"needsReconnect"`
	AutoSendEnabled   bool   `json:"autoSendEnabled"`
	SendingPaused     bool   `json:"sendingPaused"`
	DailySendLimit    int    `json:"dailySendLimit"`
	SentToday         int    `json:"sentToday"`
}

type SubmissionAttempt struct {
	ID                int64      `json:"id"`
	ApplicationID     int64      `json:"applicationId"`
	AttemptNumber     int        `json:"attemptNumber"`
	Channel           string     `json:"channel"`
	Recipient         string     `json:"recipient"`
	Status            string     `json:"status"` // claimed | sending | sent | retryable_error | uncertain | failed
	ErrorMessage      string     `json:"errorMessage,omitempty"`
	ProviderMessageID string     `json:"providerMessageId,omitempty"`
	StartedAt         time.Time  `json:"startedAt"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
}

type ApplicationDetail struct {
	Application         Application         `json:"application"`
	Vacancy             Vacancy             `json:"vacancy"`
	LatestEvaluation    *Evaluation         `json:"latestEvaluation,omitempty"`
	SubmissionAttempts  []SubmissionAttempt `json:"submissionAttempts"`
	StateHistory        []StateChange       `json:"stateHistory"`
	HasUncertainAttempt bool                `json:"hasUncertainAttempt"`
	CanQueue            bool                `json:"canQueue"`
}
