# Print Agent Windows Support Playbook

## Scope
Operational guide to install, update, verify and uninstall CyberERP Print Agent on Windows endpoints.

## Prerequisites
- Windows 10/11 with local admin access.
- Thermal printer installed in Windows.
- PowerShell execution policy allowing script run in deployment shell.

## Installation

### Method A — Self-installer (recommended for end-users)
1. In the CyberERP UI go to **Configuración de Impresión → Instalador**.
2. Enter the agent name (e.g. "Caja 1") and click **Descargar Instalador**.
3. Copy `instalar-agente-cyberpos.bat` to the target machine.
4. Right-click → **Ejecutar como Administrador**.

### Method B — PowerShell script (IT/dev use)
1. Build or download `cybererp-print-agent.exe`.
2. Run:
```powershell
Set-Location "print-agent-go"
.\deploy\windows\install-agent.ps1 -ExePath ".\dist\cybererp-print-agent.exe" `
  -GatewayWSUrl "wss://api.createam.cloud/api/v1/print-agent/ws" `
  -AgentToken "<token>"
```
3. Verify service exists:
```powershell
Get-Service -Name CyberERPPrintAgent
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
  - Check service state: `sc query CyberERPPrintAgent`
  - Check running process: `Get-Process cybererp-print-agent`
  - Check Windows Event Log: `Get-EventLog -LogName Application -Source CyberERPPrintAgent -Newest 10`
- Service fails to start (most common on new machines):
  - Verify `agent.env` exists: `Test-Path "C:\Program Files\CyberERP\PrintAgent\agent.env"`
  - Uninstall and re-run the installer .bat as Administrator:
    `& "C:\Program Files\CyberERP\PrintAgent\cybererp-print-agent.exe" uninstall`
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
