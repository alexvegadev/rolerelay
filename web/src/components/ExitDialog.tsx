import { useEffect, useId, useRef } from "react";

interface ExitDialogProps {
  open: boolean;
  exiting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

export function ExitDialog({ open, exiting, onCancel, onConfirm }: ExitDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descId = useId();

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    if (open && !dialog.open) {
      dialog.showModal();
    } else if (!open && dialog.open) {
      dialog.close();
    }
  }, [open]);

  // Fallback light-dismiss for browsers without native `closedby` support.
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
    if (!dialog) return;
    const result = dialog.returnValue;
    dialog.returnValue = "";
    if (result === "confirm") {
      onConfirm();
    } else {
      onCancel();
    }
  };

  return (
    <dialog
      ref={dialogRef}
      className="modal-dialog"
      aria-labelledby={titleId}
      aria-describedby={descId}
      onClose={handleClose}
      {...({ closedby: "any" } as Record<string, string>)}
    >
      <div className="modal-content">
        <h1 id={titleId} className="modal-title">
          Exit RoleRelay?
        </h1>
        <p id={descId} className="modal-body">
          This stops the local server and background worker. Closing this browser tab without exiting
          leaves RoleRelay running in the background. Any saved CVs and queued work stay safely in
          SQLite for the next launch.
        </p>
        <form method="dialog" className="modal-actions">
          <button
            type="submit"
            value="cancel"
            className="btn btn-secondary"
            autoFocus
            disabled={exiting}
          >
            Cancel
          </button>
          <button
            type="submit"
            value="confirm"
            className="btn btn-danger"
            disabled={exiting}
          >
            {exiting ? "Stopping..." : "Exit RoleRelay"}
          </button>
        </form>
      </div>
    </dialog>
  );
}
