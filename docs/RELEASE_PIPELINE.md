# Print Agent Release Pipeline

This document describes the process for building, signing, and distributing the print-agent binary with automatic update support.

## Overview

The release pipeline consists of:

1. **Build & Sign**: Compile binary, calculate SHA256, optionally sign
2. **Manifest Generation**: Create JSON metadata with version, hash, and download URL
3. **Binary Distribution**: Host binary on CDN or artifact server
4. **Gateway Configuration**: Update environment variables with new version/hash
5. **Auto-Update**: Print agent periodically checks for updates and auto-installs

## Build & Sign Process

### Prerequisites

```powershell
# Install/verify Go is available
go version

# For code signing (optional), ensure certificate exists
Get-ChildItem Cert:\CurrentUser\My\ | Where-Object Subject -like "*Code Signing*"
```

### Build Binary with SHA256

```powershell
# From project root:
cd print-agent-go

# Run build script (version auto-detected from git tag)
.\deploy\windows\build-and-sign.ps1 `
    -DownloadUrl "https://releases.example.com/" `
    -Version "0.2.0"

# With code signing
.\deploy\windows\build-and-sign.ps1 `
    -DownloadUrl "https://releases.example.com/" `
    -Version "0.2.0" `
    -CodeSignCertThumbprint "ABC123DEF456..."
```

### Output

```
dist/
  ├── cybererp-print-agent.exe      (11.2 MB)
  └── print-agent-manifest.json     (metadata)

📦 Manifest Content:
{
  "version": "0.2.0",
  "timestamp": "2026-05-07T14:30:00Z",
  "sha256": "a7b3c5d9...",
  "download_url": "https://releases.example.com/cybererp-print-agent.exe",
  "file_size": 11747328,
  "file_size_mb": 11.2,
  "signed": true,
  "release_notes": "..."
}
```

## Binary Distribution

### Option A: Self-Hosted S3/Azure Storage

```bash
# Upload binary to S3
aws s3 cp dist/cybererp-print-agent.exe s3://releases-bucket/print-agent/0.2.0/

# Verify upload
aws s3 ls s3://releases-bucket/print-agent/0.2.0/
```

### Option B: GitHub Releases

```bash
# Create release on GitHub
gh release create v0.2.0 dist/cybererp-print-agent.exe \
  --title "Print Agent v0.2.0" \
  --notes "$(cat release-notes.md)"

# Binary available at:
# https://github.com/owner/cyberpos/releases/download/v0.2.0/cybererp-print-agent.exe
```

### Option C: Local File Server (Development)

```bash
# Serve from local directory for testing
cd dist
python -m http.server 9000
# Binary available at: http://localhost:9000/cybererp-print-agent.exe
```

## Gateway Configuration

The api-gateway serves the manifest at `GET /api/v1/public/print-agent/latest`.

### Environment Variables

```bash
# Version number
PRINT_AGENT_VERSION=0.2.0

# SHA256 hash of binary
PRINT_AGENT_SHA256=a7b3c5d9e1f2g3h4i5j6k7l8m9n0o1p2q3r4s5t6u7v8w9x0y1z2

# Download URL (must be publicly accessible)
PRINT_AGENT_INSTALLER_URL=https://releases.example.com/cybererp-print-agent.exe
```

### Docker Compose

```yaml
# docker-compose.yml
services:
  api-gateway:
    image: cyberpos/api-gateway:latest
    environment:
      PRINT_AGENT_VERSION: "0.2.0"
      PRINT_AGENT_SHA256: "a7b3c5d9e1f2g3h4i5j6k7l8m9n0o1p2q3r4s5t6u7v8w9x0y1z2"
      PRINT_AGENT_INSTALLER_URL: "https://releases.example.com/cybererp-print-agent.exe"
```

### Kubernetes ConfigMap

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-gateway-config
  namespace: cybererp
data:
  PRINT_AGENT_VERSION: "0.2.0"
  PRINT_AGENT_SHA256: "a7b3c5d9e1f2g3h4i5j6k7l8m9n0o1p2q3r4s5t6u7v8w9x0y1z2"
  PRINT_AGENT_INSTALLER_URL: "https://releases.example.com/cybererp-print-agent.exe"
```

## Manifest Endpoint

### Request

```bash
curl -X GET http://localhost:8080/api/v1/public/print-agent/latest
```

### Response (200 OK)

```json
{
  "name": "cybererp-print-agent",
  "version": "0.2.0",
  "sha256": "a7b3c5d9e1f2g3h4i5j6k7l8m9n0o1p2q3r4s5t6u7v8w9x0y1z2",
  "download_url": "https://releases.example.com/cybererp-print-agent.exe"
}
```

### Response (404 Not Found)

```json
{
  "error": "print agent installer is not configured"
}
```

## Auto-Update Flow

### Manual Update

```powershell
# Run update script (auto-downloads if new version available)
.\deploy\windows\update-agent.ps1 `
    -ManifestUrl "http://localhost:8080/api/v1/public/print-agent/latest"

# Expected output:
# ✓ Current version: 0.1.0
# ✓ Latest version: 0.2.0
# ✓ Update available, downloading...
# ✓ SHA256 verified: OK
# ✓ Agent restarted successfully
```

### Scheduled Auto-Update (Windows Task Scheduler)

```powershell
# Create scheduled task for daily checks
$taskName = "CyberERP Print Agent Update Check"
$action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument `
    "-NoProfile -ExecutionPolicy Bypass -File C:\PrintAgent\update-agent.ps1"
$trigger = New-ScheduledTaskTrigger -Daily -At 02:00AM
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -RunLevel Highest
```

## Rollback

If a release has critical issues, rollback by reverting the environment variables:

```bash
# Revert to previous version
PRINT_AGENT_VERSION=0.1.0
PRINT_AGENT_SHA256=previous_hash_value
PRINT_AGENT_INSTALLER_URL=https://releases.example.com/cybererp-print-agent-0.1.0.exe
```

Restart api-gateway. Existing installations will auto-update back to 0.1.0.

## Version Numbering

Follow semantic versioning: **MAJOR.MINOR.PATCH**

- **MAJOR**: Breaking changes (API changes, hardware compatibility changes)
- **MINOR**: New features (new endpoints, new printer models, new ESC/POS commands)
- **PATCH**: Bug fixes and stability improvements

Examples:
- `0.1.0` → Initial release
- `0.2.0` → Add rate limiting, structured logging
- `0.2.1` → Fix rate limit calculation bug
- `1.0.0` → Production ready

## CI/CD Integration

### GitHub Actions Example

```yaml
# .github/workflows/release.yml
name: Release Print Agent

on:
  push:
    tags:
      - 'v*'

jobs:
  build:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.24'
      
      - name: Build and Sign
        run: |
          cd print-agent-go
          .\deploy\windows\build-and-sign.ps1 `
              -Version ${{ github.ref_name }} `
              -DownloadUrl "https://releases.example.com/"
      
      - name: Upload Release Asset
        uses: softprops/action-gh-release@v1
        with:
          files: dist/cybererp-print-agent.exe
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      
      - name: Update Gateway Config
        run: |
          $version = "${{ github.ref_name }}" -replace '^v', ''
          $hash = (Get-FileHash dist/cybererp-print-agent.exe -Algorithm SHA256).Hash
          
          # Push to ConfigMap/deployment
          kubectl patch configmap api-gateway-config \
              -p "{\"data\":{\"PRINT_AGENT_VERSION\":\"$version\",\"PRINT_AGENT_SHA256\":\"$hash\"}}"
```

## Testing the Release

### 1. Verify Manifest Endpoint

```bash
curl http://localhost:8080/api/v1/public/print-agent/latest | jq .
```

### 2. Verify SHA256

```powershell
$downloaded = "C:\temp\cybererp-print-agent.exe"
$expected = "a7b3c5d9e1f2..."

$actual = (Get-FileHash $downloaded -Algorithm SHA256).Hash
if ($actual -eq $expected) {
    Write-Host "✓ SHA256 verified"
} else {
    Write-Host "✗ SHA256 mismatch"
    exit 1
}
```

### 3. Test Update Script

```powershell
# On test machine with 0.1.0 installed
.\update-agent.ps1 -ManifestUrl "http://staging-gateway:8080/api/v1/public/print-agent/latest"

# Verify update
.\install-agent.ps1 -VerifyOnly
```

## Troubleshooting

### Issue: "SHA256 mismatch"

**Cause**: Binary was modified after generation, or manifest has wrong value.

**Fix**:
1. Regenerate binary: `.\build-and-sign.ps1 -DownloadUrl "..."`
2. Verify SHA256 matches manifest
3. Update gateway env var: `PRINT_AGENT_SHA256=<new_value>`

### Issue: "Failed to download binary"

**Cause**: Download URL is not accessible or network issue.

**Fix**:
1. Verify URL is public: `curl -I $PRINT_AGENT_INSTALLER_URL`
2. Check firewall/security groups allow outbound HTTPS
3. Verify certificate chain: `Get-AuthenticodeSignature $binaryPath | Select-Object Status`

### Issue: "Update script keeps downloading"

**Cause**: Gateway is returning old version or version parsing is failing.

**Fix**:
1. Verify gateway manifest: `curl http://gateway:8080/api/v1/public/print-agent/latest`
2. Check update script version comparison logic
3. Restart gateway: `docker restart api-gateway`

## Security Considerations

### Code Signing

- Use an EV Code Signing certificate for production
- Store private key securely (HSM or Azure Key Vault)
- Timestamped signatures prevent expiration issues

### SHA256 Verification

- SHA256 is cryptographically secure against known attacks
- Update script verifies before execution (prevents MITM attacks)
- Store SHA256 in gateway config (not in binary)

### Download URL Security

- Always use HTTPS with valid certificate
- Consider pinning certificate chain in update script
- Monitor CDN logs for unusual download patterns

## Maintenance

### Monthly

1. Review release notes and security advisories
2. Check auto-update success rates in gateway logs
3. Monitor update script failures across installed agents

### Quarterly

1. Test full release pipeline (build, sign, upload, deploy)
2. Verify rollback procedures work correctly
3. Update CI/CD pipeline and documentation

## References

- [Print Agent Architecture](../PRINTING_POS_ARCHITECTURE_PLAN.md)
- [Support Playbook - Windows](./SUPPORT_PLAYBOOK_WINDOWS.md)
- [Go Build Documentation](https://golang.org/cmd/go/)
- [Semantic Versioning](https://semver.org/)
