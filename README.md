# RoleRelay

A local-first, single-user AI job evaluation and application assistant packaged as a single Go executable with an embedded React dashboard and local SQLite storage.

## Key Features

- **Single-binary Windows x64 distribution**: Dashboard, JSON API, and background submission worker ship inside one `CGO_ENABLED=0` executable. Opening RoleRelay launches the dashboard in your default browser; closing the tab leaves the worker running until you click **Exit**.
- **Portable & Installed editions**: Runs as a portable app (`data/app.sqlite` alongside `rolerelay.exe`) or as a standard per-user Windows installation (`%LOCALAPPDATA%\RoleRelay\app.sqlite`).
- **Bilingual CV vault (`EN`, `ES`, or `Both`)**: Upload text-based PDF CVs with pure-Go text extraction, version history, and clear errors for scanned/non-extractable PDFs. `Both` mode enforces having both English and Spanish CVs and automatically selects the right version (or attaches both when explicitly requested and supported).
- **Vacancy import & deduplication**: Add listings manually or sync public **Greenhouse** and **Lever** job boards. Deduplicates by `(provider, company_board, external_job_id)` and links cross-source duplicates via canonical application URLs.
- **Fit evaluation with Go-verified CV evidence**: Works out of the box with a built-in offline bilingual evaluator or any hosted/local OpenAI-compatible model endpoint (OpenAI, Ollama, LM Studio, vLLM). Go verifies that every cited excerpt exists verbatim in the selected CV before qualifying an application.
- **Reliable automatic email submission**: Connects an SMTP account with credentials stored in **Windows Credential Manager** (never in SQLite). Enforces one application per vacancy, daily send limits, pause/resume controls, pre-acceptance retries, and crash/restart recovery that keeps uncertain mid-dispatch attempts `Pending` for verification rather than duplicating sends.
- **A2UI v1.0 review panel**: Renders vacancy-dependent review controls (such as expected salary amount + currency or ambiguous CV language selection) using `@a2ui/react` and `@a2ui/web_core`. Go validates every answer, persists it, and reevaluates the application before queueing.
- **Five-state tracking & SQLite backup/restore**: Tracks `Pending`, `Sent`, `In Progress`, `Interview Pending`, and `Discarded` with full timestamped history, plus consistent online SQLite export (`VACUUM INTO`) and validated restore across installations.

## Installation (Windows x64)

### Option 1: Windows Installer

1. Run `dist\rolerelay-setup-windows-amd64.exe`.
2. RoleRelay installs to `%LOCALAPPDATA%\Programs\RoleRelay` (no admin privileges required), adds Start Menu and Desktop shortcuts, and launches the dashboard in your default browser.
3. Local data is stored in `%LOCALAPPDATA%\RoleRelay\app.sqlite`.
4. To uninstall, use Windows **Installed Apps** or run `uninstall.exe --uninstall`.

### Option 2: Portable ZIP

1. Extract `dist\rolerelay-windows-amd64-portable.zip` to any folder.
2. Double-click `rolerelay.exe`.
3. Because `portable.mode` sits next to the executable, your SQLite database is stored locally in `data\app.sqlite`.

### Option 3: Build & Run from Source

**Prerequisites**: [Go](https://go.dev/) 1.25+ and [Bun](https://bun.sh/) 1.2+.

```powershell
# Install frontend dependencies
bun install --cwd web

# Build Windows x64 executable, portable ZIP, and installer into dist/
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Version 1.0.0

# Run directly in portable mode
.\dist\rolerelay.exe --portable
```

For quick local iteration without packaging:

```powershell
bun --cwd web run build
go run ./cmd/rolerelay --portable
```

## CLI Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--addr` | `127.0.0.1:41730` | Loopback address (`host:port`) for the dashboard and API |
| `--portable` | `false` | Force portable mode (`data/app.sqlite` next to executable) |
| `--db` | *(auto)* | Use a custom SQLite database file path |
| `--no-browser` | `false` | Start server and worker without opening the default browser |
| `--version` | `false` | Print version and exit |

## Running Tests

```powershell
go vet ./...
go test ./...
bun --cwd web run typecheck
```
