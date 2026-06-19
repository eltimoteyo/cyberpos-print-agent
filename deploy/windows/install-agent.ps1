param(
  [string]$ExePath = ".\dist\cybererp-print-agent.exe",
  [string]$InstallDir = "$env:ProgramFiles\CyberERP\PrintAgent",
  [string]$DataDir = "$env:ProgramData\CyberERP\PrintAgent",
  [string]$ServiceName = "CyberERPPrintAgent",
  [string]$DisplayName = "CyberERP Print Agent",
  [string]$AgentAddr = "127.0.0.1:12345",
  [string]$AllowedOrigins = "https://cyberposapp.createam.cloud,http://localhost:4200,http://localhost:4201,http://localhost:5173,http://localhost:5174,http://localhost:3000",
  [string]$GatewayWSUrl = "",
  [string]$AgentToken = ""
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $ExePath)) {
  throw "Executable not found: $ExePath"
}

Write-Host "[install] Creating install directory: $InstallDir"
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

Write-Host "[install] Creating data directory: $DataDir"
New-Item -ItemType Directory -Path $DataDir -Force | Out-Null

Write-Host "[install] Unblocking installer binary"
Unblock-File -Path $ExePath -ErrorAction SilentlyContinue

$targetExe = Join-Path $InstallDir "cybererp-print-agent.exe"
Copy-Item -Path $ExePath -Destination $targetExe -Force
Unblock-File -Path $targetExe -ErrorAction SilentlyContinue

Write-Host "[install] Writing runtime environment file"
$envFile = Join-Path $InstallDir "agent.env"
$envLines = @(
  "PRINT_AGENT_ADDR=$AgentAddr",
  "PRINT_AGENT_ALLOWED_ORIGINS=$AllowedOrigins",
  "PRINT_AGENT_DATA_DIR=$DataDir"
)
if ($GatewayWSUrl -ne "") {
  $envLines += "PRINT_AGENT_GATEWAY_WS_URL=$GatewayWSUrl"
}
if ($AgentToken -ne "") {
  $envLines += "PRINT_AGENT_TOKEN=$AgentToken"
}
$envLines | Set-Content -Path $envFile -Encoding UTF8

Write-Host "[install] Stopping and removing legacy scheduled task if exists"
$legacyTaskName = "CyberERP Print Agent"
if (Get-ScheduledTask -TaskName $legacyTaskName -ErrorAction SilentlyContinue) {
  Unregister-ScheduledTask -TaskName $legacyTaskName -Confirm:$false
}

Write-Host "[install] Stopping and removing existing service if exists"
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
  sc.exe delete $ServiceName | Out-Null
  Start-Sleep -Seconds 1
}

Write-Host "[install] Creating Windows service"
$quotedExe = '"' + $targetExe + '"'
New-Service -Name $ServiceName `
  -DisplayName $DisplayName `
  -BinaryPathName $quotedExe `
  -StartupType Automatic `
  -Description "Servicio de impresión CyberERP ESC/POS (print-agent-go)" | Out-Null

Write-Host "[install] Configuring service recovery"
sc.exe failure $ServiceName reset=86400 actions=restart/5000/restart/10000/restart/30000 | Out-Null

Write-Host "[install] Starting service"
Start-Service -Name $ServiceName

Write-Host "[install] Verifying local health endpoint"
$healthUrl = "http://$AgentAddr/health"
$online = $false
for ($i = 0; $i -lt 10; $i++) {
  try {
    $resp = Invoke-RestMethod -Method Get -Uri $healthUrl -TimeoutSec 2
    if ($resp.status -eq "ok") {
      $online = $true
      break
    }
  } catch {
    Start-Sleep -Milliseconds 700
  }
}

if ($online) {
  Write-Host "[install] Agent online at $healthUrl"
} else {
  Write-Warning "Agent did not respond on $healthUrl. Check Windows Event Logs > Application for errors."
}

Write-Host "[install] Done"
Write-Host "  Executable: $targetExe"
Write-Host "  Data: $DataDir"
Write-Host "  Service: $ServiceName"
Write-Host "  Address: $AgentAddr"
