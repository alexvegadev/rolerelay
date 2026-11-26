import { useCallback, useEffect, useRef, useState } from "react";
import { A2uiSurface } from "@a2ui/react";
import type { ReactCatalogComponent } from "@a2ui/react";
import { MessageProcessor } from "@a2ui/web_core";
import type { ActionPayload, ProcessableMessage, SurfaceModel } from "@a2ui/web_core";
import {
  ApiError,
  fetchApplicationA2UI,
  sendApplicationA2UIEvent,
} from "../api";
import { ensureA2uiStyles, roleRelayReviewCatalog } from "../a2ui/catalog";
import type { ApplicationDetail } from "../types";

interface A2uiReviewPanelProps {
  applicationId: number;
  revisionKey: string;
  onUpdated: (updated: ApplicationDetail, message: string) => void;
}

export function A2uiReviewPanel({
  applicationId,
  revisionKey,
  onUpdated,
}: A2uiReviewPanelProps) {
  const [surface, setSurface] = useState<SurfaceModel<ReactCatalogComponent> | null>(null);
  const [catalogId, setCatalogId] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);

  const processorRef = useRef<MessageProcessor<ReactCatalogComponent> | null>(null);
  const onUpdatedRef = useRef(onUpdated);
  onUpdatedRef.current = onUpdated;

  const applyMessages = useCallback(
    (messages: unknown[]) => {
      ensureA2uiStyles();
      if (processorRef.current) {
        processorRef.current.dispose();
        processorRef.current = null;
      }

      const handleAction = async (action: ActionPayload) => {
        setSubmitting(true);
        setErrorMessage(null);
        setStatusMessage(null);
        try {
          const res = await sendApplicationA2UIEvent(applicationId, {
            version: "1.0",
            action: {
              name: action.name,
              surfaceId: action.surfaceId,
              sourceComponentId: action.sourceComponentId,
              timestamp: action.timestamp || new Date().toISOString(),
              context: action.context ?? {},
            },
          });
          setStatusMessage(res.message);
          onUpdatedRef.current(res.application, res.message);
          if (res.panel?.messages) {
            applyMessages(res.panel.messages);
          }
        } catch (err) {
          setErrorMessage(
            err instanceof ApiError ? err.message : "Failed to save A2UI review answer."
          );
        } finally {
          setSubmitting(false);
        }
      };

      const processor = new MessageProcessor<ReactCatalogComponent>(
        [roleRelayReviewCatalog],
        (action) => {
          void handleAction(action);
        },
        {
          version: "v1.0",
          errorHandler: (err) => {
            setErrorMessage(err.error?.message || "A2UI surface validation error.");
          },
          reportErrorsOnly: true,
        }
      );

      processorRef.current = processor;

      let created: SurfaceModel<ReactCatalogComponent> | null = null;
      const sub = processor.onSurfaceCreated((s) => {
        created = s;
      });
      processor.processMessages(messages as ProcessableMessage[]);
      sub.unsubscribe();

      if (created) {
        setSurface(created);
      } else {
        const first = processor.getSurfaces().values().next().value ?? null;
        setSurface(first);
      }
    },
    [applicationId]
  );

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setErrorMessage(null);
    fetchApplicationA2UI(applicationId)
      .then((res) => {
        if (cancelled) return;
        setCatalogId(res.panel.catalogId);
        applyMessages(res.panel.messages);
      })
      .catch((err) => {
        if (cancelled) return;
        setErrorMessage(
          err instanceof ApiError ? err.message : "Could not load A2UI review panel."
        );
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
      if (processorRef.current) {
        processorRef.current.dispose();
        processorRef.current = null;
      }
    };
  }, [applicationId, revisionKey, applyMessages]);

  return (
    <div className="a2ui-panel-container" aria-busy={loading || submitting}>
      <div className="a2ui-panel-header">
        <span className="a2ui-panel-label">A2UI v1.0 Review Panel</span>
        {catalogId && <code className="inline-code">{catalogId}</code>}
      </div>

      {loading && <p className="muted-meta">Loading review panel...</p>}

      {errorMessage && (
        <div className="inline-alert alert-error" role="alert">
          <strong>Error:</strong> {errorMessage}
        </div>
      )}

      {statusMessage && (
        <div className="inline-alert alert-success" role="status" aria-live="polite">
          {statusMessage}
        </div>
      )}

      {!loading && surface && (
        <div className="a2ui-surface-host">
          <A2uiSurface surface={surface} />
        </div>
      )}
    </div>
  );
}
