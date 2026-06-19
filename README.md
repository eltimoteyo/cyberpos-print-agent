# Print Agent Go

Local bridge and Windows service for POS printing in CyberERP.

## Modes

The agent can run in two modes:

1. **Console / development mode**: `go run ./cmd/print-agent`
2. **Windows Service mode**: when started by the Service Control Manager (no user session required).

When running as a Windows service, the agent reads configuration from an `agent.env` file placed next to the executable. This avoids registry edits and makes deployment repeatable.

## Implemented endpoints (HTTP local)

- `GET /health`
- `GET /printers` (Windows discovery)
- `POST /config`
- `GET /config`
- `POST /print/test`
- `POST /print/ticket`
- `GET /status`

## Gateway WebSocket push

The agent can connect to `api-gateway` via WebSocket to receive print jobs in real time, enabling:

- Always-on service without a logged-in user.
- Remote/cross-device printing (tablet/smartphone/another POS sends jobs to this agent).
- Instant job delivery instead of relying on the frontend to reach `127.0.0.1:12345`.

The agent auto-generates and persists a unique `agent_id` in its data directory.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `PRINT_AGENT_ADDR` | `127.0.0.1:12345` | Bind address for the local HTTP server |
| `PRINT_AGENT_VERSION` | `0.1.0` | Version reported in `/health` and `/status` |
| `PRINT_AGENT_ALLOWED_ORIGINS` | `http://localhost:4200` | Extra CORS origins (mandatory origins are always included) |
| `PRINT_AGENT_PAIRING_TOKEN` | `""` | If set, required in header `X-Agent-Token` for write endpoints |
| `PRINT_AGENT_SIGNING_SECRET` | `""` | If set, `POST /print/ticket` requires `X-Agent-Timestamp` + `X-Agent-Signature` |
| `PRINT_AGENT_MAX_CLOCK_SKEW_SEC` | `300` | Tolerance for signed timestamps |
| `PRINT_AGENT_RATE_LIMIT_PER_MIN` | `120` | Rate limit for write operations |
| `PRINT_AGENT_QUEUE_SIZE` | `200` | Local print queue capacity |
| `PRINT_AGENT_MAX_RETRIES` | `3` | Local retry attempts per ticket |
| `PRINT_AGENT_DATA_DIR` | `""` | Data directory. Defaults to `%PROGRAMDATA%\CyberERP\PrintAgent` when running as a service, or `%UserConfigDir%\cybererp\print-agent` otherwise |
| `PRINT_AGENT_GATEWAY_WS_URL` | `""` | WebSocket URL of the gateway (e.g. `wss://api.createam.cloud/api/v1/print-agent/ws`) |
| `PRINT_AGENT_ID` | `""` | Stable agent ID. Auto-generated and persisted if empty |
| `PRINT_AGENT_TOKEN` | `""` | JWT access token used to authenticate the WebSocket connection |
| `PRINT_AGENT_HOSTNAME` | `""` | Hostname reported to the gateway. Auto-detected if empty |
| `PRINT_AGENT_CAPABILITIES` | `escpos` | Comma-separated capabilities reported to the gateway (`escpos`, `a4`, `pdf`) |

## Run locally

```bash
cd print-agent-go
go run ./cmd/print-agent
```

Default address: `http://127.0.0.1:12345`

## Install as Windows service

```powershell
# Build or use the prebuilt dist binary
cd deploy/windows
.\install-agent.ps1 `
  -ExePath "..\..\dist\cybererp-print-agent.exe" `
  -GatewayWSUrl "wss://api.createam.cloud/api/v1/print-agent/ws" `
  -AgentToken "<jwt-access-token>"
```

The installer:

- Copies the executable to `C:\Program Files\CyberERP\PrintAgent\`.
- Creates the data directory `C:\ProgramData\CyberERP\PrintAgent\`.
- Writes an `agent.env` file next to the executable.
- Removes any legacy scheduled task.
- Registers a Windows service named `CyberERPPrintAgent` with automatic recovery.
- Starts the service.

To uninstall:

```powershell
.\uninstall-agent.ps1
```

To update:

```powershell
.\update-agent.ps1 -ApiBaseUrl "https://api.createam.cloud/api/v1"
```

## Test endpoints

```bash
curl http://127.0.0.1:12345/health
curl http://127.0.0.1:12345/printers
curl -X POST http://127.0.0.1:12345/config \
  -H "Content-Type: application/json" \
  -d '{"printerName":"EPSON TM-T20","connectionType":"usb","paperWidth":"80mm","autoCut":true,"openDrawerOnSale":true}'
curl http://127.0.0.1:12345/config
curl -X POST http://127.0.0.1:12345/print/test \
  -H "Content-Type: application/json" \
  -d '{"printerName":"EPSON TM-T20"}'
curl -X POST http://127.0.0.1:12345/print/ticket \
  -H "Content-Type: application/json" \
  -d '{"jobId":"job-001","saleId":"sale-001","title":"Venta","lines":["Producto A x1 S/ 10.00","TOTAL S/ 10.00"],"footer":["Gracias por su compra"],"openDrawer":true,"cutPaper":true,"apiBaseUrl":"http://api.yourserver.com/api/v1","bearerToken":"<access_token>"}'
curl http://127.0.0.1:12345/status
```

If `PRINT_AGENT_SIGNING_SECRET` is configured, sign `POST /print/ticket` like this (PowerShell):

```powershell
$ts = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds().ToString()
$secret = "my-shared-secret"
$hmac = New-Object System.Security.Cryptography.HMACSHA256
$hmac.Key = [Text.Encoding]::UTF8.GetBytes($secret)
$sigBytes = $hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($ts))
$sig = ($sigBytes | ForEach-Object { $_.ToString('x2') }) -join ''

Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:12345/print/ticket" `
  -Headers @{ "X-Agent-Timestamp" = $ts; "X-Agent-Signature" = $sig } `
  -ContentType "application/json" `
  -Body '{"jobId":"job-001","saleId":"sale-001","title":"Venta","lines":["Producto A x1 S/ 10.00"],"openDrawer":true,"cutPaper":true}'
```

Notes:
- `apiBaseUrl` and `bearerToken` are optional.
- If `jobId` + `apiBaseUrl` are provided, the agent will report ticket result to:
  - `POST {apiBaseUrl}/print-jobs/{jobId}/result`
- When connected to the gateway via WebSocket, results are also reported through the persistent channel.
- `POST /print/ticket` queues jobs and returns `202 Accepted`.
- Duplicate `jobId` requests are handled idempotently.
- Ticket printing uses RAW ESC/POS bytes on Windows, including optional drawer pulse and paper cut commands.
- Agent emits structured JSON logs for HTTP requests and print-job reporting outcomes.

## Windows Rollout Scripts

Scripts are available under `deploy/windows`:

- `install-agent.ps1`: installs executable under `Program Files`, creates data dir under `ProgramData`, registers Windows service, and starts it.
- `update-agent.ps1`: downloads latest installer from gateway manifest endpoint, stops the service, replaces executable (with optional SHA256 check), and restarts.
- `uninstall-agent.ps1`: stops/removes service and deletes install/data directories.

Support runbook:

- `docs/SUPPORT_PLAYBOOK_WINDOWS.md`
