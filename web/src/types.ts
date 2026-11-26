export type LanguageMode = "en" | "es" | "both";
export type CVLanguage = "en" | "es";
export type WorkArrangement = "remote" | "hybrid" | "onsite" | "any" | "unknown";
export type JobBoardProvider = "manual" | "greenhouse" | "lever";
export type ApplicationState =
  | "pending"
  | "sent"
  | "in_progress"
  | "interview_pending"
  | "discarded";

export interface Profile {
  id: number;
  fullName: string;
  email: string;
  targetRoles: string[];
  location: string;
  workArrangement: WorkArrangement;
  languageMode: LanguageMode;
  fitThreshold: number;
  dailySendLimit: number;
  autoSendEnabled: boolean;
  sendingPaused: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ProfileUpdatePayload {
  fullName: string;
  email: string;
  targetRoles: string[];
  location: string;
  workArrangement: WorkArrangement;
  languageMode: LanguageMode;
  fitThreshold: number;
  dailySendLimit: number;
}

export interface CVDocument {
  id: number;
  profileId: number;
  language: CVLanguage;
  version: number;
  filename: string;
  contentType: string;
  sha256: string;
  sizeBytes: number;
  pageCount: number;
  extractedText: string;
  extractedChars: number;
  isActive: boolean;
  createdAt: string;
}

export interface ProfileReadiness {
  ready: boolean;
  requiredLanguages: CVLanguage[];
  missingLanguages: CVLanguage[];
  activeByLanguage: Record<string, CVDocument | undefined>;
  issues: string[];
}

export interface ProfileBundle {
  profile: Profile;
  cvs: CVDocument[];
  readiness: ProfileReadiness;
}

export interface WorkerStatus {
  state: string;
  paused: boolean;
  lastError?: string;
  needsReconnect?: boolean;
  startedAt: string;
  lastHeartbeatAt: string;
}

export interface AppStatus {
  appName: string;
  version: string;
  storageMode: "portable" | "installed" | "custom";
  dataDir: string;
  dbPath: string;
  schemaVersion: number;
  address: string;
  worker: WorkerStatus;
}

export interface JobBoard {
  id: number;
  provider: "greenhouse" | "lever";
  companyBoard: string;
  displayName: string;
  enabled: boolean;
  lastSyncedAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface Vacancy {
  id: number;
  sourceType: JobBoardProvider;
  provider: string;
  companyBoard: string;
  externalJobId?: string;
  canonicalUrl?: string;
  companyName: string;
  title: string;
  location: string;
  workArrangement: WorkArrangement;
  description: string;
  requestedLanguage: string;
  detectedLanguage: string;
  applicationEmail?: string;
  applicationUrl?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ManualVacancyInput {
  companyName: string;
  title: string;
  location: string;
  workArrangement: WorkArrangement;
  description: string;
  requestedLanguage: string;
  applicationEmail: string;
  applicationUrl: string;
}

export interface RequirementFinding {
  requirement: string;
  kind: "mandatory" | "preference";
  met: boolean;
  evidenceExcerpt?: string;
  cvLanguage?: CVLanguage;
  evidenceVerified: boolean;
  reason: string;
}

export interface MissingAnswer {
  key: string;
  label: string;
  kind: string;
  required: boolean;
  options?: string[];
  reason?: string;
}

export interface CVSummary {
  id: number;
  language: CVLanguage;
  version: number;
  filename: string;
  role: string;
}

export interface Evaluation {
  id: number;
  vacancyId: number;
  profileId: number;
  selectedCvIds: number[];
  selectedCvs?: CVSummary[];
  modelName: string;
  fitScore: number;
  passedFilters: boolean;
  passedThreshold: boolean;
  languageStatus: string;
  summary: string;
  findings: RequirementFinding[];
  missingAnswers: MissingAnswer[];
  createdAt: string;
}

export interface Application {
  id: number;
  profileId: number;
  vacancyId: number;
  state: ApplicationState;
  latestEvaluationId?: number;
  channel: string;
  recipient?: string;
  subject?: string;
  body?: string;
  pendingReason?: string;
  discardReason?: string;
  externalApplyUrl?: string;
  a2uiAnswers: Record<string, unknown>;
  queuedForSend: boolean;
  attachedCvs?: CVSummary[];
  createdAt: string;
  updatedAt: string;
}

export interface StateChange {
  id: number;
  applicationId: number;
  fromState: ApplicationState | "";
  toState: ApplicationState;
  reason: string;
  source: string;
  createdAt: string;
}

export interface VacancyWithEvaluation {
  vacancy: Vacancy;
  application?: Application;
  latestEvaluation?: Evaluation;
  stateHistory?: StateChange[];
}

export interface ModelSettings {
  provider: "local_heuristic" | "openai_compatible";
  endpointUrl: string;
  model: string;
  apiKey?: string;
  hasApiKey: boolean;
  timeoutSeconds: number;
}

export interface EmailSettings {
  host: string;
  port: number;
  username: string;
  password?: string;
  fromName: string;
  fromEmail: string;
  tlsMode: "starttls" | "tls" | "none";
  credentialBackend?: string;
  hasCredentials: boolean;
  needsReconnect: boolean;
  autoSendEnabled: boolean;
  sendingPaused: boolean;
  dailySendLimit: number;
  sentToday: number;
}

export interface SubmissionAttempt {
  id: number;
  applicationId: number;
  attemptNumber: number;
  channel: string;
  recipient: string;
  status: "claimed" | "sending" | "sent" | "retryable_error" | "uncertain" | "failed";
  errorMessage?: string;
  providerMessageId?: string;
  startedAt: string;
  finishedAt?: string;
}

export interface ApplicationDetail {
  application: Application;
  vacancy: Vacancy;
  latestEvaluation?: Evaluation;
  submissionAttempts: SubmissionAttempt[];
  stateHistory: StateChange[];
  hasUncertainAttempt: boolean;
  canQueue: boolean;
}
