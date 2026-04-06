# Mirrors Makefile target: build-full (build-ui + embed UI + go build).
# From repo root (no GNU make required on Windows):
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build-full.ps1
# Or: pwsh -File scripts/build-full.ps1
$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

try {
    $version = (& git describe --tags --always --dirty 2>$null)
    if (-not $version) { $version = 'dev' }
} catch {
    $version = 'dev'
}

Write-Host 'Building UI frontend...'
Push-Location (Join-Path $Root 'ui')
try {
    npm install
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    npm run build
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Pop-Location
}

Write-Host 'Copying UI to gateway/ui_dist...'
$dist = Join-Path $Root 'ui/dist'
$target = Join-Path $Root 'gateway/ui_dist'
if (-not (Test-Path $dist)) {
    Write-Error "Missing $dist — UI build did not produce dist."
}
Remove-Item -Recurse -Force $target -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path (Split-Path $target -Parent) | Out-Null
Copy-Item -Recurse -Force $dist $target

$goCache = Join-Path $Root '.gocache'
New-Item -ItemType Directory -Force -Path $goCache | Out-Null
$env:GOCACHE = $goCache

Write-Host 'Building goclaw with embedded UI...'
$ldflags = "-X 'main.Version=$version'"
go build -buildvcs=false -ldflags $ldflags -o goclaw .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "Build complete! Binary: $(Join-Path $Root 'goclaw')"
