# Print Agent Technical Design (Phase 0)

## Scope
This document defines the initial technical contract for the local print agent:
- JSON schemas
- State model
- Error codes catalog
- Hardware support matrix

## API Schemas

### GET /health response
```json
{
  "status": "ok",
  "service": "print-agent",
  "version": "0.1.0",
  "time": "2026-05-07T12:00:00Z"
}
```

### GET /printers response
```json
{
  "printers": [
    {
      "name": "EPSON TM-T20",
      "default": true,
      "workOffline": false,
      "portName": "USB001",
      "driverName": "EPSON TM-T20 Driver"
    }
  ]
}
```

### POST /config request
```json
{
  "printerName": "EPSON TM-T20",
  "connectionType": "usb",
  "paperWidth": "80mm",
  "autoCut": true,
  "openDrawerOnSale": true
}
```

### GET /config response
```json
{
  "config": {
    "printerName": "EPSON TM-T20",
    "connectionType": "usb",
    "paperWidth": "80mm",
    "autoCut": true,
    "openDrawerOnSale": true
  }
}
```

### POST /print/test request
```json
{
  "printerName": "EPSON TM-T20"
}
```

### POST /print/ticket request
```json
{
  "jobId": "pj_12345",
  "saleId": "84",
  "printerName": "EPSON TM-T20",
  "title": "Boleta B001-123",
  "lines": [
    "Producto A x1 S/ 10.00",
    "TOTAL S/ 10.00"
  ],
  "footer": [
    "Gracias por su compra"
  ],
  "openDrawer": true,
  "cutPaper": true,
  "apiBaseUrl": "https://api.createam.cloud/api/v1",
  "bearerToken": "<jwt>"
}
```

### POST /print/ticket response (queued)
```json
{
  "message": "ticket queued",
  "printerName": "EPSON TM-T20",
  "jobId": "pj_12345",
  "saleId": "84",
  "openDrawer": true,
  "cutPaper": true
}
```

### GET /status response
```json
{
  "status": {
    "connected": true,
    "version": "0.1.0",
    "lastJobId": "pj_12345",
    "lastSaleId": "84",
    "lastPrinterName": "EPSON TM-T20",
    "lastPrintStatus": "success",
    "lastError": "",
    "lastPrintAt": "2026-05-07T12:03:00Z",
    "successfulPrints": 10,
    "failedPrints": 1,
    "lastUpdatedAt": "2026-05-07T12:03:00Z",
    "configuredPrinter": "EPSON TM-T20",
    "queueDepth": 0,
    "queueCapacity": 200
  }
}
```

## State Model

### Print job lifecycle in agent
- queued
- processing
- printed
- failed

### Idempotency behavior
- `jobId` already completed: return success response with `duplicate=true`.
- `jobId` currently queued/processing: return accepted response with `duplicate=true`.

## Error Codes Catalog

Errors are returned as:
```json
{
  "error": "human readable message"
}
```

Proposed stable catalog:
- `PA-400-JSON`: invalid JSON payload
- `PA-400-LINES`: ticket lines are empty
- `PA-400-CONFIG`: local config missing or invalid
- `PA-401-TOKEN`: invalid or missing pairing token
- `PA-403-ORIGIN`: origin not allowed
- `PA-404-CONFIG`: config not found
- `PA-500-PRINTER-LIST`: failed to list printers
- `PA-500-CONFIG-SAVE`: failed to save config
- `PA-500-CONFIG-LOAD`: failed to load config
- `PA-503-QUEUE-FULL`: print queue is full

Note: current implementation returns messages; this catalog is the canonical mapping for client handling.

## Hardware Support Matrix (Initial)

| OS | Transport | Discovery | Print Path | Status |
|---|---|---|---|---|
| Windows 10/11 | USB | Win32_Printer via PowerShell | Out-Printer text pipeline | Supported (MVP) |
| Windows Server | USB/LAN | Win32_Printer via PowerShell | Out-Printer text pipeline | Supported with validation |
| Linux | USB/LAN | Not implemented | Not implemented | Planned |
| macOS | USB/LAN | Not implemented | Not implemented | Planned |

### Device notes
- Epson TM-T20/TM-T88 class: expected compatible in text mode.
- Generic thermal drivers: depends on OS spooler driver behavior.
- Raw ESC/POS byte path for deterministic cut/drawer: planned next.

## Security Baseline Mapping

- Local bind only (`127.0.0.1`): implemented.
- Origin allow-list check: implemented.
- Pairing token header (`X-Agent-Token`): implemented as optional runtime setting.
- Signed command payload validation: implemented (HMAC-SHA256 over timestamp for `POST /print/ticket`).
- Local rate limit: implemented (fixed-window, per IP+method+path for write operations).
- Structured logs: implemented (JSON log lines with event, level, status and duration).

## Open Decisions
- Keep gateway-backed status as source of truth vs local-only job status.
- Add strict schema validation with typed error code payload.
- Define support policy for non-Windows environments.
