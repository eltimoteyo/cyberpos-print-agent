param(
  [string]$InstallDir = "$env:ProgramFiles\CyberERP\PrintAgent",
  [string]$TaskName = "CyberERP Print Agent"
)

$ErrorActionPreference = "Stop"

Write-Host "[uninstall] Stopping process"
Get-Process -Name "cybererp-print-agent" -ErrorAction SilentlyContinue | Stop-Process -Force

Write-Host "[uninstall] Removing autostart task if exists"
if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
  Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}

if (Test-Path $InstallDir) {
  Write-Host "[uninstall] Removing install directory: $InstallDir"
  Remove-Item -Path $InstallDir -Recurse -Force
}

Write-Host "[uninstall] Done"
