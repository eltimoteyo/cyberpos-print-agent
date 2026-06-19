param(
  [string]$ApiBaseUrl = "https://api.createam.cloud/api/v1",
  [string]$InstallDir = "$env:ProgramFiles\CyberERP\PrintAgent",
  [string]$ServiceName = "CyberERPPrintAgent"
)

$ErrorActionPreference = "Stop"

$targetExe = Join-Path $InstallDir "cybererp-print-agent.exe"
if (-not (Test-Path $targetExe)) {
  throw "Installed executable not found: $targetExe"
}

$manifestUrl = "$ApiBaseUrl/public/print-agent/latest"
Write-Host "[update] Fetching manifest: $manifestUrl"
$manifest = Invoke-RestMethod -Method Get -Uri $manifestUrl

if (-not $manifest.download_url) {
  throw "Manifest does not include download_url"
}

$tmpExe = Join-Path $env:TEMP "cybererp-print-agent.update.exe"
Write-Host "[update] Downloading: $($manifest.download_url)"
Invoke-WebRequest -Uri $manifest.download_url -OutFile $tmpExe

if ($manifest.sha256) {
  Write-Host "[update] Verifying SHA256"
  $hash = (Get-FileHash -Path $tmpExe -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($hash -ne $manifest.sha256.ToLowerInvariant()) {
    Remove-Item $tmpExe -Force -ErrorAction SilentlyContinue
    throw "SHA256 mismatch. Expected $($manifest.sha256), got $hash"
  }
}

Write-Host "[update] Stopping service"
$serviceExists = $false
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  $serviceExists = $true
  Stop-Service -Name $ServiceName -Force
}

if (-not $serviceExists) {
  Write-Host "[update] No service found; stopping process instead"
  Get-Process -Name "cybererp-print-agent" -ErrorAction SilentlyContinue | Stop-Process -Force
}

Write-Host "[update] Replacing executable"
Copy-Item -Path $tmpExe -Destination $targetExe -Force
Remove-Item $tmpExe -Force -ErrorAction SilentlyContinue

Write-Host "[update] Starting service"
if ($serviceExists) {
  Start-Service -Name $ServiceName
} else {
  Start-Process -FilePath $targetExe -WindowStyle Hidden
}

Write-Host "[update] Done"
Write-Host "  Version: $($manifest.version)"
Write-Host "  Executable: $targetExe"
