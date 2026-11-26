import { useId, useState } from "react";
import type { FormEvent } from "react";
import { cvFileUrl } from "../api";
import type {
  JobBoard,
  ManualVacancyInput,
  VacancyWithEvaluation,
  WorkArrangement,
} from "../types";

interface VacanciesPanelProps {
  boards: JobBoard[];
  vacancies: VacancyWithEvaluation[];
  busyMessage: string | null;
  errorMessage: string | null;
  successMessage: string | null;
  evaluatingId: number | null;
  syncingBoardId: number | null;
  onCreateVacancy: (input: ManualVacancyInput) => Promise<number | null>;
  onDeleteVacancy: (id: number) => Promise<void>;
  onEvaluateVacancy: (id: number) => Promise<void>;
  onAddBoard: (provider: "greenhouse" | "lever", companyBoard: string, displayName: string) => Promise<void>;
  onSyncBoard: (id: number) => Promise<void>;
  onDeleteBoard: (id: number) => Promise<void>;
}

const SAMPLE_EN: ManualVacancyInput = {
  companyName: "Northstar Labs",
  title: "Senior Go Backend Engineer",
  location: "Remote - Americas",
  workArrangement: "remote",
  requestedLanguage: "",
  applicationEmail: "jobs@northstar.example",
  applicationUrl: "https://boards.greenhouse.io/northstar/jobs/4012",
  description:
    "We are looking for a Senior Go Backend Engineer to build local-first developer tools.\n\nRequirements:\n- Strong Go and SQLite experience\n- Designing and maintaining REST APIs\n- Production experience with Kubernetes and Rust kernel drivers\n\nNice to have:\n- TypeScript and React frontend experience\n\nPlease include your salary expectation when applying.",
};

const SAMPLE_ES: ManualVacancyInput = {
  companyName: "Soluciones Sur",
  title: "Ingeniero/a Backend Go",
  location: "Remoto LATAM",
  workArrangement: "remote",
  requestedLanguage: "",
  applicationEmail: "talento@solucionessur.example",
  applicationUrl: "https://jobs.lever.co/solucionessur/backend-go-2026",
  description:
    "Buscamos una persona para sumarse a nuestro equipo remoto construyendo servicios de alta disponibilidad.\n\nRequisitos:\n- Experiencia desarrollando en Go y SQLite\n- Diseño de APIs REST\n\nDeseable:\n- Conocimientos de React y TypeScript",
};

function stateBadgeClass(state?: string): string {
  switch (state) {
    case "sent":
    case "in_progress":
    case "interview_pending":
      return "badge-ok";
    case "discarded":
      return "badge-danger";
    case "pending":
      return "badge-warn";
    default:
      return "badge-muted";
  }
}

function formatStateLabel(state?: string): string {
  switch (state) {
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
    default:
      return "Not evaluated";
  }
}

export function VacanciesPanel({
  boards,
  vacancies,
  busyMessage,
  errorMessage,
  successMessage,
  evaluatingId,
  syncingBoardId,
  onCreateVacancy,
  onDeleteVacancy,
  onEvaluateVacancy,
  onAddBoard,
  onSyncBoard,
  onDeleteBoard,
}: VacanciesPanelProps) {
  const manualHeadingId = useId();
  const boardsHeadingId = useId();
  const listHeadingId = useId();
  const findingsCaptionId = useId();

  const titleId = useId();
  const companyId = useId();
  const locationId = useId();
  const emailId = useId();
  const urlId = useId();
  const descId = useId();
  const boardTokenId = useId();
  const boardDisplayId = useId();

  const [form, setForm] = useState<ManualVacancyInput>({
    companyName: "",
    title: "",
    location: "",
    workArrangement: "unknown",
    description: "",
    requestedLanguage: "",
    applicationEmail: "",
    applicationUrl: "",
  });

  const [boardProvider, setBoardProvider] = useState<"greenhouse" | "lever">("greenhouse");
  const [boardToken, setBoardToken] = useState("");
  const [boardDisplay, setBoardDisplay] = useState("");
  const [selectedVacancyId, setSelectedVacancyId] = useState<number | null>(
    vacancies[0]?.vacancy.id ?? null
  );

  const selectedItem =
    vacancies.find((v) => v.vacancy.id === selectedVacancyId) ?? vacancies[0] ?? null;

  const handleManualSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const createdId = await onCreateVacancy(form);
    if (createdId !== null) {
      setSelectedVacancyId(createdId);
      setForm({
        companyName: "",
        title: "",
        location: "",
        workArrangement: "unknown",
        description: "",
        requestedLanguage: "",
        applicationEmail: "",
        applicationUrl: "",
      });
    }
  };

  const handleBoardSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!boardToken.trim()) return;
    await onAddBoard(boardProvider, boardToken.trim(), boardDisplay.trim());
    setBoardToken("");
    setBoardDisplay("");
  };

  return (
    <div className="vacancies-layout">
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
      {busyMessage && (
        <div className="inline-alert alert-info" role="status" aria-live="polite">
          {busyMessage}
        </div>
      )}

      <div className="workspace-grid">
        <section className="card" aria-labelledby={manualHeadingId}>
          <div className="card-header">
            <div>
              <h2 id={manualHeadingId} className="card-title">
                Add Vacancy Manually
              </h2>
              <p className="card-subtitle">
                Paste a job opening in English or Spanish. Duplicates with the same canonical URL
                are linked automatically.
              </p>
            </div>
            <div className="sample-btn-row">
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                onClick={() => setForm(SAMPLE_EN)}
              >
                EN sample
              </button>
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                onClick={() => setForm(SAMPLE_ES)}
              >
                ES sample
              </button>
            </div>
          </div>

          <form className="profile-form" onSubmit={handleManualSubmit}>
            <div className="field-grid-two">
              <div className="field">
                <label htmlFor={titleId}>
                  Job title <span aria-hidden="true">*</span>
                </label>
                <input
                  id={titleId}
                  type="text"
                  required
                  value={form.title}
                  onChange={(e) => setForm({ ...form, title: e.target.value })}
                  placeholder="Senior Backend Engineer"
                />
              </div>

              <div className="field">
                <label htmlFor={companyId}>Company</label>
                <input
                  id={companyId}
                  type="text"
                  value={form.companyName}
                  onChange={(e) => setForm({ ...form, companyName: e.target.value })}
                  placeholder="Acme Corp"
                />
              </div>
            </div>

            <div className="field-grid-two">
              <div className="field">
                <label htmlFor={locationId}>Location</label>
                <input
                  id={locationId}
                  type="text"
                  value={form.location}
                  onChange={(e) => setForm({ ...form, location: e.target.value })}
                  placeholder="Remote / Madrid / Buenos Aires"
                />
              </div>

              <div className="field">
                <label htmlFor={emailId}>Application email (if listed)</label>
                <input
                  id={emailId}
                  type="email"
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  value={form.applicationEmail}
                  onChange={(e) => setForm({ ...form, applicationEmail: e.target.value })}
                  placeholder="jobs@company.com"
                />
              </div>
            </div>

            <div className="field">
              <label htmlFor={urlId}>Official application URL</label>
              <input
                id={urlId}
                type="url"
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
                value={form.applicationUrl}
                onChange={(e) => setForm({ ...form, applicationUrl: e.target.value })}
                placeholder="https://boards.greenhouse.io/company/jobs/12345"
              />
            </div>

            <div className="field-grid-two">
              <fieldset className="choice-fieldset">
                <legend>Work arrangement</legend>
                <div className="radio-inline-group">
                  {(["unknown", "remote", "hybrid", "onsite"] as WorkArrangement[]).map((opt) => (
                    <label key={opt} className="radio-pill">
                      <input
                        type="radio"
                        name="vacancyWorkArrangement"
                        value={opt}
                        checked={form.workArrangement === opt}
                        onChange={() => setForm({ ...form, workArrangement: opt })}
                      />
                      <span>{opt === "unknown" ? "Auto" : opt}</span>
                    </label>
                  ))}
                </div>
              </fieldset>

              <fieldset className="choice-fieldset">
                <legend>Requested CV language</legend>
                <div className="radio-inline-group">
                  {[
                    { value: "", label: "Auto" },
                    { value: "en", label: "EN" },
                    { value: "es", label: "ES" },
                    { value: "both", label: "Both" },
                  ].map((opt) => (
                    <label key={opt.label} className="radio-pill">
                      <input
                        type="radio"
                        name="vacancyReqLang"
                        value={opt.value}
                        checked={form.requestedLanguage === opt.value}
                        onChange={() => setForm({ ...form, requestedLanguage: opt.value })}
                      />
                      <span>{opt.label}</span>
                    </label>
                  ))}
                </div>
              </fieldset>
            </div>

            <div className="field">
              <label htmlFor={descId}>
                Job description <span aria-hidden="true">*</span>
              </label>
              <textarea
                id={descId}
                rows={5}
                required
                value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
                placeholder="Paste requirements, responsibilities, and any language or salary instructions..."
              />
            </div>

            <div className="form-actions">
              <button type="submit" className="btn btn-primary">
                Save vacancy
              </button>
            </div>
          </form>
        </section>

        <section className="card" aria-labelledby={boardsHeadingId}>
          <div className="card-header">
            <div>
              <h2 id={boardsHeadingId} className="card-title">
                Company Job Boards
              </h2>
              <p className="card-subtitle">
                Import openings from public Greenhouse and Lever boards with automatic
                deduplication.
              </p>
            </div>
          </div>

          <form className="profile-form" onSubmit={handleBoardSubmit}>
            <fieldset className="choice-fieldset">
              <legend>Provider</legend>
              <div className="radio-inline-group">
                <label className="radio-pill">
                  <input
                    type="radio"
                    name="boardProvider"
                    value="greenhouse"
                    checked={boardProvider === "greenhouse"}
                    onChange={() => setBoardProvider("greenhouse")}
                  />
                  <span>Greenhouse</span>
                </label>
                <label className="radio-pill">
                  <input
                    type="radio"
                    name="boardProvider"
                    value="lever"
                    checked={boardProvider === "lever"}
                    onChange={() => setBoardProvider("lever")}
                  />
                  <span>Lever</span>
                </label>
              </div>
            </fieldset>

            <div className="field-grid-two">
              <div className="field">
                <label htmlFor={boardTokenId}>
                  Board slug / token <span aria-hidden="true">*</span>
                </label>
                <input
                  id={boardTokenId}
                  type="text"
                  required
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  value={boardToken}
                  onChange={(e) => setBoardToken(e.target.value)}
                  placeholder={boardProvider === "greenhouse" ? "e.g. stripe" : "e.g. netflix"}
                />
              </div>
              <div className="field">
                <label htmlFor={boardDisplayId}>Display name</label>
                <input
                  id={boardDisplayId}
                  type="text"
                  value={boardDisplay}
                  onChange={(e) => setBoardDisplay(e.target.value)}
                  placeholder="Optional company label"
                />
              </div>
            </div>

            <div className="form-actions">
              <button type="submit" className="btn btn-secondary">
                Add job board
              </button>
            </div>
          </form>

          {boards.length === 0 ? (
            <p className="muted-meta">No company boards configured yet.</p>
          ) : (
            <ul className="cv-list">
              {boards.map((b) => (
                <li key={b.id} className="cv-list-item">
                  <div className="cv-list-main">
                    <div className="cv-list-title-row">
                      <span className="badge badge-lang">{b.provider.toUpperCase()}</span>
                      <span className="cv-filename">{b.displayName}</span>
                      <code className="inline-code">{b.companyBoard}</code>
                    </div>
                    <div className="muted-meta">
                      {b.lastSyncedAt
                        ? `Last synced ${new Date(b.lastSyncedAt).toLocaleString()}`
                        : "Not synced yet"}
                    </div>
                  </div>
                  <div className="cv-list-actions">
                    <button
                      type="button"
                      className="btn btn-primary btn-sm"
                      disabled={syncingBoardId === b.id}
                      onClick={() => void onSyncBoard(b.id)}
                    >
                      {syncingBoardId === b.id ? "Syncing..." : "Sync jobs"}
                    </button>
                    <button
                      type="button"
                      className="btn btn-ghost-danger btn-sm"
                      onClick={() => void onDeleteBoard(b.id)}
                    >
                      Remove
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>

      <section className="card" aria-labelledby={listHeadingId}>
        <div className="card-header">
          <div>
            <h2 id={listHeadingId} className="card-title">
              Imported Vacancies &amp; Bilingual Evaluation ({vacancies.length})
            </h2>
            <p className="card-subtitle">
              Evaluate openings against your active CV(s). Go verifies every cited excerpt directly
              against the selected CV text.
            </p>
          </div>
        </div>

        {vacancies.length === 0 ? (
          <p className="muted-meta">
            No vacancies imported yet. Add a manual vacancy above (or try the EN / ES samples) or
            sync a Greenhouse/Lever board.
          </p>
        ) : (
          <div className="vacancy-split">
            <ul className="vacancy-list">
              {vacancies.map((item) => {
                const { vacancy, application, latestEvaluation } = item;
                const isSelected = selectedItem?.vacancy.id === vacancy.id;
                return (
                  <li
                    key={vacancy.id}
                    className={`vacancy-row ${isSelected ? "vacancy-row-selected" : ""}`}
                  >
                    <button
                      type="button"
                      className="vacancy-select-btn"
                      onClick={() => setSelectedVacancyId(vacancy.id)}
                      aria-pressed={isSelected}
                    >
                      <div className="vacancy-row-head">
                        <span className="cv-filename">{vacancy.title}</span>
                        <span className={`badge ${stateBadgeClass(application?.state)}`}>
                          {formatStateLabel(application?.state)}
                        </span>
                      </div>
                      <div className="muted-meta">
                        {vacancy.companyName || "Company not specified"}
                        {vacancy.location ? ` · ${vacancy.location}` : ""} ·{" "}
                        {vacancy.workArrangement}
                      </div>
                      <div className="vacancy-tags">
                        <span className="badge badge-muted">
                          {vacancy.sourceType.toUpperCase()}
                        </span>
                        <span className="badge badge-lang">
                          Lang: {vacancy.detectedLanguage.toUpperCase()}
                          {vacancy.requestedLanguage
                            ? ` (req: ${vacancy.requestedLanguage.toUpperCase()})`
                            : ""}
                        </span>
                        {latestEvaluation && (
                          <span className="badge badge-ok">
                            Fit: {latestEvaluation.fitScore}%
                          </span>
                        )}
                      </div>
                    </button>

                    <div className="vacancy-row-actions">
                      <button
                        type="button"
                        className="btn btn-primary btn-sm"
                        disabled={evaluatingId === vacancy.id}
                        onClick={() => {
                          setSelectedVacancyId(vacancy.id);
                          void onEvaluateVacancy(vacancy.id);
                        }}
                      >
                        {evaluatingId === vacancy.id
                          ? "Evaluating..."
                          : latestEvaluation
                            ? "Re-evaluate"
                            : "Evaluate fit"}
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost-danger btn-sm"
                        onClick={() => void onDeleteVacancy(vacancy.id)}
                      >
                        Delete
                      </button>
                    </div>
                  </li>
                );
              })}
            </ul>

            {selectedItem && (
              <div className="evaluation-detail">
                <div className="eval-header">
                  <div>
                    <h3 className="eval-title">{selectedItem.vacancy.title}</h3>
                    <p className="muted-meta">
                      {selectedItem.vacancy.companyName || "Unknown company"} ·{" "}
                      {selectedItem.vacancy.location || "Location unspecified"} ·{" "}
                      {selectedItem.vacancy.workArrangement}
                    </p>
                  </div>
                  <div className="eval-header-actions">
                    {selectedItem.vacancy.applicationUrl && (
                      <a
                        href={selectedItem.vacancy.applicationUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="btn btn-secondary btn-sm"
                      >
                        Official listing
                      </a>
                    )}
                    <button
                      type="button"
                      className="btn btn-primary btn-sm"
                      disabled={evaluatingId === selectedItem.vacancy.id}
                      onClick={() => void onEvaluateVacancy(selectedItem.vacancy.id)}
                    >
                      {evaluatingId === selectedItem.vacancy.id ? "Evaluating..." : "Evaluate fit"}
                    </button>
                  </div>
                </div>

                {selectedItem.application && (
                  <div
                    className={`inline-alert ${
                      selectedItem.application.state === "discarded"
                        ? "alert-error"
                        : selectedItem.latestEvaluation?.passedThreshold &&
                            selectedItem.latestEvaluation.languageStatus === "ok" &&
                            selectedItem.latestEvaluation.missingAnswers.length === 0 &&
                            selectedItem.latestEvaluation.findings.every(
                              (f) => f.kind !== "mandatory" || (f.met && f.evidenceVerified)
                            )
                          ? "alert-success"
                          : "alert-warn"
                    }`}
                  >
                    <strong>{formatStateLabel(selectedItem.application.state)}:</strong>{" "}
                    {selectedItem.application.discardReason ||
                      selectedItem.application.pendingReason ||
                      "Evaluation recorded."}
                  </div>
                )}

                {selectedItem.latestEvaluation ? (
                  <>
                    <div className="eval-meta-grid">
                      <div className="eval-stat">
                        <span className="muted-meta">Fit Score</span>
                        <strong>{selectedItem.latestEvaluation.fitScore}%</strong>
                      </div>
                      <div className="eval-stat">
                        <span className="muted-meta">Language Selection</span>
                        <strong>{selectedItem.latestEvaluation.languageStatus}</strong>
                      </div>
                      <div className="eval-stat">
                        <span className="muted-meta">Evaluator</span>
                        <strong>{selectedItem.latestEvaluation.modelName}</strong>
                      </div>
                      <div className="eval-stat">
                        <span className="muted-meta">Selected CV(s)</span>
                        <div className="attached-cv-links">
                          {selectedItem.latestEvaluation.selectedCvs &&
                          selectedItem.latestEvaluation.selectedCvs.length > 0 ? (
                            selectedItem.latestEvaluation.selectedCvs.map((c) => (
                              <a
                                key={c.id}
                                href={cvFileUrl(c.id)}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="badge badge-ok"
                              >
                                {c.language.toUpperCase()} v{c.version}: {c.filename}
                              </a>
                            ))
                          ) : (
                            <span className="muted-meta">None selected</span>
                          )}
                        </div>
                      </div>
                    </div>

                    <p className="eval-summary">{selectedItem.latestEvaluation.summary}</p>

                    {selectedItem.latestEvaluation.findings.length > 0 && (
                      <div
                        tabIndex={0}
                        role="region"
                        aria-labelledby={findingsCaptionId}
                        className="table-wrapper"
                      >
                        <table className="findings-table">
                          <caption id={findingsCaptionId} className="table-caption">
                            Requirement Findings &amp; Go-Verified CV Evidence
                          </caption>
                          <thead>
                            <tr>
                              <th scope="col">Requirement</th>
                              <th scope="col">Priority</th>
                              <th scope="col">Verification</th>
                              <th scope="col">CV Evidence &amp; Explanation</th>
                            </tr>
                          </thead>
                          <tbody>
                            {selectedItem.latestEvaluation.findings.map((f, idx) => (
                              <tr key={`${f.requirement}-${idx}`}>
                                <th scope="row">{f.requirement}</th>
                                <td>
                                  <span
                                    className={`badge ${
                                      f.kind === "mandatory" ? "badge-lang" : "badge-muted"
                                    }`}
                                  >
                                    {f.kind === "mandatory" ? "Mandatory" : "Preference"}
                                  </span>
                                </td>
                                <td>
                                  <span
                                    className={`badge ${
                                      f.met && f.evidenceVerified
                                        ? "badge-ok"
                                        : f.kind === "mandatory"
                                          ? "badge-warn"
                                          : "badge-muted"
                                    }`}
                                  >
                                    {f.met && f.evidenceVerified
                                      ? "Verified in CV"
                                      : "Missing / Unverified"}
                                  </span>
                                </td>
                                <td>
                                  {f.evidenceExcerpt && (
                                    <blockquote className="cv-quote">
                                      &ldquo;{f.evidenceExcerpt}&rdquo;
                                    </blockquote>
                                  )}
                                  <div className="muted-meta">{f.reason}</div>
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    )}

                    {selectedItem.latestEvaluation.missingAnswers.length > 0 && (
                      <div className="missing-answers-box">
                        <h4 className="section-subheading">
                          Missing Answers (Review Required)
                        </h4>
                        <ul className="readiness-issues">
                          {selectedItem.latestEvaluation.missingAnswers.map((m) => (
                            <li key={m.key}>
                              <strong>{m.label}</strong> ({m.kind}): {m.reason || "Required by vacancy."}
                            </li>
                          ))}
                        </ul>
                      </div>
                    )}
                  </>
                ) : (
                  <p className="muted-meta">
                    Click <strong>Evaluate fit</strong> to run bilingual CV selection and
                    requirement verification for this vacancy.
                  </p>
                )}
              </div>
            )}
          </div>
        )}
      </section>
    </div>
  );
}
