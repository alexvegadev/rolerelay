import { useCallback, useEffect, useState } from "react";
import {
  ApiError,
  activateCV,
  addBoard,
  createManualVacancy,
  deleteBoard,
  deleteCV,
  deleteVacancy,
  disconnectEmailCredentials,
  evaluateVacancy,
  exitApp,
  fetchApplications,
  fetchBoards,
  fetchEmailSettings,
  fetchModelSettings,
  fetchProfileBundle,
  fetchStatus,
  fetchVacancies,
  pauseWorker,
  queueApplication,
  resumeWorker,
  saveEmailSettings,
  saveModelSettings,
  saveProfile,
  syncBoard,
  testEmailConnection,
  updateApplicationState,
  uploadCV,
} from "./api";
import { ApplicationsPanel } from "./components/ApplicationsPanel";
import { CvManager } from "./components/CvManager";
import { CvPreviewDialog } from "./components/CvPreviewDialog";
import { ExitDialog } from "./components/ExitDialog";
import { ModelSettingsPanel } from "./components/ModelSettingsPanel";
import { ProfileForm } from "./components/ProfileForm";
import { SendingSettingsPanel } from "./components/SendingSettingsPanel";
import { VacanciesPanel } from "./components/VacanciesPanel";
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
  ProfileUpdatePayload,
  VacancyWithEvaluation,
} from "./types";

type ActiveSection = "profile" | "vacancies" | "applications" | "settings";

export function App() {
  const [section, setSection] = useState<ActiveSection>("profile");
  const [status, setStatus] = useState<AppStatus | null>(null);
  const [bundle, setBundle] = useState<ProfileBundle | null>(null);
  const [boards, setBoards] = useState<JobBoard[]>([]);
  const [vacancies, setVacancies] = useState<VacancyWithEvaluation[]>([]);
  const [applications, setApplications] = useState<ApplicationDetail[]>([]);
  const [modelSettings, setModelSettings] = useState<ModelSettings>({
    provider: "local_heuristic",
    endpointUrl: "",
    model: "",
    hasApiKey: false,
    timeoutSeconds: 45,
  });
  const [emailSettings, setEmailSettings] = useState<EmailSettings>({
    host: "",
    port: 587,
    username: "",
    fromName: "",
    fromEmail: "",
    tlsMode: "starttls",
    hasCredentials: false,
    needsReconnect: false,
    autoSendEnabled: false,
    sendingPaused: false,
    dailySendLimit: 10,
    sentToday: 0,
  });

  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [savingProfile, setSavingProfile] = useState(false);
  const [profileError, setProfileError] = useState<string | null>(null);
  const [profileSuccess, setProfileSuccess] = useState<string | null>(null);

  const [uploadingCV, setUploadingCV] = useState(false);
  const [cvError, setCvError] = useState<string | null>(null);
  const [cvSuccess, setCvSuccess] = useState<string | null>(null);
  const [previewCV, setPreviewCV] = useState<CVDocument | null>(null);

  const [vacBusy, setVacBusy] = useState<string | null>(null);
  const [vacError, setVacError] = useState<string | null>(null);
  const [vacSuccess, setVacSuccess] = useState<string | null>(null);
  const [evaluatingId, setEvaluatingId] = useState<number | null>(null);
  const [syncingBoardId, setSyncingBoardId] = useState<number | null>(null);

  const [appError, setAppError] = useState<string | null>(null);
  const [appSuccess, setAppSuccess] = useState<string | null>(null);
  const [busyAppId, setBusyAppId] = useState<number | null>(null);

  const [savingEmail, setSavingEmail] = useState(false);
  const [testingEmail, setTestingEmail] = useState(false);
  const [emailError, setEmailError] = useState<string | null>(null);
  const [emailSuccess, setEmailSuccess] = useState<string | null>(null);

  const [savingModel, setSavingModel] = useState(false);
  const [modelError, setModelError] = useState<string | null>(null);
  const [modelSuccess, setModelSuccess] = useState<string | null>(null);

  const [workerBusy, setWorkerBusy] = useState(false);
  const [exitOpen, setExitOpen] = useState(false);
  const [exiting, setExiting] = useState(false);
  const [stoppedMessage, setStoppedMessage] = useState<string | null>(null);

  const loadAll = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const [st, prof, brds, vacs, apps, mdl, eml] = await Promise.all([
        fetchStatus(),
        fetchProfileBundle(),
        fetchBoards(),
        fetchVacancies(),
        fetchApplications(),
        fetchModelSettings(),
        fetchEmailSettings(),
      ]);
      setStatus(st);
      setBundle(prof);
      setBoards(brds.boards);
      setVacancies(vacs.vacancies);
      setApplications(apps.applications);
      setModelSettings(mdl.settings);
      setEmailSettings(eml.settings);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : "Failed to connect to RoleRelay backend.");
    } finally {
      setLoading(false);
    }
  }, []);

  const refreshApplications = useCallback(async () => {
    try {
      const res = await fetchApplications();
      setApplications(res.applications);
      setEmailSettings(res.emailSettings);
    } catch {
      // Ignore background refresh transient errors
    }
  }, []);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  useEffect(() => {
    if (section !== "applications" || stoppedMessage) return;
    const timer = window.setInterval(() => {
      void refreshApplications();
    }, 4000);
    return () => window.clearInterval(timer);
  }, [section, stoppedMessage, refreshApplications]);

  const handleSaveProfile = async (payload: ProfileUpdatePayload) => {
    setSavingProfile(true);
    setProfileError(null);
    setProfileSuccess(null);
    try {
      const updated = await saveProfile(payload);
      setBundle(updated);
      setProfileSuccess("Profile saved to local SQLite.");
    } catch (err) {
      setProfileError(err instanceof ApiError ? err.message : "Could not save profile.");
    } finally {
      setSavingProfile(false);
    }
  };

  const handleUploadCV = async (language: CVLanguage, file: File): Promise<boolean> => {
    setUploadingCV(true);
    setCvError(null);
    setCvSuccess(null);
    try {
      const result = await uploadCV(language, file);
      setBundle((prev) => (prev ? { ...prev, cvs: result.cvs, readiness: result.readiness } : prev));
      setCvSuccess(
        `Saved ${result.cv.filename} (${result.cv.language.toUpperCase()} v${result.cv.version}, ${result.cv.extractedChars.toLocaleString()} characters extracted).`
      );
      return true;
    } catch (err) {
      setCvError(err instanceof ApiError ? err.message : "Failed to upload CV.");
      return false;
    } finally {
      setUploadingCV(false);
    }
  };

  const handleActivateCV = async (id: number) => {
    setCvError(null);
    setCvSuccess(null);
    try {
      const result = await activateCV(id);
      setBundle((prev) => (prev ? { ...prev, cvs: result.cvs, readiness: result.readiness } : prev));
      setCvSuccess(`Activated ${result.cv.filename} (v${result.cv.version}).`);
    } catch (err) {
      setCvError(err instanceof ApiError ? err.message : "Failed to activate CV.");
    }
  };

  const handleDeleteCV = async (id: number) => {
    setCvError(null);
    setCvSuccess(null);
    try {
      const result = await deleteCV(id);
      setBundle((prev) => (prev ? { ...prev, cvs: result.cvs, readiness: result.readiness } : prev));
      setCvSuccess("CV version deleted.");
    } catch (err) {
      setCvError(err instanceof ApiError ? err.message : "Failed to delete CV.");
    }
  };

  const handleCreateVacancy = async (input: ManualVacancyInput): Promise<number | null> => {
    setVacError(null);
    setVacSuccess(null);
    setVacBusy("Saving vacancy...");
    try {
      const res = await createManualVacancy(input);
      setVacancies((prev) => {
        const filtered = prev.filter((v) => v.vacancy.id !== res.item.vacancy.id);
        return [res.item, ...filtered];
      });
      setVacSuccess(
        res.deduplicated
          ? `Linked to existing vacancy (${res.matchedBy}) and updated details.`
          : `Saved "${res.item.vacancy.title}" (detected language: ${res.item.vacancy.detectedLanguage.toUpperCase()}).`
      );
      return res.item.vacancy.id;
    } catch (err) {
      setVacError(err instanceof ApiError ? err.message : "Could not save vacancy.");
      return null;
    } finally {
      setVacBusy(null);
    }
  };

  const handleDeleteVacancy = async (id: number) => {
    setVacError(null);
    setVacSuccess(null);
    try {
      await deleteVacancy(id);
      setVacancies((prev) => prev.filter((v) => v.vacancy.id !== id));
      await refreshApplications();
      setVacSuccess("Vacancy deleted.");
    } catch (err) {
      setVacError(err instanceof ApiError ? err.message : "Could not delete vacancy.");
    }
  };

  const handleEvaluateVacancy = async (id: number) => {
    setVacError(null);
    setVacSuccess(null);
    setEvaluatingId(id);
    try {
      const res = await evaluateVacancy(id);
      setVacancies((prev) =>
        prev.map((v) => (v.vacancy.id === res.item.vacancy.id ? res.item : v))
      );
      await refreshApplications();
      setVacSuccess(
        `Evaluated "${res.item.vacancy.title}" — ${res.item.application?.state ?? "updated"}.`
      );
    } catch (err) {
      setVacError(err instanceof ApiError ? err.message : "Evaluation failed.");
    } finally {
      setEvaluatingId(null);
    }
  };

  const handleAddBoard = async (
    provider: "greenhouse" | "lever",
    companyBoard: string,
    displayName: string
  ) => {
    setVacError(null);
    setVacSuccess(null);
    try {
      const res = await addBoard({ provider, companyBoard, displayName });
      setBoards((prev) => {
        const filtered = prev.filter((b) => b.id !== res.board.id);
        return [...filtered, res.board];
      });
      setVacSuccess(`Added ${res.board.provider} board "${res.board.displayName}".`);
    } catch (err) {
      setVacError(err instanceof ApiError ? err.message : "Could not add job board.");
    }
  };

  const handleSyncBoard = async (id: number) => {
    setVacError(null);
    setVacSuccess(null);
    setSyncingBoardId(id);
    try {
      const res = await syncBoard(id);
      setBoards((prev) => prev.map((b) => (b.id === res.board.id ? res.board : b)));
      setVacancies(res.vacancies);
      setVacSuccess(
        `Synced ${res.board.displayName}: ${res.fetched} fetched, ${res.created} new, ${res.deduplicated} deduplicated.`
      );
    } catch (err) {
      setVacError(err instanceof ApiError ? err.message : "Board sync failed.");
    } finally {
      setSyncingBoardId(null);
    }
  };

  const handleDeleteBoard = async (id: number) => {
    setVacError(null);
    setVacSuccess(null);
    try {
      await deleteBoard(id);
      setBoards((prev) => prev.filter((b) => b.id !== id));
      setVacSuccess("Job board removed.");
    } catch (err) {
      setVacError(err instanceof ApiError ? err.message : "Could not remove job board.");
    }
  };

  const handleQueueApplication = async (appId: number, confirmNotSent = false) => {
    setAppError(null);
    setAppSuccess(null);
    setBusyAppId(appId);
    try {
      const res = await queueApplication(appId, confirmNotSent);
      setApplications((prev) =>
        prev.map((a) => (a.application.id === appId ? res.application : a))
      );
      setAppSuccess("Application queued for email dispatch.");
    } catch (err) {
      setAppError(err instanceof ApiError ? err.message : "Could not queue application.");
    } finally {
      setBusyAppId(null);
    }
  };

  const handleUpdateAppState = async (
    appId: number,
    state: ApplicationState,
    reason: string
  ) => {
    setAppError(null);
    setAppSuccess(null);
    setBusyAppId(appId);
    try {
      const res = await updateApplicationState(appId, state, reason);
      setApplications((prev) =>
        prev.map((a) => (a.application.id === appId ? res.application : a))
      );
      setAppSuccess(`Updated status to ${state}.`);
    } catch (err) {
      setAppError(err instanceof ApiError ? err.message : "Could not update status.");
    } finally {
      setBusyAppId(null);
    }
  };

  const handleSaveEmailSettings = async (next: EmailSettings) => {
    setSavingEmail(true);
    setEmailError(null);
    setEmailSuccess(null);
    try {
      const res = await saveEmailSettings(next);
      setEmailSettings(res.settings);
      const st = await fetchStatus();
      setStatus(st);
      setEmailSuccess("Sending account settings saved (password stored in OS credential store).");
    } catch (err) {
      setEmailError(err instanceof ApiError ? err.message : "Could not save sending settings.");
    } finally {
      setSavingEmail(false);
    }
  };

  const handleTestEmail = async (recipient: string) => {
    setTestingEmail(true);
    setEmailError(null);
    setEmailSuccess(null);
    try {
      const res = await testEmailConnection(recipient);
      setEmailSuccess(`Test email accepted for ${res.recipient} (${res.messageId}).`);
    } catch (err) {
      setEmailError(err instanceof ApiError ? err.message : "SMTP test failed.");
    } finally {
      setTestingEmail(false);
    }
  };

  const handleDisconnectEmail = async () => {
    setEmailError(null);
    setEmailSuccess(null);
    try {
      const res = await disconnectEmailCredentials();
      setEmailSettings(res.settings);
      setEmailSuccess("Sending credentials removed from OS credential store and auto-send disabled.");
    } catch (err) {
      setEmailError(err instanceof ApiError ? err.message : "Could not remove credentials.");
    }
  };

  const handleSaveModelSettings = async (next: ModelSettings) => {
    setSavingModel(true);
    setModelError(null);
    setModelSuccess(null);
    try {
      const res = await saveModelSettings(next);
      setModelSettings(res.settings);
      setModelSuccess("Evaluation model settings saved.");
    } catch (err) {
      setModelError(err instanceof ApiError ? err.message : "Could not save model settings.");
    } finally {
      setSavingModel(false);
    }
  };

  const handleToggleWorker = async () => {
    if (!status) return;
    setWorkerBusy(true);
    try {
      const res = status.worker.paused ? await resumeWorker() : await pauseWorker();
      setStatus((prev) => (prev ? { ...prev, worker: res.worker } : prev));
      setEmailSettings((prev) => ({ ...prev, sendingPaused: res.worker.paused }));
    } finally {
      setWorkerBusy(false);
    }
  };

  const handleConfirmExit = async () => {
    setExiting(true);
    try {
      const res = await exitApp();
      setExitOpen(false);
      setStoppedMessage(res.message);
    } catch {
      setExitOpen(false);
      setStoppedMessage("RoleRelay has stopped. You can close this browser tab.");
    } finally {
      setExiting(false);
    }
  };

  if (stoppedMessage) {
    return (
      <main className="stopped-screen">
        <div className="card stopped-card">
          <h1>RoleRelay stopped</h1>
          <p>{stoppedMessage}</p>
          <p className="muted-meta">
            You can close this tab now. Launch RoleRelay again whenever you want to resume.
          </p>
        </div>
      </main>
    );
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="topbar-inner">
          <div className="brand-block">
            <span className="brand-mark" aria-hidden="true">
              RR
            </span>
            <div>
              <h1 className="brand-name">RoleRelay</h1>
              <p className="brand-tagline">Local AI job evaluation &amp; application assistant</p>
            </div>
          </div>

          <div className="topbar-controls">
            {status && (
              <>
                <span className="pill pill-neutral" title={status.dbPath}>
                  Storage: <strong>{status.storageMode}</strong>
                </span>
                <button
                  type="button"
                  className={`pill pill-button ${status.worker.paused ? "pill-warn" : "pill-ok"}`}
                  onClick={handleToggleWorker}
                  disabled={workerBusy}
                >
                  Worker: <strong>{status.worker.paused ? "Paused" : "Running"}</strong>
                </button>
              </>
            )}
            <button
              type="button"
              className="btn btn-danger-outline btn-sm"
              onClick={() => setExitOpen(true)}
            >
              Exit
            </button>
          </div>
        </div>

        <nav className="section-nav" aria-label="Workspace sections">
          <div className="section-nav-inner">
            <button
              type="button"
              className="nav-item"
              aria-current={section === "profile" ? "page" : undefined}
              onClick={() => setSection("profile")}
            >
              Profile &amp; CVs
            </button>
            <button
              type="button"
              className="nav-item"
              aria-current={section === "vacancies" ? "page" : undefined}
              onClick={() => setSection("vacancies")}
            >
              Vacancies &amp; Evaluation ({vacancies.length})
            </button>
            <button
              type="button"
              className="nav-item"
              aria-current={section === "applications" ? "page" : undefined}
              onClick={() => {
                setSection("applications");
                void refreshApplications();
              }}
            >
              Applications &amp; Queue ({applications.length})
            </button>
            <button
              type="button"
              className="nav-item"
              aria-current={section === "settings" ? "page" : undefined}
              onClick={() => setSection("settings")}
            >
              Sending &amp; AI Settings
            </button>
          </div>
        </nav>
      </header>

      <main className="main-container">
        {loading && (
          <div className="card" role="status">
            Loading RoleRelay workspace...
          </div>
        )}

        {!loading && loadError && (
          <div className="card inline-alert alert-error" role="alert">
            <div>
              <strong>Error:</strong> {loadError}
            </div>
            <button type="button" className="btn btn-secondary btn-sm" onClick={() => void loadAll()}>
              Retry
            </button>
          </div>
        )}

        {!loading && bundle && status && (
          <>
            <section
              className={`readiness-banner ${bundle.readiness.ready ? "readiness-ok" : "readiness-warn"}`}
              aria-live="polite"
            >
              <div>
                <div className="readiness-title">
                  {bundle.readiness.ready ? "Profile & CVs ready" : "Complete your profile setup"}
                </div>
                {bundle.readiness.ready ? (
                  <p className="readiness-desc">
                    Required CV documents are saved in SQLite and ready for bilingual evaluation and
                    submission.
                  </p>
                ) : (
                  <ul className="readiness-issues">
                    {bundle.readiness.issues.map((issue) => (
                      <li key={issue}>{issue}</li>
                    ))}
                  </ul>
                )}
              </div>
              <div className="storage-meta">
                <div className="muted-meta">Database file</div>
                <code className="db-path">{status.dbPath}</code>
              </div>
            </section>

            {section === "profile" && (
              <div className="workspace-grid">
                <ProfileForm
                  profile={bundle.profile}
                  saving={savingProfile}
                  errorMessage={profileError}
                  successMessage={profileSuccess}
                  onSave={handleSaveProfile}
                />

                <CvManager
                  languageMode={bundle.profile.languageMode}
                  cvs={bundle.cvs}
                  uploading={uploadingCV}
                  errorMessage={cvError}
                  successMessage={cvSuccess}
                  onUpload={handleUploadCV}
                  onActivate={handleActivateCV}
                  onDelete={handleDeleteCV}
                  onInspect={(cv) => setPreviewCV(cv)}
                />
              </div>
            )}

            {section === "vacancies" && (
              <VacanciesPanel
                boards={boards}
                vacancies={vacancies}
                busyMessage={vacBusy}
                errorMessage={vacError}
                successMessage={vacSuccess}
                evaluatingId={evaluatingId}
                syncingBoardId={syncingBoardId}
                onCreateVacancy={handleCreateVacancy}
                onDeleteVacancy={handleDeleteVacancy}
                onEvaluateVacancy={handleEvaluateVacancy}
                onAddBoard={handleAddBoard}
                onSyncBoard={handleSyncBoard}
                onDeleteBoard={handleDeleteBoard}
              />
            )}

            {section === "applications" && (
              <ApplicationsPanel
                applications={applications}
                emailSettings={emailSettings}
                errorMessage={appError}
                successMessage={appSuccess}
                busyAppId={busyAppId}
                onRefresh={refreshApplications}
                onQueue={handleQueueApplication}
                onUpdateState={handleUpdateAppState}
              />
            )}

            {section === "settings" && (
              <div className="vacancies-layout">
                <SendingSettingsPanel
                  settings={emailSettings}
                  saving={savingEmail}
                  testing={testingEmail}
                  errorMessage={emailError}
                  successMessage={emailSuccess}
                  onSave={handleSaveEmailSettings}
                  onTest={handleTestEmail}
                  onDisconnect={handleDisconnectEmail}
                />

                <ModelSettingsPanel
                  settings={modelSettings}
                  saving={savingModel}
                  errorMessage={modelError}
                  successMessage={modelSuccess}
                  onSave={handleSaveModelSettings}
                />
              </div>
            )}

            <footer className="phase-footer">
              <span>
                RoleRelay v{status.version} · Schema v{status.schemaVersion} · Loopback{" "}
                <code>{status.address}</code>
              </span>
              <span>
                Phase 3 Automatic Submission active · Next: A2UI Review Panel, Backup &amp; Restore
              </span>
            </footer>
          </>
        )}
      </main>

      <ExitDialog
        open={exitOpen}
        exiting={exiting}
        onCancel={() => setExitOpen(false)}
        onConfirm={() => void handleConfirmExit()}
      />

      <CvPreviewDialog cv={previewCV} onClose={() => setPreviewCV(null)} />
    </div>
  );
}
