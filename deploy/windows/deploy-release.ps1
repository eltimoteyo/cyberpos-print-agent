#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Deploy a print-agent release to gateway configuration.
    
.DESCRIPTION
    Updates the api-gateway with new print-agent version, SHA256, and download URL.
    Can update Docker environment variables or Kubernetes ConfigMap.
    
.PARAMETER ManifestPath
    Path to print-agent-manifest.json file from build output.
    
.PARAMETER Environment
    Target environment: "docker" or "k8s" (Kubernetes).
    
.PARAMETER NamespaceName
    Kubernetes namespace (only for -Environment k8s). Default: "cybererp"
    
.PARAMETER DockerServiceName
    Docker service name (only for -Environment docker). Default: "api-gateway"
    
.PARAMETER DockerComposeFile
    Path to docker-compose file (only for -Environment docker).
    
.EXAMPLE
    .\deploy-release.ps1 -ManifestPath ".\print-agent-go\dist\print-agent-manifest.json" `
        -Environment docker -DockerComposeFile docker-compose.prod.yml
    
.EXAMPLE
    .\deploy-release.ps1 -ManifestPath ".\print-agent-go\dist\print-agent-manifest.json" `
        -Environment k8s -NamespaceName cybererp-prod
#>
param(
    [Parameter(Mandatory=$true)]
    [ValidateScript({ Test-Path $_ })]
    [string]$ManifestPath,
    
    [Parameter(Mandatory=$true)]
    [ValidateSet("docker", "k8s")]
    [string]$Environment,
    
    [string]$NamespaceName = "cybererp",
    [string]$DockerServiceName = "api-gateway",
    [string]$DockerComposeFile = "docker-compose.prod.yml"
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

Write-Host "==============================================="
Write-Host "Print Agent Release Deployment"
Write-Host "==============================================="
Write-Host ""

# ============================================================================
# 1. PARSE MANIFEST
# ============================================================================
Write-Host "📋 Reading manifest..." -ForegroundColor Cyan

$manifest = Get-Content -Path $ManifestPath -Raw | ConvertFrom-Json

Write-Host "   Version:     $($manifest.version)"
Write-Host "   SHA256:      $($manifest.sha256.Substring(0, 16))..."
Write-Host "   Download:    $($manifest.download_url)"
Write-Host "   File Size:   $($manifest.file_size_mb) MB"
Write-Host ""

# ============================================================================
# 2. DEPLOY TO ENVIRONMENT
# ============================================================================

if ($Environment -eq "docker") {
    Write-Host "🐳 Deploying to Docker..." -ForegroundColor Cyan
    
    if (-not (Test-Path $DockerComposeFile)) {
        Write-Host "   ✗ File not found: $DockerComposeFile" -ForegroundColor Red
        exit 1
    }
    
    # Read current docker-compose.yml
    Write-Host "   Reading: $DockerComposeFile"
    
    # Use sed to update environment variables (cross-platform via PowerShell)
    $content = Get-Content -Path $DockerComposeFile -Raw
    
    # Update version
    $content = $content -replace `
        "(?<=PRINT_AGENT_VERSION:\s*)['\`"]?[^'\`""`n]+['\`"]?", `
        "`"$($manifest.version)`""
    
    # Update SHA256
    $content = $content -replace `
        "(?<=PRINT_AGENT_SHA256:\s*)['\`"]?[^'\`""`n]+['\`"]?", `
        "`"$($manifest.sha256)`""
    
    # Update URL
    $content = $content -replace `
        "(?<=PRINT_AGENT_INSTALLER_URL:\s*)['\`"]?[^'\`""`n]+['\`"]?", `
        "`"$($manifest.download_url)`""
    
    # Write back
    Set-Content -Path $DockerComposeFile -Value $content -Encoding UTF8
    
    Write-Host "   ✓ Updated: $DockerComposeFile"
    Write-Host ""
    Write-Host "📋 Docker-compose environment updated:"
    Write-Host "   PRINT_AGENT_VERSION=$($manifest.version)"
    Write-Host "   PRINT_AGENT_SHA256=$($manifest.sha256.Substring(0, 32))..."
    Write-Host "   PRINT_AGENT_INSTALLER_URL=$($manifest.download_url)"
    Write-Host ""
    Write-Host "⚠️  Next steps:"
    Write-Host "   1. Review changes: git diff $DockerComposeFile"
    Write-Host "   2. Restart service: docker-compose -f $DockerComposeFile up -d $DockerServiceName"
    Write-Host "   3. Verify: curl http://localhost:8080/api/v1/public/print-agent/latest"
    
} elseif ($Environment -eq "k8s") {
    Write-Host "☸️  Deploying to Kubernetes..." -ForegroundColor Cyan
    
    # Check if kubectl is available
    try {
        $null = kubectl cluster-info 2>$null
    } catch {
        Write-Host "   ✗ kubectl not found or cluster not accessible" -ForegroundColor Red
        exit 1
    }
    
    Write-Host "   Namespace: $NamespaceName"
    Write-Host "   ConfigMap: api-gateway-config"
    Write-Host ""
    
    # Patch ConfigMap with new values
    Write-Host "   Patching ConfigMap..."
    
    $patchJson = @{
        data = @{
            "PRINT_AGENT_VERSION" = $manifest.version
            "PRINT_AGENT_SHA256" = $manifest.sha256
            "PRINT_AGENT_INSTALLER_URL" = $manifest.download_url
        }
    } | ConvertTo-Json -Compress
    
    try {
        kubectl patch configmap api-gateway-config `
            -n $NamespaceName `
            -p $patchJson `
            --type merge | Out-Null
        
        Write-Host "   ✓ ConfigMap updated"
    } catch {
        Write-Host "   ✗ Failed to patch ConfigMap: $_" -ForegroundColor Red
        exit 1
    }
    
    Write-Host ""
    Write-Host "📋 Kubernetes ConfigMap updated:"
    Write-Host "   PRINT_AGENT_VERSION=$($manifest.version)"
    Write-Host "   PRINT_AGENT_SHA256=$($manifest.sha256.Substring(0, 32))..."
    Write-Host "   PRINT_AGENT_INSTALLER_URL=$($manifest.download_url)"
    Write-Host ""
    Write-Host "⚠️  Next steps:"
    Write-Host "   1. Restart pod: kubectl rollout restart deployment/api-gateway -n $NamespaceName"
    Write-Host "   2. Monitor: kubectl logs -f deployment/api-gateway -n $NamespaceName"
    Write-Host "   3. Verify: kubectl exec <pod> -- curl http://localhost:8080/api/v1/public/print-agent/latest"
}

Write-Host ""
Write-Host "✅ Release deployment initiated!" -ForegroundColor Green
Write-Host ""

exit 0
