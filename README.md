# Print Agent Go (MVP)

Local bridge for POS printing in CyberERP.

## Implemented in this first step

- `GET /health`
- `GET /printers` (Windows discovery)
- `POST /config`
- `GET /config`
- `POST /print/test`
- `POST /print/ticket`
- `GET /status`

## Run locally

```bash
cd print-agent-go
go run ./cmd/print-agent
```

Default address: `127.0.0.1:12345`

Optional env vars:

- `PRINT_AGENT_VERSION` (default: `0.1.0`)
- `PRINT_AGENT_ALLOWED_ORIGINS` (comma-separated, default: `http://localhost:4200`)
- `PRINT_AGENT_PAIRING_TOKEN` (if set, required in header `X-Agent-Token` for `POST /config`, `POST /print/test`, `POST /print/ticket`)
- `PRINT_AGENT_SIGNING_SECRET` (if set, `POST /print/ticket` requires `X-Agent-Timestamp` and `X-Agent-Signature`)
- `PRINT_AGENT_MAX_CLOCK_SKEW_SEC` (default: `300`)
- `PRINT_AGENT_RATE_LIMIT_PER_MIN` (default: `120`, applies to write operations)
- `PRINT_AGENT_QUEUE_SIZE` (default: `200`)
- `PRINT_AGENT_MAX_RETRIES` (default: `3`)

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

Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:12345/print/ticket" \
  -Headers @{ "X-Agent-Timestamp" = $ts; "X-Agent-Signature" = $sig } \
  -ContentType "application/json" \
  -Body '{"jobId":"job-001","saleId":"sale-001","title":"Venta","lines":["Producto A x1 S/ 10.00"],"openDrawer":true,"cutPaper":true}'
```

Notes:
- `apiBaseUrl` and `bearerToken` are optional.
- If `jobId` + `apiBaseUrl` are provided, the agent will report ticket result to:
  - `POST {apiBaseUrl}/print-jobs/{jobId}/result`
- `POST /print/ticket` now queues jobs and returns `202 Accepted`.
- Duplicate `jobId` requests are handled idempotently.
- Ticket printing now uses RAW ESC/POS bytes on Windows, including optional drawer pulse and paper cut commands.
- Agent emits structured JSON logs for HTTP requests and print-job reporting outcomes.

## Windows Rollout Scripts

Scripts are available under `deploy/windows`:

- `install-agent.ps1`: installs executable under `Program Files`, registers autostart scheduled task, and starts the agent.
- `update-agent.ps1`: downloads latest installer from gateway manifest endpoint and replaces executable (with optional SHA256 check).
- `uninstall-agent.ps1`: removes process, scheduled task and install directory.

Support runbook:

- `docs/SUPPORT_PLAYBOOK_WINDOWS.md`