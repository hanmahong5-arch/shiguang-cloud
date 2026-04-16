# build.ps1 — One-click build for the full ShiguangSuite.
# Produces four Windows binaries in tools/ShiguangSuite/release/:
#   shiguang-gate.exe       — TCP gateway (run once per service line)
#   shiguang-control.exe    — HTTP/WSS control center + admin SPA
#   shiguang-launcher.exe   — Wails-based player launcher
#
# Usage:
#   cd tools/ShiguangSuite
#   powershell -ExecutionPolicy Bypass -File build.ps1

$ErrorActionPreference = "Stop"

$Root = $PSScriptRoot
$Release = Join-Path $Root "release"
New-Item -ItemType Directory -Path $Release -Force | Out-Null

Write-Host "===========================================" -ForegroundColor Cyan
Write-Host "ShiguangSuite one-click build" -ForegroundColor Cyan
Write-Host "===========================================" -ForegroundColor Cyan

# ---- shared/ workspace tests ----
Write-Host "`n[1/5] Running shared/ tests..." -ForegroundColor Yellow
Push-Location (Join-Path $Root "shared")
go test ./crypto/...
if ($LASTEXITCODE -ne 0) { throw "shared/ tests failed" }
Pop-Location

# ---- shiguang-gate ----
Write-Host "`n[2/5] Building shiguang-gate..." -ForegroundColor Yellow
Push-Location (Join-Path $Root "shiguang-gate")
go test ./...
if ($LASTEXITCODE -ne 0) { throw "gate tests failed" }
go build -ldflags "-s -w" -o (Join-Path $Release "shiguang-gate.exe") ./cmd/gate
if ($LASTEXITCODE -ne 0) { throw "gate build failed" }
Copy-Item "configs/gate-58.yaml" (Join-Path $Release "gate-58.yaml") -Force
Copy-Item "configs/gate-48.yaml" (Join-Path $Release "gate-48.yaml") -Force
Pop-Location

# ---- shiguang-control ----
Write-Host "`n[3/5] Building shiguang-control..." -ForegroundColor Yellow
Push-Location (Join-Path $Root "shiguang-control")
go test ./...
if ($LASTEXITCODE -ne 0) { throw "control tests failed" }
go build -ldflags "-s -w" -o (Join-Path $Release "shiguang-control.exe") ./cmd/control
if ($LASTEXITCODE -ne 0) { throw "control build failed" }
Copy-Item "configs/control.yaml" (Join-Path $Release "control.yaml") -Force
# Copy the static admin SPA alongside the binary
$ControlWeb = Join-Path $Release "web/dist"
New-Item -ItemType Directory -Path $ControlWeb -Force | Out-Null
Copy-Item "web/dist/*" $ControlWeb -Recurse -Force
Pop-Location

# ---- shiguang-launcher ----
Write-Host "`n[4/5] Building shiguang-launcher (Wails)..." -ForegroundColor Yellow
Push-Location (Join-Path $Root "shiguang-launcher")
# Go-only tests for the launcher's internal packages
go test ./internal/...
if ($LASTEXITCODE -ne 0) { throw "launcher tests failed" }
wails build -clean
if ($LASTEXITCODE -ne 0) { throw "wails build failed" }
Copy-Item "build/bin/shiguang-launcher.exe" (Join-Path $Release "shiguang-launcher.exe") -Force
Pop-Location

# ---- summary ----
Write-Host "`n[5/5] Build complete" -ForegroundColor Green
Write-Host "`nArtifacts in release/:" -ForegroundColor Cyan
Get-ChildItem $Release -Recurse | ForEach-Object {
    if (-not $_.PSIsContainer) {
        $size = "{0:N0} bytes" -f $_.Length
        Write-Host ("  {0}  ({1})" -f $_.FullName.Substring($Release.Length + 1), $size)
    }
}

Write-Host "`nDeployment:" -ForegroundColor Cyan
Write-Host "  1. Copy release/ to the production server"
Write-Host "  2. Edit release/control.yaml — set jwt.secret and launcher.public_gate_ip"
Write-Host "  3. Edit release/gate-58.yaml and gate-48.yaml if needed"
Write-Host "  4. Start: ./shiguang-gate.exe -config gate-58.yaml (in its own terminal/service)"
Write-Host "  5. Start: ./shiguang-gate.exe -config gate-48.yaml (in its own terminal/service)"
Write-Host "  6. Start: SHIGUANG_ADMIN_PASS=... ./shiguang-control.exe -config control.yaml"
Write-Host "  7. Distribute release/shiguang-launcher.exe to players"
