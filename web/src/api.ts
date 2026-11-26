import type {
  AppStatus,
  ApplicationDetail,
  ApplicationState,
  CVDocument,
  CVLanguage,
  EmailSettings,
  JobBoard,
  ManualVacancyInput,
  ModelSettings,
  ProfileBundle,
  ProfileReadiness,
  ProfileUpdatePayload,
  VacancyWithEvaluation,
  WorkerStatus,
} from "./types";

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  readonly field?: string;

  constructor(status: number, code: string, message: string, field?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.field = field;
  }
}

async function parseResponse<T>(res: Response): Promise<T> {
  const text = await res.text();
  let data: Record<string, unknown> = {};
  if (text) {
    try {
      data = JSON.parse(text) as Record<string, unknown>;
    } catch {
      if (!res.ok) {
        throw new ApiError(res.status, "http_error", text || res.statusText);
      }
    }
  }
  if (!res.ok) {
    const code = typeof data.code === "string" ? data.code : "http_error";
    const msg = typeof data.error === "string" ? data.error : res.statusText || "Request failed";
    const field = typeof data.field === "string" ? data.field : undefined;
    throw new ApiError(res.status, code, msg, field);
  }
  return data as T;
}

export async function fetchStatus(): Promise<AppStatus> {
  const res = await fetch("/api/status", { cache: "no-store" });
  return parseResponse<AppStatus>(res);
}

export async function fetchProfileBundle(): Promise<ProfileBundle> {
  const res = await fetch("/api/profile", { cache: "no-store" });
  return parseResponse<ProfileBundle>(res);
}

export async function saveProfile(payload: ProfileUpdatePayload): Promise<ProfileBundle> {
  const res = await fetch("/api/profile", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  return parseResponse<ProfileBundle>(res);
}

export interface UploadCVResult {
  cv: CVDocument;
  cvs: CVDocument[];
  readiness: ProfileReadiness;
}

export async function uploadCV(language: CVLanguage, file: File): Promise<UploadCVResult> {
  const form = new FormData();
  form.append("language", language);
  form.append("file", file);
  const res = await fetch("/api/cvs", {
    method: "POST",
    body: form,
  });
  return parseResponse<UploadCVResult>(res);
}

export async function activateCV(id: number): Promise<UploadCVResult> {
  const res = await fetch(`/api/cvs/${id}/activate`, { method: "POST" });
  return parseResponse<UploadCVResult>(res);
}

export async function deleteCV(id: number): Promise<{ cvs: CVDocument[]; readiness: ProfileReadiness }> {
  const res = await fetch(`/api/cvs/${id}`, { method: "DELETE" });
  return parseResponse<{ cvs: CVDocument[]; readiness: ProfileReadiness }>(res);
}

export async function fetchBoards(): Promise<{ boards: JobBoard[] }> {
  const res = await fetch("/api/boards", { cache: "no-store" });
  return parseResponse<{ boards: JobBoard[] }>(res);
}

export async function addBoard(payload: {
  provider: "greenhouse" | "lever";
  companyBoard: string;
  displayName: string;
}): Promise<{ board: JobBoard }> {
  const res = await fetch("/api/boards", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  return parseResponse<{ board: JobBoard }>(res);
}

export async function deleteBoard(id: number): Promise<{ deleted: boolean }> {
  const res = await fetch(`/api/boards/${id}`, { method: "DELETE" });
  return parseResponse<{ deleted: boolean }>(res);
}

export async function syncBoard(id: number): Promise<{
  board: JobBoard;
  fetched: number;
  created: number;
  deduplicated: number;
  vacancies: VacancyWithEvaluation[];
}> {
  const res = await fetch(`/api/boards/${id}/sync`, { method: "POST" });
  return parseResponse<{
    board: JobBoard;
    fetched: number;
    created: number;
    deduplicated: number;
    vacancies: VacancyWithEvaluation[];
  }>(res);
}

export async function fetchVacancies(): Promise<{ vacancies: VacancyWithEvaluation[] }> {
  const res = await fetch("/api/vacancies", { cache: "no-store" });
  return parseResponse<{ vacancies: VacancyWithEvaluation[] }>(res);
}

export async function createManualVacancy(input: ManualVacancyInput): Promise<{
  item: VacancyWithEvaluation;
  created: boolean;
  deduplicated: boolean;
  matchedBy?: string;
}> {
  const res = await fetch("/api/vacancies", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return parseResponse<{
    item: VacancyWithEvaluation;
    created: boolean;
    deduplicated: boolean;
    matchedBy?: string;
  }>(res);
}

export async function deleteVacancy(id: number): Promise<{ deleted: boolean }> {
  const res = await fetch(`/api/vacancies/${id}`, { method: "DELETE" });
  return parseResponse<{ deleted: boolean }>(res);
}

export async function evaluateVacancy(id: number): Promise<{ item: VacancyWithEvaluation }> {
  const res = await fetch(`/api/vacancies/${id}/evaluate`, { method: "POST" });
  return parseResponse<{ item: VacancyWithEvaluation }>(res);
}

export async function fetchModelSettings(): Promise<{ settings: ModelSettings }> {
  const res = await fetch("/api/settings/model", { cache: "no-store" });
  return parseResponse<{ settings: ModelSettings }>(res);
}

export async function saveModelSettings(settings: ModelSettings): Promise<{ settings: ModelSettings }> {
  const res = await fetch("/api/settings/model", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(settings),
  });
  return parseResponse<{ settings: ModelSettings }>(res);
}

export async function fetchApplications(): Promise<{
  applications: ApplicationDetail[];
  emailSettings: EmailSettings;
}> {
  const res = await fetch("/api/applications", { cache: "no-store" });
  return parseResponse<{ applications: ApplicationDetail[]; emailSettings: EmailSettings }>(res);
}

export async function queueApplication(
  id: number,
  confirmNotSent = false
): Promise<{ application: ApplicationDetail }> {
  const res = await fetch(`/api/applications/${id}/queue`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ confirmNotSent }),
  });
  return parseResponse<{ application: ApplicationDetail }>(res);
}

export async function updateApplicationState(
  id: number,
  state: ApplicationState,
  reason: string
): Promise<{ application: ApplicationDetail }> {
  const res = await fetch(`/api/applications/${id}/state`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ state, reason }),
  });
  return parseResponse<{ application: ApplicationDetail }>(res);
}

export async function fetchEmailSettings(): Promise<{ settings: EmailSettings }> {
  const res = await fetch("/api/settings/email", { cache: "no-store" });
  return parseResponse<{ settings: EmailSettings }>(res);
}

export async function saveEmailSettings(settings: EmailSettings): Promise<{ settings: EmailSettings }> {
  const res = await fetch("/api/settings/email", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(settings),
  });
  return parseResponse<{ settings: EmailSettings }>(res);
}

export async function disconnectEmailCredentials(): Promise<{ settings: EmailSettings }> {
  const res = await fetch("/api/settings/email/credentials", { method: "DELETE" });
  return parseResponse<{ settings: EmailSettings }>(res);
}

export async function testEmailConnection(recipient: string): Promise<{
  ok: boolean;
  recipient: string;
  messageId: string;
}> {
  const res = await fetch("/api/settings/email/test", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ recipient }),
  });
  return parseResponse<{ ok: boolean; recipient: string; messageId: string }>(res);
}

export interface A2UIPanelPayload {
  surfaceId: string;
  catalogId: string;
  reviewCases: string[];
  hasInputs: boolean;
  messages: unknown[];
}

export async function fetchApplicationA2UI(id: number): Promise<{
  panel: A2UIPanelPayload;
  application: ApplicationDetail;
}> {
  const res = await fetch(`/api/applications/${id}/a2ui`, { cache: "no-store" });
  return parseResponse<{ panel: A2UIPanelPayload; application: ApplicationDetail }>(res);
}

export async function sendApplicationA2UIEvent(
  id: number,
  envelope: {
    version: "1.0";
    action: {
      name: string;
      surfaceId: string;
      sourceComponentId: string;
      timestamp: string;
      context: Record<string, unknown>;
    };
  }
): Promise<{
  message: string;
  application: ApplicationDetail;
  panel: A2UIPanelPayload;
}> {
  const res = await fetch(`/api/applications/${id}/a2ui/event`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(envelope),
  });
  return parseResponse<{
    message: string;
    application: ApplicationDetail;
    panel: A2UIPanelPayload;
  }>(res);
}

export async function pauseWorker(): Promise<{ worker: WorkerStatus }> {
  const res = await fetch("/api/worker/pause", { method: "POST" });
  return parseResponse<{ worker: WorkerStatus }>(res);
}

export async function resumeWorker(): Promise<{ worker: WorkerStatus }> {
  const res = await fetch("/api/worker/resume", { method: "POST" });
  return parseResponse<{ worker: WorkerStatus }>(res);
}

export async function exitApp(): Promise<{ exiting: boolean; message: string }> {
  const res = await fetch("/api/app/exit", { method: "POST" });
  return parseResponse<{ exiting: boolean; message: string }>(res);
}

export function cvFileUrl(id: number, download = false): string {
  return download ? `/api/cvs/${id}/file?download=1` : `/api/cvs/${id}/file`;
}

export function backupExportUrl(): string {
  return "/api/backup/export";
}

export interface RestoreSummary {
  schemaVersion: number;
  cvCount: number;
  vacancyCount: number;
  evaluationCount: number;
  applicationCount: number;
  attemptCount: number;
  stateChangeCount: number;
  safetyBackupPath?: string;
}

export async function restoreBackup(file: File): Promise<{
  restored: boolean;
  summary: RestoreSummary;
  emailSettings: EmailSettings;
}> {
  const form = new FormData();
  form.append("file", file);
  const res = await fetch("/api/backup/restore", {
    method: "POST",
    body: form,
  });
  return parseResponse<{
    restored: boolean;
    summary: RestoreSummary;
    emailSettings: EmailSettings;
  }>(res);
}
