#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Build and sign the print-agent binary, generate manifest with SHA256.
    
.DESCRIPTION
    Compiles print-agent from source, calculates SHA256, and generates a manifest JSON
    that includes version, SHA256, and download URL for auto-update functionality.
    
    Output:
    - Binary: $OutputDir/cybererp-print-agent.exe
    - Manifest: $ManifestDir/print-agent-manifest.json
    - SHA256: Logged and embedded in manifest
    
.PARAMETER SourceDir
    Path to print-agent-go directory. Default: parent of this script.
    
.PARAMETER OutputDir
    Output directory for compiled binary. Default: $SourceDir/dist
    
.PARAMETER ManifestDir
    Output directory for manifest JSON. Default: $OutputDir
    
.PARAMETER Version
    Semantic version string (e.g., 0.2.0). Default: extracted from git tag or fallback to 0.1.0
    
.PARAMETER DownloadUrl
    Base URL for downloading binary (e.g., https://api.cybererp.local/public/print-agent/).
    Manifest will point to: {DownloadUrl}cybererp-print-agent.exe
    
.PARAMETER CodeSignCertThumbprint
    Thumbprint of code-signing certificate (optional). If provided, will sign the binary.
    Certificate must be in CurrentUser\My store.
    
.EXAMPLE
    .\build-and-sign.ps1 -DownloadUrl "https://releases.example.com/"
    
.EXAMPLE
    .\build-and-sign.ps1 -Version "0.2.1" -CodeSignCertThumbprint "ABC123..." `
        -DownloadUrl "https://api.cybererp.local/public/print-agent/"
#>
param(
    [string]$SourceDir = (Split-Path -Parent $PSScriptRoot),
    [string]$OutputDir = (Join-Path $SourceDir "dist"),
    [string]$ManifestDir = $OutputDir,
    [string]$Version,
    [string]$DownloadUrl,
    [string]$CodeSignCertThumbprint
)

$ErrorActionPreference = "Stop"

# Suppress progress bars for cleaner output
$ProgressPreference = "SilentlyContinue"

Write-Host "==============================================="
Write-Host "Print Agent Build & Sign Pipeline"
Write-Host "==============================================="
Write-Host ""

# ============================================================================
# 1. RESOLVE VERSION
# ============================================================================
if (-not $Version) {
    Write-Host "📋 Resolving version from git..." -ForegroundColor Cyan
    Push-Location $SourceDir
    try {
        $gitTag = git describe --tags --abbrev=0 2>$null
        if ($gitTag) {
            $Version = $gitTag -replace '^v', ''
        } else {
            $Version = "0.1.0"
        }
    } catch {
        $Version = "0.1.0"
    }
    Pop-Location
    Write-Host "   ✓ Version: $Version"
} else {
    Write-Host "📋 Using provided version: $Version" -ForegroundColor Cyan
}

# ============================================================================
# 2. CREATE OUTPUT DIRECTORIES
# ============================================================================
Write-Host ""
Write-Host "📂 Preparing output directories..." -ForegroundColor Cyan
if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
    Write-Host "   ✓ Created: $OutputDir"
} else {
    Write-Host "   ✓ Already exists: $OutputDir"
}

if (-not (Test-Path $ManifestDir)) {
    New-Item -ItemType Directory -Path $ManifestDir -Force | Out-Null
    Write-Host "   ✓ Created: $ManifestDir"
} else {
    Write-Host "   ✓ Already exists: $ManifestDir"
}

# ============================================================================
# 3. COMPILE BINARY
# ============================================================================
Write-Host ""
Write-Host "🔨 Compiling print-agent binary..." -ForegroundColor Cyan

$binaryPath = Join-Path $OutputDir "cybererp-print-agent.exe"
$cmdPath = Join-Path $SourceDir "cmd" "print-agent" "main.go"

# Remove old binary if exists
if (Test-Path $binaryPath) {
    Remove-Item $binaryPath -Force | Out-Null
}

try {
    Push-Location $SourceDir
    
    # Compile with embedded version
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    
    $ldflags = "-X main.Version=$Version -X main.BuildTime=$(Get-Date -Format 'o')"
    
    Write-Host "   Building: go build -ldflags '$ldflags' -o '$binaryPath' '$cmdPath'"
    go build -ldflags $ldflags -o $binaryPath $cmdPath
    
    if (-not (Test-Path $binaryPath)) {
        throw "Build failed: binary not created"
    }
    
    $fileSize = (Get-Item $binaryPath).Length
    $fileSizeMB = [math]::Round($fileSize / 1MB, 2)
    Write-Host "   ✓ Binary compiled: $fileSize bytes ($fileSizeMB MB)"
    
} catch {
    Write-Host "   ✗ Build failed: $_" -ForegroundColor Red
    exit 1
} finally {
    Pop-Location
}

# ============================================================================
# 4. CALCULATE SHA256
# ============================================================================
Write-Host ""
Write-Host "🔐 Calculating SHA256 hash..." -ForegroundColor Cyan

$sha256Hash = (Get-FileHash -Path $binaryPath -Algorithm SHA256).Hash
Write-Host "   ✓ SHA256: $sha256Hash"

# ============================================================================
# 5. CODE SIGNING (OPTIONAL)
# ============================================================================
if ($CodeSignCertThumbprint) {
    Write-Host ""
    Write-Host "✍️  Code signing binary..." -ForegroundColor Cyan
    
    try {
        $cert = Get-ChildItem -Path Cert:\CurrentUser\My\$CodeSignCertThumbprint -ErrorAction Stop
        
        Set-AuthenticodeSignature -FilePath $binaryPath -Certificate $cert -TimestampServer "http://timestamp.digicert.com" | Out-Null
        
        Write-Host "   ✓ Binary signed with certificate: $($cert.Subject)"
        Write-Host "   ⚠️  SHA256 may have changed after signing - recalculating..."
        
        # Recalculate SHA256 after signing
        Start-Sleep -Milliseconds 500
        $sha256Hash = (Get-FileHash -Path $binaryPath -Algorithm SHA256).Hash
        Write-Host "   ✓ SHA256 (after signing): $sha256Hash"
        
    } catch {
        Write-Host "   ✗ Code signing failed: $_" -ForegroundColor Red
        Write-Host "   ⚠️  Continuing without signature. Manifest will use SHA256 only."
    }
} else {
    Write-Host ""
    Write-Host "⚠️  Code signing skipped (no certificate provided)" -ForegroundColor Yellow
}

# ============================================================================
# 6. GENERATE MANIFEST JSON
# ============================================================================
Write-Host ""
Write-Host "📝 Generating manifest JSON..." -ForegroundColor Cyan

if (-not $DownloadUrl) {
    Write-Host "   ⚠️  DownloadUrl not provided; using placeholder" -ForegroundColor Yellow
    $DownloadUrl = "https://releases.example.com/"
}

# Ensure DownloadUrl ends with /
if (-not $DownloadUrl.EndsWith('/')) {
    $DownloadUrl += '/'
}

$manifest = @{
    version        = $Version
    timestamp      = (Get-Date -Format 'o')
    sha256         = $sha256Hash
    download_url   = "${DownloadUrl}cybererp-print-agent.exe"
    file_size      = $fileSize
    file_size_mb   = $fileSizeMB
    signed         = if ($CodeSignCertThumbprint) { $true } else { $false }
    release_notes  = "Automated build from print-agent-go"
} | ConvertTo-Json -Depth 10

$manifestPath = Join-Path $ManifestDir "print-agent-manifest.json"
$manifest | Out-File -FilePath $manifestPath -Encoding UTF8 -Force

Write-Host "   ✓ Manifest: $manifestPath"
Write-Host ""
Write-Host "📦 Manifest Content:"
Write-Host "---"
$manifest | ConvertFrom-Json | Format-Table -Wrap
Write-Host "---"

# ============================================================================
# 7. SUMMARY & INSTRUCTIONS
# ============================================================================
Write-Host ""
Write-Host "✅ Build Complete!" -ForegroundColor Green
Write-Host ""
Write-Host "📦 Artifacts:"
Write-Host "   Binary:   $binaryPath"
Write-Host "   Manifest: $manifestPath"
Write-Host ""
Write-Host "📋 Next Steps:"
Write-Host "   1. Upload binary to: $DownloadUrl"
Write-Host "   2. Copy manifest to gateway: /internal/gateway/releases/manifest.json"
Write-Host "   3. Update gateway to serve: GET /public/print-agent/latest"
Write-Host "   4. Update PRINT_AGENT_VERSION env var to: $Version"
Write-Host ""
Write-Host "🔗 Manifest URL (for update script):"
Write-Host "   http://localhost:8080/public/print-agent/latest"
Write-Host ""

exit 0
