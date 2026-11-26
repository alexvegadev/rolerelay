import { useId, useRef, useState } from "react";
import type { FormEvent } from "react";
import { cvFileUrl } from "../api";
import type { CVDocument, CVLanguage, LanguageMode } from "../types";

interface CvManagerProps {
  languageMode: LanguageMode;
  cvs: CVDocument[];
  uploading: boolean;
  errorMessage: string | null;
  successMessage: string | null;
  onUpload: (language: CVLanguage, file: File) => Promise<boolean>;
  onActivate: (id: number) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
  onInspect: (cv: CVDocument) => void;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

function formatDate(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function CvManager({
  languageMode,
  cvs,
  uploading,
  errorMessage,
  successMessage,
  onUpload,
  onActivate,
  onDelete,
  onInspect,
}: CvManagerProps) {
  const headingId = useId();
  const fileInputId = useId();
  const fileHintId = useId();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const defaultLang: CVLanguage = languageMode === "es" ? "es" : "en";
  const [selectedLanguage, setSelectedLanguage] = useState<CVLanguage>(defaultLang);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);

  const activeEN = cvs.find((c) => c.language === "en" && c.isActive);
  const activeES = cvs.find((c) => c.language === "es" && c.isActive);

  const needsEN = languageMode === "en" || languageMode === "both";
  const needsES = languageMode === "es" || languageMode === "both";

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setLocalError(null);
    if (!selectedFile) {
      setLocalError("Choose a PDF CV file before uploading.");
      fileInputRef.current?.focus();
      return;
    }
    const ok = await onUpload(selectedLanguage, selectedFile);
    if (ok) {
      setSelectedFile(null);
      if (fileInputRef.current) {
        fileInputRef.current.value = "";
      }
    }
  };

  return (
    <section className="card" aria-labelledby={headingId}>
      <div className="card-header">
        <div>
          <h2 id={headingId} className="card-title">
            CV Documents
          </h2>
          <p className="card-subtitle">
            Upload text-based PDF CVs. Saved in local SQLite with extracted text and version history.
          </p>
        </div>
      </div>

      <div className="cv-slots-grid">
        <div className={`cv-slot ${needsEN ? "cv-slot-required" : "cv-slot-optional"}`}>
          <div className="cv-slot-head">
            <span className="slot-lang-title">English CV (EN)</span>
            <span className={`badge ${activeEN ? "badge-ok" : needsEN ? "badge-warn" : "badge-muted"}`}>
              {activeEN ? `Active v${activeEN.version}` : needsEN ? "Required" : "Optional"}
            </span>
          </div>
          {activeEN ? (
            <div className="cv-slot-body">
              <div className="cv-filename">{activeEN.filename}</div>
              <div className="muted-meta">
                {activeEN.pageCount} {activeEN.pageCount === 1 ? "page" : "pages"} ·{" "}
                {formatBytes(activeEN.sizeBytes)} · {activeEN.extractedChars.toLocaleString()} chars
              </div>
              <div className="cv-slot-actions">
                <a
                  href={cvFileUrl(activeEN.id)}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="btn btn-secondary btn-sm"
                >
                  Open PDF
                </a>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  onClick={() => onInspect(activeEN)}
                >
                  Inspect text
                </button>
              </div>
            </div>
          ) : (
            <p className="muted-meta">No English PDF uploaded yet.</p>
          )}
        </div>

        <div className={`cv-slot ${needsES ? "cv-slot-required" : "cv-slot-optional"}`}>
          <div className="cv-slot-head">
            <span className="slot-lang-title">Spanish CV (ES)</span>
            <span className={`badge ${activeES ? "badge-ok" : needsES ? "badge-warn" : "badge-muted"}`}>
              {activeES ? `Active v${activeES.version}` : needsES ? "Required" : "Optional"}
            </span>
          </div>
          {activeES ? (
            <div className="cv-slot-body">
              <div className="cv-filename">{activeES.filename}</div>
              <div className="muted-meta">
                {activeES.pageCount} {activeES.pageCount === 1 ? "page" : "pages"} ·{" "}
                {formatBytes(activeES.sizeBytes)} · {activeES.extractedChars.toLocaleString()} chars
              </div>
              <div className="cv-slot-actions">
                <a
                  href={cvFileUrl(activeES.id)}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="btn btn-secondary btn-sm"
                >
                  Open PDF
                </a>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  onClick={() => onInspect(activeES)}
                >
                  Inspect text
                </button>
              </div>
            </div>
          ) : (
            <p className="muted-meta">No Spanish PDF uploaded yet.</p>
          )}
        </div>
      </div>

      <form
        className="upload-form"
        method="POST"
        action="/api/cvs"
        encType="multipart/form-data"
        onSubmit={handleSubmit}
        noValidate
      >
        <fieldset className="choice-fieldset">
          <legend>Upload a new PDF CV version</legend>
          <div className="radio-inline-group">
            <label className="radio-pill">
              <input
                type="radio"
                name="cvLanguage"
                value="en"
                checked={selectedLanguage === "en"}
                onChange={() => setSelectedLanguage("en")}
              />
              <span>English (EN)</span>
            </label>
            <label className="radio-pill">
              <input
                type="radio"
                name="cvLanguage"
                value="es"
                checked={selectedLanguage === "es"}
                onChange={() => setSelectedLanguage("es")}
              />
              <span>Spanish (ES)</span>
            </label>
          </div>
        </fieldset>

        <div className="field">
          <label htmlFor={fileInputId}>
            PDF file <span aria-hidden="true">*</span>
          </label>
          <span id={fileHintId} className="field-hint">
            Text-based PDF up to 20 MB. Scanned image-only PDFs without extractable text will be
            rejected with an error.
          </span>
          <input
            ref={fileInputRef}
            id={fileInputId}
            name="file"
            type="file"
            accept="application/pdf,.pdf"
            aria-describedby={fileHintId}
            onChange={(e) => {
              setLocalError(null);
              setSelectedFile(e.target.files?.[0] ?? null);
            }}
          />
        </div>

        {(localError || errorMessage) && (
          <div className="inline-alert alert-error" role="alert">
            <strong>Error:</strong> {localError || errorMessage}
          </div>
        )}
        {successMessage && (
          <div className="inline-alert alert-success" role="status" aria-live="polite">
            {successMessage}
          </div>
        )}

        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={uploading}>
            {uploading ? "Extracting & saving..." : "Upload CV PDF"}
          </button>
        </div>
      </form>

      {cvs.length > 0 && (
        <div className="cv-history">
          <h3 className="section-subheading">Saved CV Versions ({cvs.length})</h3>
          <ul className="cv-list">
            {cvs.map((cv) => (
              <li key={cv.id} className="cv-list-item">
                <div className="cv-list-main">
                  <div className="cv-list-title-row">
                    <span className="badge badge-lang">{cv.language.toUpperCase()}</span>
                    <span className="cv-filename">{cv.filename}</span>
                    <span className="muted-meta">v{cv.version}</span>
                    {cv.isActive && <span className="badge badge-ok">Active</span>}
                  </div>
                  <div className="muted-meta">
                    Uploaded {formatDate(cv.createdAt)} · {cv.pageCount}{" "}
                    {cv.pageCount === 1 ? "page" : "pages"} · {formatBytes(cv.sizeBytes)} ·{" "}
                    {cv.extractedChars.toLocaleString()} chars · SHA-256 {cv.sha256.slice(0, 10)}
                  </div>
                </div>
                <div className="cv-list-actions">
                  <a
                    href={cvFileUrl(cv.id)}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="btn btn-secondary btn-sm"
                  >
                    Open PDF
                  </a>
                  <button
                    type="button"
                    className="btn btn-secondary btn-sm"
                    onClick={() => onInspect(cv)}
                  >
                    Text
                  </button>
                  {!cv.isActive && (
                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      onClick={() => onActivate(cv.id)}
                    >
                      Set active
                    </button>
                  )}
                  <button
                    type="button"
                    className="btn btn-ghost-danger btn-sm"
                    onClick={() => onDelete(cv.id)}
                  >
                    Delete
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
