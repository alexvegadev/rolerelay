import { useEffect, useId, useRef, useState } from "react";
import type { FormEvent } from "react";
import type { LanguageMode, Profile, ProfileUpdatePayload, WorkArrangement } from "../types";

interface ProfileFormProps {
  profile: Profile;
  saving: boolean;
  errorMessage: string | null;
  successMessage: string | null;
  onSave: (payload: ProfileUpdatePayload) => Promise<void>;
}

const WORK_OPTIONS: { value: WorkArrangement; label: string; hint: string }[] = [
  { value: "remote", label: "Remote", hint: "Fully remote roles" },
  { value: "hybrid", label: "Hybrid", hint: "Mixed remote and office" },
  { value: "onsite", label: "On-site", hint: "In-office roles" },
  { value: "any", label: "Any", hint: "No arrangement restriction" },
];

const LANGUAGE_OPTIONS: { value: LanguageMode; label: string; hint: string }[] = [
  { value: "en", label: "English only", hint: "Requires 1 English PDF CV" },
  { value: "es", label: "Spanish only", hint: "Requires 1 Spanish PDF CV" },
  { value: "both", label: "Both (English & Spanish)", hint: "Requires 2 separate PDF CVs" },
];

export function ProfileForm({
  profile,
  saving,
  errorMessage,
  successMessage,
  onSave,
}: ProfileFormProps) {
  const formRef = useRef<HTMLFormElement>(null);
  const headingId = useId();
  const fullNameId = useId();
  const emailId = useId();
  const emailHintId = useId();
  const emailErrId = useId();
  const rolesId = useId();
  const rolesHintId = useId();
  const rolesErrId = useId();
  const locationId = useId();
  const thresholdId = useId();
  const limitId = useId();

  const [fullName, setFullName] = useState(profile.fullName);
  const [email, setEmail] = useState(profile.email);
  const [rolesText, setRolesText] = useState(profile.targetRoles.join(", "));
  const [location, setLocation] = useState(profile.location);
  const [workArrangement, setWorkArrangement] = useState<WorkArrangement>(profile.workArrangement);
  const [languageMode, setLanguageMode] = useState<LanguageMode>(profile.languageMode);
  const [fitThreshold, setFitThreshold] = useState(profile.fitThreshold);
  const [dailySendLimit, setDailySendLimit] = useState(profile.dailySendLimit);

  const [touched, setTouched] = useState<{ email: boolean; roles: boolean }>({
    email: false,
    roles: false,
  });
  const [submittedOnce, setSubmittedOnce] = useState(false);

  useEffect(() => {
    setFullName(profile.fullName);
    setEmail(profile.email);
    setRolesText(profile.targetRoles.join(", "));
    setLocation(profile.location);
    setWorkArrangement(profile.workArrangement);
    setLanguageMode(profile.languageMode);
    setFitThreshold(profile.fitThreshold);
    setDailySendLimit(profile.dailySendLimit);
  }, [profile]);

  const parsedRoles = rolesText
    .split(/[,\n]+/)
    .map((s) => s.trim())
    .filter(Boolean);

  const emailInvalid =
    (touched.email || submittedOnce) &&
    email.trim().length > 0 &&
    !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());

  const rolesInvalid = (touched.roles || submittedOnce) && parsedRoles.length === 0;

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setSubmittedOnce(true);

    const isEmailBad =
      email.trim().length > 0 && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());
    const isRolesBad = parsedRoles.length === 0;

    if (isEmailBad) {
      formRef.current?.querySelector<HTMLInputElement>(`#${CSS.escape(emailId)}`)?.focus();
      return;
    }
    if (isRolesBad) {
      formRef.current?.querySelector<HTMLTextAreaElement>(`#${CSS.escape(rolesId)}`)?.focus();
      return;
    }

    await onSave({
      fullName: fullName.trim(),
      email: email.trim(),
      targetRoles: parsedRoles,
      location: location.trim(),
      workArrangement,
      languageMode,
      fitThreshold,
      dailySendLimit,
    });
  };

  return (
    <section className="card" aria-labelledby={headingId}>
      <div className="card-header">
        <div>
          <h2 id={headingId} className="card-title">
            Profile &amp; Preferences
          </h2>
          <p className="card-subtitle">
            Define target roles, location, work arrangement, and application language mode.
          </p>
        </div>
      </div>

      <form
        ref={formRef}
        className="profile-form"
        method="POST"
        action="/api/profile"
        aria-labelledby={headingId}
        noValidate
        onSubmit={handleSubmit}
      >
        <div className="field-grid-two">
          <div className="field">
            <label htmlFor={fullNameId}>Full name</label>
            <input
              id={fullNameId}
              name="fullName"
              type="text"
              autoComplete="name"
              value={fullName}
              onChange={(e) => setFullName(e.target.value)}
              placeholder="e.g. Ana Martinez"
            />
          </div>

          <div className="field">
            <label htmlFor={emailId}>Contact email</label>
            <span id={emailHintId} className="field-hint">
              Format: you@example.com
            </span>
            <input
              id={emailId}
              name="email"
              type="email"
              autoComplete="email"
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              onBlur={() => setTouched((prev) => ({ ...prev, email: true }))}
              aria-invalid={emailInvalid ? "true" : "false"}
              aria-describedby={emailInvalid ? `${emailHintId} ${emailErrId}` : emailHintId}
              className={emailInvalid ? "user-invalid-fallback" : undefined}
              placeholder="you@example.com"
            />
            {emailInvalid && (
              <div id={emailErrId} className="field-error">
                <span className="error-prefix">Error:</span> Enter an email address in the format
                you@example.com.
              </div>
            )}
          </div>
        </div>

        <div className="field">
          <label htmlFor={rolesId}>
            Target roles <span aria-hidden="true">*</span>
          </label>
          <span id={rolesHintId} className="field-hint">
            Separate multiple roles with commas or new lines (e.g. Backend Engineer, Go Developer).
          </span>
          <textarea
            id={rolesId}
            name="targetRoles"
            rows={2}
            required
            value={rolesText}
            onChange={(e) => setRolesText(e.target.value)}
            onBlur={() => setTouched((prev) => ({ ...prev, roles: true }))}
            aria-invalid={rolesInvalid ? "true" : "false"}
            aria-describedby={rolesInvalid ? `${rolesHintId} ${rolesErrId}` : rolesHintId}
            className={rolesInvalid ? "user-invalid-fallback" : undefined}
            placeholder="Backend Engineer, Full-Stack Engineer"
          />
          {rolesInvalid && (
            <div id={rolesErrId} className="field-error">
              <span className="error-prefix">Error:</span> Enter at least one target role.
            </div>
          )}
        </div>

        <div className="field">
          <label htmlFor={locationId}>Preferred location</label>
          <input
            id={locationId}
            name="location"
            type="text"
            value={location}
            onChange={(e) => setLocation(e.target.value)}
            placeholder="e.g. Buenos Aires, Madrid, or US/LATAM Remote"
          />
        </div>

        <fieldset className="choice-fieldset">
          <legend>Work arrangement</legend>
          <div className="radio-grid">
            {WORK_OPTIONS.map((opt) => (
              <label key={opt.value} className="radio-card">
                <input
                  type="radio"
                  name="workArrangement"
                  value={opt.value}
                  checked={workArrangement === opt.value}
                  onChange={() => setWorkArrangement(opt.value)}
                />
                <span className="radio-card-text">
                  <span className="radio-card-title">{opt.label}</span>
                  <span className="radio-card-hint">{opt.hint}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>

        <fieldset className="choice-fieldset">
          <legend>Application language mode</legend>
          <div className="radio-grid radio-grid-three">
            {LANGUAGE_OPTIONS.map((opt) => (
              <label key={opt.value} className="radio-card">
                <input
                  type="radio"
                  name="languageMode"
                  value={opt.value}
                  checked={languageMode === opt.value}
                  onChange={() => setLanguageMode(opt.value)}
                />
                <span className="radio-card-text">
                  <span className="radio-card-title">{opt.label}</span>
                  <span className="radio-card-hint">{opt.hint}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>

        <div className="field-grid-two">
          <div className="field">
            <label htmlFor={thresholdId}>Minimum fit threshold ({fitThreshold}%)</label>
            <input
              id={thresholdId}
              name="fitThreshold"
              type="range"
              min={0}
              max={100}
              step={5}
              value={fitThreshold}
              onChange={(e) => setFitThreshold(Number(e.target.value))}
            />
          </div>

          <div className="field">
            <label htmlFor={limitId}>Daily automatic send limit</label>
            <input
              id={limitId}
              name="dailySendLimit"
              type="text"
              inputMode="numeric"
              pattern="[0-9]+"
              value={String(dailySendLimit)}
              onChange={(e) => {
                const digits = e.target.value.replace(/\D+/g, "");
                const num = digits ? Math.min(500, Math.max(1, Number(digits))) : 1;
                setDailySendLimit(num);
              }}
            />
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

        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={saving}>
            {saving ? "Saving profile..." : "Save profile"}
          </button>
        </div>
      </form>
    </section>
  );
}
