param(
  [string]$ExePath = ".\dist\cybererp-print-agent.exe",
  [string]$InstallDir = "$env:ProgramFiles\CyberERP\PrintAgent",
  [string]$TaskName = "CyberERP Print Agent",
  [string]$AgentAddr = "127.0.0.1:12345",
  [string]$AllowedOrigins = "https://cyberposapp.createam.cloud,http://localhost:4200,http://localhost:4201,http://localhost:5173,http://localhost:5174,http://localhost:3000"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $ExePath)) {
  throw "Executable not found: $ExePath"
}

Write-Host "[install] Creating install directory: $InstallDir"
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

Write-Host "[install] Unblocking installer binary"
Unblock-File -Path $ExePath -ErrorAction SilentlyContinue

$targetExe = Join-Path $InstallDir "cybererp-print-agent.exe"
Copy-Item -Path $ExePath -Destination $targetExe -Force
Unblock-File -Path $targetExe -ErrorAction SilentlyContinue

Write-Host "[install] Writing runtime environment file"
$envFile = Join-Path $InstallDir "agent.env.ps1"
@(
  "$env:PRINT_AGENT_ADDR = '$AgentAddr'",
  "$env:PRINT_AGENT_ALLOWED_ORIGINS = '$AllowedOrigins'"
) | Set-Content -Path $envFile -Encoding UTF8

$taskScript = Join-Path $InstallDir "start-agent.ps1"
@(
  "$ErrorActionPreference = 'Stop'",
  ". '$envFile'",
  "Start-Process -FilePath '$targetExe' -WindowStyle Hidden"
) | Set-Content -Path $taskScript -Encoding UTF8

Write-Host "[install] Registering scheduled task for autostart"
if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
  Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}

$action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-NoProfile -ExecutionPolicy Bypass -File `"$taskScript`""
$trigger = New-ScheduledTaskTrigger -AtLogOn
$principal = New-ScheduledTaskPrincipal -UserId $env:UserName -LogonType Interactive -RunLevel Limited
Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Principal $principal | Out-Null

Write-Host "[install] Starting agent now"
Start-Process -FilePath "powershell.exe" -ArgumentList "-NoProfile -ExecutionPolicy Bypass -File `"$taskScript`"" -WindowStyle Hidden

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
  Write-Warning "Agent did not respond on $healthUrl. If Windows Defender blocked it, allow the app and run the installer again as Administrator."
}

Write-Host "[install] Done"
Write-Host "  Executable: $targetExe"
Write-Host "  Task: $TaskName"
Write-Host "  Address: $AgentAddr"
