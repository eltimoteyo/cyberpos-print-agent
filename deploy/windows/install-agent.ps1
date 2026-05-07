param(
  [string]$ExePath = ".\dist\cybererp-print-agent.exe",
  [string]$InstallDir = "$env:ProgramFiles\CyberERP\PrintAgent",
  [string]$TaskName = "CyberERP Print Agent",
  [string]$AgentAddr = "127.0.0.1:12345",
  [string]$AllowedOrigins = "http://localhost:4200"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $ExePath)) {
  throw "Executable not found: $ExePath"
}

Write-Host "[install] Creating install directory: $InstallDir"
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

$targetExe = Join-Path $InstallDir "cybererp-print-agent.exe"
Copy-Item -Path $ExePath -Destination $targetExe -Force

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

Write-Host "[install] Done"
Write-Host "  Executable: $targetExe"
Write-Host "  Task: $TaskName"
Write-Host "  Address: $AgentAddr"
