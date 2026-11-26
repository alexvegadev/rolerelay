import { createComponentImplementation } from "@a2ui/react";
import type { ReactCatalogComponent } from "@a2ui/react";
import { Catalog } from "@a2ui/web_core";
import {
  A2uiButton,
  A2uiCard,
  A2uiCheckBox,
  A2uiChoicePicker,
  A2uiColumn,
  A2uiDivider,
  A2uiRow,
  A2uiText,
  A2uiTextField,
  BASIC_FUNCTIONS,
  injectBasicCatalogStyles,
} from "@a2ui/web_core/catalogs/basic/v1";
import { z } from "zod";

export const ROLERELAY_A2UI_CATALOG_ID =
  "https://rolerelay.local/a2ui/v1/review-catalog.json";

let stylesInjected = false;
export function ensureA2uiStyles(): void {
  if (stylesInjected || typeof document === "undefined") return;
  injectBasicCatalogStyles();
  stylesInjected = true;
}

const MissingAnswerBannerApi = {
  name: "MissingAnswerBanner",
  schema: z
    .object({
      title: z.string(),
      description: z.string(),
      cases: z.array(z.string()).optional(),
    })
    .strict(),
};

const MissingAnswerBannerComponent = createComponentImplementation(
  MissingAnswerBannerApi,
  ({ props }) => {
    const cases = Array.isArray(props.cases) ? props.cases : [];
    return (
      <div className="a2ui-review-banner">
        <div className="a2ui-review-banner-head">
          <strong className="a2ui-review-banner-title">{props.title}</strong>
          {cases.length > 0 && (
            <div className="a2ui-case-tags">
              {cases.map((c) => (
                <span key={c} className="badge badge-lang">
                  A2UI case: {c}
                </span>
              ))}
            </div>
          )}
        </div>
        <p className="a2ui-review-banner-desc">{props.description}</p>
      </div>
    );
  }
);

const RequirementEvidenceCardApi = {
  name: "RequirementEvidenceCard",
  schema: z
    .object({
      requirement: z.string(),
      kind: z.string(),
      met: z.boolean(),
      evidenceVerified: z.boolean(),
      evidenceExcerpt: z.string().optional(),
      reason: z.string().optional(),
    })
    .strict(),
};

const RequirementEvidenceCardComponent = createComponentImplementation(
  RequirementEvidenceCardApi,
  ({ props }) => {
    const verified = Boolean(props.met && props.evidenceVerified);
    const isMandatory = props.kind === "mandatory";
    return (
      <div className="a2ui-evidence-card">
        <div className="a2ui-evidence-head">
          <span className="a2ui-evidence-req">{props.requirement}</span>
          <div className="a2ui-evidence-badges">
            <span className={`badge ${isMandatory ? "badge-lang" : "badge-muted"}`}>
              {isMandatory ? "Mandatory" : "Preference"}
            </span>
            <span
              className={`badge ${
                verified ? "badge-ok" : isMandatory ? "badge-warn" : "badge-muted"
              }`}
            >
              {verified ? "Verified in CV" : "Missing / Unverified"}
            </span>
          </div>
        </div>
        {props.evidenceExcerpt && (
          <blockquote className="cv-quote">&ldquo;{props.evidenceExcerpt}&rdquo;</blockquote>
        )}
        {props.reason && <p className="muted-meta">{props.reason}</p>}
      </div>
    );
  }
);

export const roleRelayReviewCatalog = new Catalog<ReactCatalogComponent>(
  ROLERELAY_A2UI_CATALOG_ID,
  "1.0",
  [
    A2uiColumn,
    A2uiRow,
    A2uiCard,
    A2uiText,
    A2uiDivider,
    A2uiTextField,
    A2uiCheckBox,
    A2uiChoicePicker,
    A2uiButton,
    MissingAnswerBannerComponent,
    RequirementEvidenceCardComponent,
  ],
  BASIC_FUNCTIONS
);
