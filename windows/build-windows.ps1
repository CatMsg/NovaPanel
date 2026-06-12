# PowerShell script for building NovaPanel on Windows
param(
    [string]$Architecture = "amd64",
    [switch]$Help
)

if ($Help) {
    Write-Host "Usage: .\build-windows.ps1 [-Architecture <arch>] [-Help]"
    Write-Host "Architectures: amd64, 386, arm64"
    Write-Host "Examples:"
    Write-Host "  .\build-windows.ps1                    # Build for amd64"
    Write-Host "  .\build-windows.ps1 -Architecture 386 # Build for 32-bit Windows"
    exit 0
}

Write-Host "Building NovaPanel for Windows ($Architecture)..." -ForegroundColor Green

# Check if Go is installed
try {
    $goVersion = go version 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Go not found"
    }
    Write-Host "Go version: $goVersion" -ForegroundColor Green
} catch {
    Write-Host "Error: Go is not installed or not in PATH" -ForegroundColor Red
    Write-Host "Please install Go from https://golang.org/dl/" -ForegroundColor Yellow
    Read-Host "Press Enter to exit"
    exit 1
}

# Check if Node.js is installed
try {
    $nodeVersion = node --version 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Node.js not found"
    }
    Write-Host "Node.js version: $nodeVersion" -ForegroundColor Green
} catch {
    Write-Host "Error: Node.js is not installed or not in PATH" -ForegroundColor Red
    Write-Host "Please install Node.js from https://nodejs.org/" -ForegroundColor Yellow
    Read-Host "Press Enter to exit"
    exit 1
}

# Build frontend
Write-Host "Building frontend..." -ForegroundColor Yellow
Push-Location frontend

try {
    Write-Host "Installing dependencies..." -ForegroundColor Cyan
    npm install
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to install frontend dependencies"
    }

    Write-Host "Building frontend..." -ForegroundColor Cyan
    npm run build
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to build frontend"
    }
} catch {
    Write-Host "Error: $_" -ForegroundColor Red
    Pop-Location
    Read-Host "Press Enter to exit"
    exit 1
}

Pop-Location

# Apply sing-box Windows compatibility patch before building the backend.
Write-Host "Applying sing-box Windows compatibility patch..." -ForegroundColor Yellow
go run .\scripts\patch-sing-box-windows.go
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Failed to patch sing-box compatibility" -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

# Create web/html directory
Write-Host "Creating web/html directory..." -ForegroundColor Yellow
if (!(Test-Path "web\html")) {
    New-Item -ItemType Directory -Path "web\html" -Force | Out-Null
}

# Copy frontend build files
Write-Host "Copying frontend build files..." -ForegroundColor Yellow
Copy-Item "frontend\dist\*" "web\html\" -Recurse -Force

# Build backend
Write-Host "Building backend..." -ForegroundColor Yellow

# Set environment variables
$env:GOOS = "windows"
$env:GOARCH = $Architecture
$env:CGO_ENABLED = "0"
Write-Host "Building with release-matched Windows settings (CGO disabled)..." -ForegroundColor Yellow

$buildCmd = "go build -ldflags `"-w -s -checklinkname=0`" -tags `"with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale`" -o sui.exe main.go"

try {
    Invoke-Expression $buildCmd
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to build backend"
    }
} catch {
    Write-Host "Error: $_" -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

if (Test-Path "NovaPanel-windows") {
    Remove-Item "NovaPanel-windows" -Recurse -Force
}

New-Item -ItemType Directory -Path "NovaPanel-windows" -Force | Out-Null

try {
    Copy-Item "sui.exe" "NovaPanel-windows\" -Force -ErrorAction Stop
    Copy-Item "windows\*" "NovaPanel-windows\" -Recurse -Force -ErrorAction Stop
} catch {
    Write-Host "Error: Failed to assemble Windows output directory" -ForegroundColor Red
    Write-Host $_ -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

Write-Host "Build completed successfully!" -ForegroundColor Green
Write-Host "Output: NovaPanel-windows\" -ForegroundColor Green

# Show file info
if (Test-Path "NovaPanel-windows\sui.exe") {
    $fileInfo = Get-Item "NovaPanel-windows\sui.exe"
    Write-Host "File size: $([math]::Round($fileInfo.Length / 1MB, 2)) MB" -ForegroundColor Cyan
    Write-Host "Created: $($fileInfo.CreationTime)" -ForegroundColor Cyan
}

Read-Host "Press Enter to exit"
