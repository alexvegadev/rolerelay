import { useEffect, useId, useState } from "react";
import type { FormEvent } from "react";
import type { ModelSettings } from "../types";

interface ModelSettingsPanelProps {
  settings: ModelSettings;
  saving: boolean;
  errorMessage: string | null;
  successMessage: string | null;
  onSave: (settings: ModelSettings) => Promise<void>;
}

export function ModelSettingsPanel({
  settings,
  saving,
  errorMessage,
  successMessage,
  onSave,
}: ModelSettingsPanelProps) {
  const headingId = useId();
  const endpointId = useId();
  const modelId = useId();
  const keyId = useId();
  const timeoutId = useId();

  const [provider, setProvider] = useState<"local_heuristic" | "openai_compatible">(
    settings.provider
  );
  const [endpointUrl, setEndpointUrl] = useState(settings.endpointUrl);
  const [model, setModel] = useState(settings.model);
  const [apiKey, setApiKey] = useState("");
  const [timeoutSeconds, setTimeoutSeconds] = useState(settings.timeoutSeconds || 45);

  useEffect(() => {
    setProvider(settings.provider);
    setEndpointUrl(settings.endpointUrl);
    setModel(settings.model);
    setApiKey("");
    setTimeoutSeconds(settings.timeoutSeconds || 45);
  }, [settings]);

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    await onSave({
      provider,
      endpointUrl: endpointUrl.trim(),
      model: model.trim(),
      apiKey: apiKey.trim(),
      hasApiKey: settings.hasApiKey || apiKey.trim().length > 0,
      timeoutSeconds,
    });
  };

  return (
    <section className="card" aria-labelledby={headingId}>
      <div className="card-header">
        <div>
          <h2 id={headingId} className="card-title">
            AI Evaluation Endpoint
          </h2>
          <p className="card-subtitle">
            Use the built-in offline bilingual evaluator or connect any hosted/local
            OpenAI-compatible endpoint (OpenAI, Ollama, LM Studio, vLLM). Go always verifies every
            cited CV excerpt before qualifying an application.
          </p>
        </div>
      </div>

      <form className="profile-form" onSubmit={handleSubmit}>
        <fieldset className="choice-fieldset">
          <legend>Evaluation engine</legend>
          <div className="radio-grid">
            <label className="radio-card">
              <input
                type="radio"
                name="modelProvider"
                value="local_heuristic"
                checked={provider === "local_heuristic"}
                onChange={() => setProvider("local_heuristic")}
              />
              <span className="radio-card-text">
                <span className="radio-card-title">Built-in Local Bilingual Evaluator</span>
                <span className="radio-card-hint">
                  Zero external API calls; deterministic EN/ES requirement &amp; CV matching.
                </span>
              </span>
            </label>

            <label className="radio-card">
              <input
                type="radio"
                name="modelProvider"
                value="openai_compatible"
                checked={provider === "openai_compatible"}
                onChange={() => setProvider("openai_compatible")}
              />
              <span className="radio-card-text">
                <span className="radio-card-title">OpenAI-Compatible Endpoint</span>
                <span className="radio-card-hint">
                  Hosted API or local model server supporting /chat/completions JSON output.
                </span>
              </span>
            </label>
          </div>
        </fieldset>

        {provider === "openai_compatible" && (
          <>
            <div className="field-grid-two">
              <div className="field">
                <label htmlFor={endpointId}>
                  Base Endpoint URL <span aria-hidden="true">*</span>
                </label>
                <span className="field-hint">
                  e.g. http://localhost:11434/v1 or https://api.openai.com/v1
                </span>
                <input
                  id={endpointId}
                  type="url"
                  required
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  value={endpointUrl}
                  onChange={(e) => setEndpointUrl(e.target.value)}
                  placeholder="http://localhost:11434/v1"
                />
              </div>

              <div className="field">
                <label htmlFor={modelId}>
                  Model Name <span aria-hidden="true">*</span>
                </label>
                <span className="field-hint">e.g. gpt-4o-mini, llama3.1, qwen2.5</span>
                <input
                  id={modelId}
                  type="text"
                  required
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                  placeholder="gpt-4o-mini"
                />
              </div>
            </div>

            <div className="field-grid-two">
              <div className="field">
                <label htmlFor={keyId}>
                  API Key {settings.hasApiKey ? "(saved — leave blank to keep)" : "(optional for local servers)"}
                </label>
                <input
                  id={keyId}
                  type="password"
                  autoComplete="off"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder={settings.hasApiKey ? "••••••••••••" : "sk-..."}
                />
              </div>

              <div className="field">
                <label htmlFor={timeoutId}>Request timeout (seconds)</label>
                <input
                  id={timeoutId}
                  type="text"
                  inputMode="numeric"
                  value={String(timeoutSeconds)}
                  onChange={(e) => {
                    const digits = e.target.value.replace(/\D+/g, "");
                    setTimeoutSeconds(digits ? Math.min(300, Math.max(5, Number(digits))) : 45);
                  }}
                />
              </div>
            </div>
          </>
        )}

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
            {saving ? "Saving..." : "Save evaluation settings"}
          </button>
        </div>
      </form>
    </section>
  );
}
