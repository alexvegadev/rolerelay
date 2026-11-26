param(
    [string]$Version = "0.1.0"
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$DistDir = Join-Path $Root "dist"
$WebDir = Join-Path $Root "web"
$EmbedDist = Join-Path $Root "internal\webui\dist"
$PayloadDir = Join-Path $Root "cmd\installer\payload"
$PortableStage = Join-Path $DistDir "portable-stage"

Write-Host "==> Building RoleRelay v$Version (Windows x64)" -ForegroundColor Cyan

# 1. Clean previous build artifacts
New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
Get-ChildItem -Path $EmbedDist -Exclude ".gitkeep" -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force
if (Test-Path $PortableStage) { Remove-Item -Recurse -Force $PortableStage }

# 2. Typecheck and build React frontend with Bun
Write-Host "==> Typechecking & bundling React frontend with Bun..." -ForegroundColor Cyan
Push-Location $WebDir
try {
    & bun run typecheck
    if ($LASTEXITCODE -ne 0) { throw "Frontend typecheck failed" }
    & bun run build
    if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
} finally {
    Pop-Location
}

# 3. Build single-binary Go executable for Windows x64 (pure Go, CGO_ENABLED=0)
Write-Host "==> Compiling rolerelay.exe (GOOS=windows GOARCH=amd64 CGO_ENABLED=0)..." -ForegroundColor Cyan
$AppExe = Join-Path $DistDir "rolerelay.exe"
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
Push-Location $Root
try {
    & go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $AppExe ./cmd/rolerelay
    if ($LASTEXITCODE -ne 0) { throw "Go build of rolerelay.exe failed" }
} finally {
    Pop-Location
}

# 4. Package Portable ZIP
Write-Host "==> Packaging portable ZIP..." -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $PortableStage | Out-Null
Copy-Item $AppExe (Join-Path $PortableStage "rolerelay.exe")
Set-Content -Path (Join-Path $PortableStage "portable.mode") -Value "portable" -Encoding UTF8
@"
RoleRelay v$Version — Portable Edition (Windows x64)
====================================================

1. Extract this entire ZIP into any folder.
2. Double-click rolerelay.exe to start RoleRelay and open the dashboard in your default browser.
3. Because 'portable.mode' is present alongside rolerelay.exe, your SQLite database is stored in:
     data\app.sqlite
4. Closing the browser tab leaves the local background worker running.
   Use the Exit button in the dashboard to stop RoleRelay cleanly.
"@ | Set-Content -Path (Join-Path $PortableStage "README.txt") -Encoding UTF8

$PortableZip = Join-Path $DistDir "rolerelay-windows-amd64-portable.zip"
if (Test-Path $PortableZip) { Remove-Item -Force $PortableZip }
Compress-Archive -Path (Join-Path $PortableStage "*") -DestinationPath $PortableZip -CompressionLevel Optimal
Remove-Item -Recurse -Force $PortableStage

# 5. Build self-contained Windows x64 installer
Write-Host "==> Building Windows x64 installer (rolerelay-setup-windows-amd64.exe)..." -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $PayloadDir | Out-Null
$PayloadExe = Join-Path $PayloadDir "rolerelay.exe"
Copy-Item $AppExe $PayloadExe
$InstallerExe = Join-Path $DistDir "rolerelay-setup-windows-amd64.exe"
Push-Location $Root
try {
    & go build -tags embed_payload -trimpath -ldflags "-s -w -X main.version=$Version" -o $InstallerExe ./cmd/installer
    if ($LASTEXITCODE -ne 0) { throw "Go build of installer failed" }
} finally {
    Pop-Location
    Remove-Item -Force $PayloadExe -ErrorAction SilentlyContinue
}

# 6. Summary & checksums
Write-Host ""
Write-Host "==> Release artifacts built successfully:" -ForegroundColor Green
foreach ($artifact in @($AppExe, $PortableZip, $InstallerExe)) {
    $hash = (Get-FileHash -Algorithm SHA256 $artifact).Hash.ToLowerInvariant()
    $sizeMB = [Math]::Round((Get-Item $artifact).Length / 1MB, 2)
    Write-Host ("  {0,-42} {1,6} MB  sha256:{2}" -f (Split-Path -Leaf $artifact), $sizeMB, $hash.Substring(0, 16))
}
