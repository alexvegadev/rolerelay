# Portable AI Job Application App

## Project goal

Build a single-user app that evaluates job openings against the user's experience and automatically submits qualified applications through supported channels. The app supports English, Spanish, or both and stores CVs, vacancies, evaluations, application history, and settings in local SQLite.

The first release targets Windows x64 with an installer and a portable ZIP. Opening the app launches its dashboard in the default browser. The dashboard, API, and background worker ship in one Go executable.

## MVP workflow

1. **Set up the profile.** Configure target roles, location, work arrangement, and the language of applications. English or Spanish mode requires a PDF CV in the chosen language. Both mode requires two separate PDFs, one in each language. Show a clear error when a PDF has no extractable text.
2. **Import vacancies.** Accept a manually entered listing and import from configured company job boards. Start with Greenhouse, then add Lever. Deduplicate by provider, company board, and external job ID. Use a canonical application URL to link duplicates across sources when available.
3. **Select the CV.** Use the language explicitly requested by the vacancy. When the vacancy gives no instruction, use the listing's language if it is clear. Send ambiguous cases to the review panel and record a language mismatch when the vacancy requires a language outside the configured mode. In Both mode, select the appropriate CV for a single application. Attach both versions when the vacancy explicitly requests both and the channel supports it.
4. **Evaluate fit.** Ask the model for structured results that distinguish mandatory requirements from preferences and cite supporting CV excerpts. Go checks that the cited evidence exists in the selected CV and applies the configured filters and fit threshold. Missing evidence for a mandatory requirement or missing form answers leave the application Pending. A confirmed mismatch can mark it Discarded with a reason.
5. **Submit and track.** Start with email for listings that explicitly provide an application address. The user connects a sending account and enables automatic sending. For listings that require an unsupported form, keep the application Pending with a link to the official form and let the user record submission after completing it. Greenhouse and Lever provide listing APIs and require organization credentials for direct API submission. [Greenhouse Job Board API](https://docs.greenhouse.io/job-board.html#authentication) · [Lever Postings API](https://github.com/lever/postings-api#apply-to-a-job-posting)

## Architecture and distribution

| Layer | Implementation |
| --- | --- |
| Frontend | React and TypeScript for profile setup, vacancy review, and the application dashboard. Bun runs development tooling and builds the frontend assets. |
| Backend | Go serves the bundled frontend and JSON API on loopback. It owns CV extraction, connectors, eligibility rules, application state, and the submission worker. |
| Storage | SQLite through `database/sql` and `modernc.org/sqlite`. Versioned migrations cover the profile, CV documents, vacancies, evaluations, applications, attached CV versions, submission attempts, and state changes. |
| AI evaluation | A Go interface accepts the vacancy and relevant CV text and returns typed requirement findings and evidence. The MVP integrates one model endpoint. It can be hosted or local depending on the deployment's privacy and hardware needs. |

Use `go:embed` to include the built frontend in the executable. The installed edition stores SQLite in the user's app data directory. The portable edition stores it in `data/app.sqlite` alongside the executable. Add export and restore using a consistent SQLite backup. Keep credentials in the OS credential store and reconnect the sending account after a move to another machine. [Bun static bundling](https://bun.com/docs/bundler/html-static) · [Go embed](https://pkg.go.dev/embed) · [Go SQLite driver](https://pkg.go.dev/modernc.org/sqlite) · [SQLite backup](https://www.sqlite.org/backup.html)

The worker runs while the Go process is active and the machine is awake. Closing the browser tab leaves the worker running. The dashboard's Exit control stops the process, and queued work resumes on the next launch.

## A2UI v1.0 review panel

Use A2UI in the Pending application detail view, where the question depends on the vacancy. A first example is a required salary field that is missing from the profile. Go sends a panel with amount and currency controls. When the user saves the answer, Go validates it, records it, and reevaluates whether the application can enter the submission queue.

- Build the panel with the official `@a2ui/react` renderer and `@a2ui/web_core`. Bundle both with the React frontend. Start with local HTTP requests for panel messages and user events. [A2UI SDKs](https://a2ui.org/concepts/sdks/#renderer-sdk) · [A2UI protocol and transports](https://a2ui.org/specification/v1.0-a2ui/#transport-interaction-patterns)
- Define a small catalog for requirement evidence, missing-answer fields, and CV language selection. Generate the first panels in Go from validated evaluation data. Keep submission and state changes behind Go handlers that enforce the application's rules. [A2UI catalogs](https://a2ui.org/concepts/catalogs/)
- Compare two review cases, such as salary and CV language. Keep A2UI in the MVP if one catalog supports both cases and the packaged app handles them end to end. Use a fixed React form if the review fields settle into a predictable shape.

## Application states

| State | Meaning |
| --- | --- |
| Pending | Awaiting evaluation, an answer, a supported submission path, a queued send, or verification of an uncertain attempt. |
| Sent | The delivery channel accepted the submission or the user confirmed a manual submission. |
| In Progress | The employer confirmed active review or requested another step. |
| Interview Pending | An interview invitation needs a response or an interview is scheduled. |
| Discarded | The role was unsuitable, the user withdrew, or the employer rejected the application. Record the reason. |

Store a timestamp, reason, and source for each state change. In the MVP, users record employer updates from the dashboard.

## Submission reliability

- Enforce one application per profile and vacancy in SQLite. Persist the intended recipient, message, and exact CV versions with the queued submission.
- Claim queue work and record each attempt in SQLite. Recheck eligibility and sending preferences before dispatch. Provide a pause control and daily send limit.
- Retry failures that the channel confirms occurred before acceptance. If an interruption leaves delivery uncertain, keep the application Pending for verification. Resume a send automatically only when the channel supports safe replay or the app can verify the previous attempt did not send.

## Implementation milestones

| Milestone | Deliverable and acceptance check |
| --- | --- |
| 1. Portable foundation | Build the installer and ZIP. On a clean Windows x64 machine without development tools, open the dashboard, upload a CV, restart the app, and reopen the saved CV. |
| 2. Bilingual evaluation | Import vacancies in English and Spanish. Verify that Both mode requires both CVs, the correct CV is selected, and insufficient evidence for a mandatory requirement produces a reviewable explanation. |
| 3. Automatic submission | Send to a controlled inbox. Confirm that a qualifying vacancy sends one application with the correct attachment. Test a restart during dispatch and verify that an uncertain send stays Pending. |
| 4. A2UI review | In the packaged build, complete two different Pending cases, persist the answers, and verify that Go reevaluates each case before it can be queued. |
| 5. Release validation | Exercise all five states, export a backup, and restore it on a second installation. Verify that both CV versions, evaluation evidence, and submission history are recoverable. |

After the Windows release, package and verify macOS and Linux builds. Add employer-status synchronization or direct submission integrations when a supported channel is available.
