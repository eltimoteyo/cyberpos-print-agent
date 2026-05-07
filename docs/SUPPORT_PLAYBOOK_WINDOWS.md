# Print Agent Windows Support Playbook

## Scope
Operational guide to install, update, verify and uninstall CyberERP Print Agent on Windows endpoints.

## Prerequisites
- Windows 10/11 with local admin access.
- Thermal printer installed in Windows.
- PowerShell execution policy allowing script run in deployment shell.

## Installation
1. Build or download `cybererp-print-agent.exe`.
2. Run:
```powershell
Set-Location "print-agent-go"
.\deploy\windows\install-agent.ps1 -ExePath ".\dist\cybererp-print-agent.exe"
```
3. Verify scheduled task exists:
```powershell
Get-ScheduledTask -TaskName "CyberERP Print Agent"
```

## Health checks
```powershell
Invoke-RestMethod -Uri "http://127.0.0.1:12345/health"
Invoke-RestMethod -Uri "http://127.0.0.1:12345/printers"
Invoke-RestMethod -Uri "http://127.0.0.1:12345/status"
```

## Update process
```powershell
Set-Location "print-agent-go"
.\deploy\windows\update-agent.ps1 -ApiBaseUrl "https://api.createam.cloud/api/v1"
```

## Uninstall process
```powershell
Set-Location "print-agent-go"
.\deploy\windows\uninstall-agent.ps1
```

## Troubleshooting
- Agent not reachable on localhost:
  - Check running process: `Get-Process cybererp-print-agent`.
  - Check startup task status: `Get-ScheduledTask -TaskName "CyberERP Print Agent"`.
- No printers listed:
  - Validate printer in Windows control panel.
  - Reinstall vendor driver.
- Ticket fails but API sale succeeds:
  - Confirm print job status in gateway `GET /api/v1/print-jobs/:id`.
  - Run manual test print via `POST /print/test`.

## Security notes
- Keep `PRINT_AGENT_PAIRING_TOKEN` and `PRINT_AGENT_SIGNING_SECRET` in local secure deployment automation.
- Restrict allowed origins to known UI hosts.
- Enable SHA256 validation in update manifests.
