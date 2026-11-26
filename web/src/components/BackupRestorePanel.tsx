import { useEffect, useId, useRef, useState } from "react";
import { backupExportUrl } from "../api";
import type { RestoreSummary } from "../api";

interface BackupRestorePanelProps {
  dbPath: string;
  restoring: boolean;
  errorMessage: string | null;
  successMessage: string | null;
  lastSummary: RestoreSummary | null;
  onRestore: (file: File) => Promise<boolean>;
}

export function BackupRestorePanel({
  dbPath,
  restoring,
  errorMessage,
  successMessage,
  lastSummary,
  onRestore,
}: BackupRestorePanelProps) {
  const headingId = useId();
  const fileInputId = useId();
  const fileHintId = useId();
  const dialogTitleId = useId();
  const dialogDescId = useId();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLDialogElement>(null);

  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (confirmOpen && !dialog.open) {
      dialog.showModal();
    } else if (!confirmOpen && dialog.open) {
      dialog.close();
    }
  }, [confirmOpen]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog || "closedBy" in HTMLDialogElement.prototype) return;

    let pressedOutside = false;
    const isOutside = (e: PointerEvent) => {
      const rect = dialog.getBoundingClientRect();
      return (
        e.clientX < rect.left ||
        e.clientX > rect.right ||
        e.clientY < rect.top ||
        e.clientY > rect.bottom
      );
    };
    const onPointerDown = (e: PointerEvent) => {
      pressedOutside = isOutside(e);
    };
    const onPointerUp = (e: PointerEvent) => {
      if (pressedOutside && isOutside(e) && dialog.open) {
        dialog.close();
      }
      pressedOutside = false;
    };

    dialog.addEventListener("pointerdown", onPointerDown);
    dialog.addEventListener("pointerup", onPointerUp);
    return () => {
      dialog.removeEventListener("pointerdown", onPointerDown);
      dialog.removeEventListener("pointerup", onPointerUp);
    };
  }, []);

  const handleDialogClose = () => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const result = dialog.returnValue;
    dialog.returnValue = "";
    setConfirmOpen(false);
    if (result === "confirm" && selectedFile) {
      void onRestore(selectedFile).then((ok) => {
        if (ok) {
          setSelectedFile(null);
          if (fileInputRef.current) {
            fileInputRef.current.value = "";
          }
        }
      });
    }
  };

  return (
    <section className="card" aria-labelledby={headingId}>
      <div className="card-header">
        <div>
          <h2 id={headingId} className="card-title">
            SQLite Backup &amp; Restore
          </h2>
          <p className="card-subtitle">
            Export a consistent single-file SQLite snapshot of your profile, both CV versions,
            vacancies, evaluation evidence, and submission history. Sending passwords remain in the
            OS credential store and must be reconnected after moving to another machine.
          </p>
        </div>
      </div>

      <div className="workspace-grid">
        <div className="cv-slot">
          <div className="slot-lang-title">Export consistent backup</div>
          <p className="muted-meta">
            Active database: <code className="inline-code">{dbPath}</code>
          </p>
          <p className="muted-meta">
            Uses SQLite <code>VACUUM INTO</code> with integrity verification so the download is safe
            while the worker is active.
          </p>
          <div className="form-actions">
            <a href={backupExportUrl()} download className="btn btn-primary btn-sm">
              Download SQLite Backup
            </a>
          </div>
        </div>

        <div className="cv-slot">
          <div className="slot-lang-title">Restore from backup</div>
          <div className="field">
            <label htmlFor={fileInputId}>Backup file (.sqlite)</label>
            <span id={fileHintId} className="field-hint">
              Validated for SQLite integrity and RoleRelay schema before replacing local data.
            </span>
            <input
              ref={fileInputRef}
              id={fileInputId}
              type="file"
              accept=".sqlite,.db,application/vnd.sqlite3,application/x-sqlite3"
              aria-describedby={fileHintId}
              onChange={(e) => setSelectedFile(e.target.files?.[0] ?? null)}
            />
          </div>
          <div className="form-actions">
            <button
              type="button"
              className="btn btn-danger-outline btn-sm"
              disabled={!selectedFile || restoring}
              onClick={() => setConfirmOpen(true)}
            >
              {restoring ? "Restoring backup..." : "Restore backup"}
            </button>
          </div>
        </div>
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
      {lastSummary && (
        <div className="eval-meta-grid">
          <div className="eval-stat">
            <span className="muted-meta">CVs Restored</span>
            <strong>{lastSummary.cvCount}</strong>
          </div>
          <div className="eval-stat">
            <span className="muted-meta">Vacancies</span>
            <strong>{lastSummary.vacancyCount}</strong>
          </div>
          <div className="eval-stat">
            <span className="muted-meta">Evaluations</span>
            <strong>{lastSummary.evaluationCount}</strong>
          </div>
          <div className="eval-stat">
            <span className="muted-meta">Applications</span>
            <strong>{lastSummary.applicationCount}</strong>
          </div>
          <div className="eval-stat">
            <span className="muted-meta">Submission Attempts</span>
            <strong>{lastSummary.attemptCount}</strong>
          </div>
        </div>
      )}

      <dialog
        ref={dialogRef}
        className="modal-dialog"
        aria-labelledby={dialogTitleId}
        aria-describedby={dialogDescId}
        onClose={handleDialogClose}
        {...({ closedby: "any" } as Record<string, string>)}
      >
        <div className="modal-content">
          <h1 id={dialogTitleId} className="modal-title">
            Restore SQLite backup?
          </h1>
          <p id={dialogDescId} className="modal-body">
            This replaces your current local database with{" "}
            <strong>{selectedFile?.name || "the selected backup"}</strong>. RoleRelay saves a
            pre-restore safety copy alongside your database before swapping. If you moved from
            another computer, reconnect your sending account password afterward.
          </p>
          <form method="dialog" className="modal-actions">
            <button type="submit" value="cancel" className="btn btn-secondary" autoFocus>
              Cancel
            </button>
            <button type="submit" value="confirm" className="btn btn-danger">
              Restore Backup
            </button>
          </form>
        </div>
      </dialog>
    </section>
  );
}
