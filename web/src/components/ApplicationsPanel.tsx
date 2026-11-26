import { useId, useState } from "react";
import { cvFileUrl } from "../api";
import type {
  ApplicationDetail,
  ApplicationState,
  EmailSettings,
} from "../types";

interface ApplicationsPanelProps {
  applications: ApplicationDetail[];
  emailSettings: EmailSettings;
  errorMessage: string | null;
  successMessage: string | null;
  busyAppId: number | null;
  onRefresh: () => Promise<void>;
  onQueue: (appId: number, confirmNotSent?: boolean) => Promise<void>;
  onUpdateState: (appId: number, state: ApplicationState, reason: string) => Promise<void>;
}

const STATE_TABS: { key: "all" | ApplicationState; label: string }[] = [
  { key: "all", label: "All" },
  { key: "pending", label: "Pending" },
  { key: "sent", label: "Sent" },
  { key: "in_progress", label: "In Progress" },
  { key: "interview_pending", label: "Interview Pending" },
  { key: "discarded", label: "Discarded" },
];

function formatState(s: ApplicationState): string {
  switch (s) {
    case "pending":
      return "Pending";
    case "sent":
      return "Sent";
    case "in_progress":
      return "In Progress";
    case "interview_pending":
      return "Interview Pending";
    case "discarded":
      return "Discarded";
  }
}

function stateBadge(s: ApplicationState): string {
  switch (s) {
    case "sent":
    case "in_progress":
    case "interview_pending":
      return "badge-ok";
    case "discarded":
      return "badge-danger";
    default:
      return "badge-warn";
  }
}

function attemptBadge(status: string): string {
  switch (status) {
    case "sent":
      return "badge-ok";
    case "uncertain":
    case "failed":
      return "badge-danger";
    default:
      return "badge-warn";
  }
}

export function ApplicationsPanel({
  applications,
  emailSettings,
  errorMessage,
  successMessage,
  busyAppId,
  onRefresh,
  onQueue,
  onUpdateState,
}: ApplicationsPanelProps) {
  const headingId = useId();
  const [filter, setFilter] = useState<"all" | ApplicationState>("all");
  const [editingStateId, setEditingStateId] = useState<number | null>(null);
  const [nextState, setNextState] = useState<ApplicationState>("in_progress");
  const [stateReason, setStateReason] = useState("");

  const filtered =
    filter === "all"
      ? applications
      : applications.filter((a) => a.application.state === filter);

  const countFor = (k: "all" | ApplicationState) =>
    k === "all"
      ? applications.length
      : applications.filter((a) => a.application.state === k).length;

  return (
    <div className="vacancies-layout">
      <section className="card" aria-labelledby={headingId}>
        <div className="card-header">
          <div>
            <h2 id={headingId} className="card-title">
              Applications &amp; Submission Queue
            </h2>
            <p className="card-subtitle">
              Track all five states, verify uncertain sends after interruptions, and record manual
              submissions or employer updates.
            </p>
          </div>
          <button type="button" className="btn btn-secondary btn-sm" onClick={() => void onRefresh()}>
            Refresh
          </button>
        </div>

        <div className="eval-meta-grid">
          <div className="eval-stat">
            <span className="muted-meta">Sending Account</span>
            <strong>
              {emailSettings.needsReconnect
                ? "Reconnect needed"
                : emailSettings.hasCredentials && emailSettings.host
                  ? emailSettings.fromEmail || emailSettings.username || emailSettings.host
                  : "Not configured"}
            </strong>
          </div>
          <div className="eval-stat">
            <span className="muted-meta">Auto-Send</span>
            <strong>
              {emailSettings.autoSendEnabled
                ? emailSettings.sendingPaused
                  ? "Enabled (Paused)"
                  : "Active"
                : "Disabled"}
            </strong>
          </div>
          <div className="eval-stat">
            <span className="muted-meta">Daily Send Limit</span>
            <strong>
              {emailSettings.sentToday} / {emailSettings.dailySendLimit} sent today
            </strong>
          </div>
          <div className="eval-stat">
            <span className="muted-meta">Queued Now</span>
            <strong>
              {applications.filter((a) => a.application.queuedForSend).length} waiting
            </strong>
          </div>
        </div>

        <div className="filter-pill-bar" role="group" aria-label="Filter applications by state">
          {STATE_TABS.map((t) => (
            <button
              key={t.key}
              type="button"
              className={`pill pill-button ${filter === t.key ? "pill-active" : "pill-neutral"}`}
              aria-pressed={filter === t.key}
              onClick={() => setFilter(t.key)}
            >
              {t.label} ({countFor(t.key)})
            </button>
          ))}
        </div>

        {errorMessage && (
          <div className="inline-alert alert-error" role="alert">
            <strong>Error:</strong> {errorMessage}
          </div>
        )}
        {successMessage && (
          <div className="inline-alert alert-success" role="status" aria-live="polite">
            {successMessage}
          </div>
        )}

        {filtered.length === 0 ? (
          <p className="muted-meta">
            No applications match this filter yet. Evaluate a vacancy in{" "}
            <strong>Vacancies &amp; Evaluation</strong> to create one.
          </p>
        ) : (
          <ul className="application-cards">
            {filtered.map((item) => {
              const { application: app, vacancy } = item;
              const isBusy = busyAppId === app.id;
              return (
                <li key={app.id} className="application-card">
                  <div className="application-card-head">
                    <div>
                      <div className="cv-list-title-row">
                        <span className="cv-filename">{vacancy.title}</span>
                        <span className={`badge ${stateBadge(app.state)}`}>
                          {formatState(app.state)}
                        </span>
                        {app.queuedForSend && <span className="badge badge-lang">Queued</span>}
                        {item.hasUncertainAttempt && (
                          <span className="badge badge-danger">Uncertain delivery</span>
                        )}
                      </div>
                      <p className="muted-meta">
                        {vacancy.companyName || "Unknown company"} · Channel: {app.channel}
                        {app.recipient ? ` → ${app.recipient}` : ""}
                      </p>
                    </div>

                    <div className="attached-cv-links">
                      {app.attachedCvs?.map((c) => (
                        <a
                          key={c.id}
                          href={cvFileUrl(c.id)}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="badge badge-ok"
                        >
                          {c.language.toUpperCase()} v{c.version}: {c.filename}
                        </a>
                      ))}
                    </div>
                  </div>

                  {(app.pendingReason || app.discardReason) && (
                    <div
                      className={`inline-alert ${
                        item.hasUncertainAttempt || app.state === "discarded"
                          ? "alert-error"
                          : "alert-warn"
                      }`}
                    >
                      {app.discardReason || app.pendingReason}
                    </div>
                  )}

                  <div className="application-actions">
                    {item.hasUncertainAttempt && app.state === "pending" && (
                      <>
                        <button
                          type="button"
                          className="btn btn-primary btn-sm"
                          disabled={isBusy}
                          onClick={() =>
                            void onUpdateState(
                              app.id,
                              "sent",
                              "User verified delivery in Sent folder after uncertain attempt"
                            )
                          }
                        >
                          Confirm it was Sent
                        </button>
                        <button
                          type="button"
                          className="btn btn-secondary btn-sm"
                          disabled={isBusy}
                          onClick={() => void onQueue(app.id, true)}
                        >
                          Confirmed NOT sent — Requeue
                        </button>
                      </>
                    )}

                    {!item.hasUncertainAttempt &&
                      item.canQueue &&
                      !app.queuedForSend &&
                      app.state === "pending" && (
                        <button
                          type="button"
                          className="btn btn-primary btn-sm"
                          disabled={isBusy}
                          onClick={() => void onQueue(app.id, false)}
                        >
                          Queue for Email Send
                        </button>
                      )}

                    {app.externalApplyUrl && (
                      <a
                        href={app.externalApplyUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="btn btn-secondary btn-sm"
                      >
                        Open Official Form
                      </a>
                    )}

                    {app.state === "pending" && app.channel !== "email" && (
                      <button
                        type="button"
                        className="btn btn-primary btn-sm"
                        disabled={isBusy}
                        onClick={() =>
                          void onUpdateState(
                            app.id,
                            "sent",
                            "User completed manual submission via official form"
                          )
                        }
                      >
                        Record Manual Submission
                      </button>
                    )}

                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      onClick={() => {
                        if (editingStateId === app.id) {
                          setEditingStateId(null);
                        } else {
                          setEditingStateId(app.id);
                          setNextState(app.state === "sent" ? "in_progress" : "interview_pending");
                          setStateReason("");
                        }
                      }}
                    >
                      {editingStateId === app.id ? "Close status update" : "Update status"}
                    </button>
                  </div>

                  {editingStateId === app.id && (
                    <form
                      className="state-update-form"
                      onSubmit={async (e) => {
                        e.preventDefault();
                        await onUpdateState(app.id, nextState, stateReason.trim());
                        setEditingStateId(null);
                      }}
                    >
                      <fieldset className="choice-fieldset">
                        <legend>New application state</legend>
                        <div className="radio-inline-group">
                          {(
                            [
                              "pending",
                              "sent",
                              "in_progress",
                              "interview_pending",
                              "discarded",
                            ] as ApplicationState[]
                          ).map((st) => (
                            <label key={st} className="radio-pill">
                              <input
                                type="radio"
                                name={`state-${app.id}`}
                                value={st}
                                checked={nextState === st}
                                onChange={() => setNextState(st)}
                              />
                              <span>{formatState(st)}</span>
                            </label>
                          ))}
                        </div>
                      </fieldset>
                      <div className="field">
                        <label htmlFor={`reason-${app.id}`}>Reason / employer update</label>
                        <input
                          id={`reason-${app.id}`}
                          type="text"
                          value={stateReason}
                          onChange={(e) => setStateReason(e.target.value)}
                          placeholder="e.g. Recruiter scheduled technical interview for Tuesday"
                        />
                      </div>
                      <div className="form-actions">
                        <button type="submit" className="btn btn-primary btn-sm" disabled={isBusy}>
                          Save status update
                        </button>
                      </div>
                    </form>
                  )}

                  {item.submissionAttempts.length > 0 && (
                    <div className="sub-log-block">
                      <div className="sub-log-title">
                        Submission Attempts ({item.submissionAttempts.length})
                      </div>
                      <ul className="sub-log-list">
                        {item.submissionAttempts.map((att) => (
                          <li key={att.id} className="sub-log-row">
                            <span className={`badge ${attemptBadge(att.status)}`}>
                              #{att.attemptNumber} {att.status}
                            </span>
                            <span>{att.recipient}</span>
                            {att.providerMessageId && (
                              <code className="inline-code">{att.providerMessageId}</code>
                            )}
                            {att.errorMessage && (
                              <span className="muted-meta">{att.errorMessage}</span>
                            )}
                            <span className="muted-meta">
                              {new Date(att.startedAt).toLocaleString()}
                            </span>
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}

                  {item.stateHistory.length > 0 && (
                    <details className="history-details">
                      <summary>State change history ({item.stateHistory.length})</summary>
                      <ul className="sub-log-list">
                        {item.stateHistory.map((sc) => (
                          <li key={sc.id} className="sub-log-row">
                            <span className="badge badge-muted">{sc.source}</span>
                            <span>
                              {sc.fromState ? `${sc.fromState} → ` : ""}
                              <strong>{sc.toState}</strong>
                            </span>
                            <span className="muted-meta">{sc.reason}</span>
                            <span className="muted-meta">
                              {new Date(sc.createdAt).toLocaleString()}
                            </span>
                          </li>
                        ))}
                      </ul>
                    </details>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}
