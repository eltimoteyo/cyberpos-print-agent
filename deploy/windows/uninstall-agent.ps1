param(
  [string]$InstallDir = "$env:ProgramFiles\CyberERP\PrintAgent",
  [string]$DataDir = "$env:ProgramData\CyberERP\PrintAgent",
  [string]$ServiceName = "CyberERPPrintAgent",
  [string]$TaskName = "CyberERP Print Agent"
)

$ErrorActionPreference = "Stop"

Write-Host "[uninstall] Stopping service if exists"
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
  sc.exe delete $ServiceName | Out-Null
  Start-Sleep -Seconds 1
}

Write-Host "[uninstall] Stopping process"
Get-Process -Name "cybererp-print-agent" -ErrorAction SilentlyContinue | Stop-Process -Force

Write-Host "[uninstall] Removing legacy scheduled task if exists"
if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
  Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}

if (Test-Path $InstallDir) {
  Write-Host "[uninstall] Removing install directory: $InstallDir"
  Remove-Item -Path $InstallDir -Recurse -Force
}

if (Test-Path $DataDir) {
  Write-Host "[uninstall] Removing data directory: $DataDir"
  Remove-Item -Path $DataDir -Recurse -Force
}

Write-Host "[uninstall] Done"
