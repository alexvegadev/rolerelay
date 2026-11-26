import { useEffect, useId, useState } from "react";
import type { FormEvent } from "react";
import type { EmailSettings } from "../types";

interface SendingSettingsPanelProps {
  settings: EmailSettings;
  saving: boolean;
  testing: boolean;
  errorMessage: string | null;
  successMessage: string | null;
  onSave: (settings: EmailSettings) => Promise<void>;
  onTest: (recipient: string) => Promise<void>;
  onDisconnect: () => Promise<void>;
}

export function SendingSettingsPanel({
  settings,
  saving,
  testing,
  errorMessage,
  successMessage,
  onSave,
  onTest,
  onDisconnect,
}: SendingSettingsPanelProps) {
  const headingId = useId();
  const hostId = useId();
  const portId = useId();
  const userId = useId();
  const passId = useId();
  const fromNameId = useId();
  const fromEmailId = useId();
  const limitId = useId();
  const autoSendId = useId();
  const pausedId = useId();
  const testRcptId = useId();

  const [host, setHost] = useState(settings.host);
  const [port, setPort] = useState(settings.port || 587);
  const [username, setUsername] = useState(settings.username);
  const [password, setPassword] = useState("");
  const [fromName, setFromName] = useState(settings.fromName);
  const [fromEmail, setFromEmail] = useState(settings.fromEmail);
  const [tlsMode, setTlsMode] = useState<"starttls" | "tls" | "none">(settings.tlsMode || "starttls");
  const [autoSendEnabled, setAutoSendEnabled] = useState(settings.autoSendEnabled);
  const [sendingPaused, setSendingPaused] = useState(settings.sendingPaused);
  const [dailySendLimit, setDailySendLimit] = useState(settings.dailySendLimit || 10);
  const [testRecipient, setTestRecipient] = useState(settings.fromEmail || settings.username || "");

  useEffect(() => {
    setHost(settings.host);
    setPort(settings.port || 587);
    setUsername(settings.username);
    setPassword("");
    setFromName(settings.fromName);
    setFromEmail(settings.fromEmail);
    setTlsMode(settings.tlsMode || "starttls");
    setAutoSendEnabled(settings.autoSendEnabled);
    setSendingPaused(settings.sendingPaused);
    setDailySendLimit(settings.dailySendLimit || 10);
    setTestRecipient((prev) => prev || settings.fromEmail || settings.username || "");
  }, [settings]);

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    await onSave({
      ...settings,
      host: host.trim(),
      port,
      username: username.trim(),
      password: password,
      fromName: fromName.trim(),
      fromEmail: fromEmail.trim(),
      tlsMode,
      autoSendEnabled,
      sendingPaused,
      dailySendLimit,
    });
    setPassword("");
  };

  return (
    <section className="card" aria-labelledby={headingId}>
      <div className="card-header">
        <div>
          <h2 id={headingId} className="card-title">
            Sending Account &amp; Automatic Dispatch
          </h2>
          <p className="card-subtitle">
            Connect an SMTP account to send qualifying email applications with attached CV PDFs.
            Passwords are stored in your OS credential store (
            <code>{settings.credentialBackend || "os-credential-store"}</code>), never in SQLite.
          </p>
        </div>
        <span
          className={`badge ${
            settings.needsReconnect
              ? "badge-danger"
              : settings.hasCredentials && settings.host
                ? "badge-ok"
                : "badge-muted"
          }`}
        >
          {settings.needsReconnect
            ? "Reconnect required"
            : settings.hasCredentials && settings.host
              ? "Connected"
              : "Not connected"}
        </span>
      </div>

      {settings.needsReconnect && (
        <div className="inline-alert alert-warn" role="alert">
          <strong>Reconnect required:</strong> Sending account settings were found in SQLite, but
          the password is not present in this machine&apos;s OS credential store. Re-enter your
          password and save to resume automatic sending.
        </div>
      )}

      <form className="profile-form" onSubmit={handleSubmit}>
        <div className="field-grid-two">
          <div className="field">
            <label htmlFor={hostId}>SMTP Host</label>
            <input
              id={hostId}
              type="text"
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="smtp.gmail.com or 127.0.0.1"
            />
          </div>

          <div className="field">
            <label htmlFor={portId}>SMTP Port</label>
            <input
              id={portId}
              type="text"
              inputMode="numeric"
              value={String(port)}
              onChange={(e) => {
                const digits = e.target.value.replace(/\D+/g, "");
                setPort(digits ? Math.min(65535, Number(digits)) : 587);
              }}
            />
          </div>
        </div>

        <fieldset className="choice-fieldset">
          <legend>Connection security (TLS)</legend>
          <div className="radio-inline-group">
            {[
              { value: "starttls" as const, label: "STARTTLS (port 587)" },
              { value: "tls" as const, label: "Implicit TLS (port 465)" },
              { value: "none" as const, label: "None (local test server)" },
            ].map((opt) => (
              <label key={opt.value} className="radio-pill">
                <input
                  type="radio"
                  name="smtpTlsMode"
                  value={opt.value}
                  checked={tlsMode === opt.value}
                  onChange={() => setTlsMode(opt.value)}
                />
                <span>{opt.label}</span>
              </label>
            ))}
          </div>
        </fieldset>

        <div className="field-grid-two">
          <div className="field">
            <label htmlFor={userId}>SMTP Username</label>
            <input
              id={userId}
              type="text"
              autoComplete="username"
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="you@example.com"
            />
          </div>

          <div className="field">
            <label htmlFor={passId}>
              SMTP Password / App Password{" "}
              {settings.hasCredentials ? "(saved in OS store — leave blank to keep)" : ""}
            </label>
            <input
              id={passId}
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={settings.hasCredentials ? "••••••••••••" : "App password"}
            />
          </div>
        </div>

        <div className="field-grid-two">
          <div className="field">
            <label htmlFor={fromNameId}>From Name</label>
            <input
              id={fromNameId}
              type="text"
              value={fromName}
              onChange={(e) => setFromName(e.target.value)}
              placeholder="Ada Lovelace"
            />
          </div>

          <div className="field">
            <label htmlFor={fromEmailId}>From Email</label>
            <input
              id={fromEmailId}
              type="email"
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
              value={fromEmail}
              onChange={(e) => setFromEmail(e.target.value)}
              placeholder="you@example.com"
            />
          </div>
        </div>

        <div className="field-grid-two">
          <div className="field">
            <label htmlFor={limitId}>
              Daily send limit (sent today: {settings.sentToday} / {dailySendLimit})
            </label>
            <input
              id={limitId}
              type="text"
              inputMode="numeric"
              value={String(dailySendLimit)}
              onChange={(e) => {
                const digits = e.target.value.replace(/\D+/g, "");
                setDailySendLimit(digits ? Math.min(500, Math.max(1, Number(digits))) : 1);
              }}
            />
          </div>

          <div className="checkbox-stack">
            <label className="checkbox-row" htmlFor={autoSendId}>
              <input
                id={autoSendId}
                type="checkbox"
                checked={autoSendEnabled}
                onChange={(e) => setAutoSendEnabled(e.target.checked)}
              />
              <span>Enable automatic sending for qualified email listings</span>
            </label>

            <label className="checkbox-row" htmlFor={pausedId}>
              <input
                id={pausedId}
                type="checkbox"
                checked={sendingPaused}
                onChange={(e) => setSendingPaused(e.target.checked)}
              />
              <span>Pause submission queue</span>
            </label>
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
            {saving ? "Saving..." : "Save sending account"}
          </button>
          {settings.hasCredentials && (
            <button
              type="button"
              className="btn btn-danger-outline"
              onClick={() => void onDisconnect()}
            >
              Disconnect credentials
            </button>
          )}
        </div>
      </form>

      <div className="test-smtp-bar">
        <div className="field test-smtp-input">
          <label htmlFor={testRcptId}>Send test email to controlled inbox</label>
          <input
            id={testRcptId}
            type="email"
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
            value={testRecipient}
            onChange={(e) => setTestRecipient(e.target.value)}
            placeholder="test-inbox@example.com"
          />
        </div>
        <button
          type="button"
          className="btn btn-secondary"
          disabled={testing || !host.trim()}
          onClick={() => void onTest(testRecipient.trim())}
        >
          {testing ? "Sending test..." : "Send test email"}
        </button>
      </div>
    </section>
  );
}
