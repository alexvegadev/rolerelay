import { useEffect, useId, useRef } from "react";
import { cvFileUrl } from "../api";
import type { CVDocument } from "../types";

interface CvPreviewDialogProps {
  cv: CVDocument | null;
  onClose: () => void;
}

export function CvPreviewDialog({ cv, onClose }: CvPreviewDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (cv && !dialog.open) {
      dialog.showModal();
    } else if (!cv && dialog.open) {
      dialog.close();
    }
  }, [cv]);

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

  const handleClose = () => {
    const dialog = dialogRef.current;
    if (dialog) {
      dialog.returnValue = "";
    }
    onClose();
  };

  return (
    <dialog
      ref={dialogRef}
      className="modal-dialog modal-dialog-wide"
      aria-labelledby={titleId}
      onClose={handleClose}
      {...({ closedby: "any" } as Record<string, string>)}
    >
      {cv && (
        <div className="modal-content">
          <div className="modal-header-row">
            <div>
              <h1 id={titleId} className="modal-title">
                {cv.filename}
              </h1>
              <p className="muted-meta">
                {cv.language === "en" ? "English" : "Spanish"} · v{cv.version} · {cv.pageCount}{" "}
                {cv.pageCount === 1 ? "page" : "pages"} · {cv.extractedChars.toLocaleString()} extracted
                characters
              </p>
            </div>
            <div className="modal-header-links">
              <a
                href={cvFileUrl(cv.id)}
                target="_blank"
                rel="noopener noreferrer"
                className="btn btn-secondary btn-sm"
              >
                Open PDF
              </a>
              <a href={cvFileUrl(cv.id, true)} className="btn btn-secondary btn-sm">
                Download
              </a>
            </div>
          </div>

          <div className="extracted-text-box" tabIndex={0} aria-label="Extracted CV text">
            <pre>{cv.extractedText}</pre>
          </div>

          <form method="dialog" className="modal-actions">
            <button type="submit" value="close" className="btn btn-primary" autoFocus>
              Close
            </button>
          </form>
        </div>
      )}
    </dialog>
  );
}
