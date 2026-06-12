@echo off
setlocal enabledelayedexpansion

echo Building NovaPanel for Windows...

cd /d "%~dp0"

REM Check if Go is installed
go version >nul 2>&1
if errorlevel 1 (
    echo Error: Go is not installed or not in PATH
    echo Please install Go from https://golang.org/dl/
    pause
    exit /b 1
)

REM Check if Node.js is installed
node --version >nul 2>&1
if errorlevel 1 (
    echo Error: Node.js is not installed or not in PATH
    echo Please install Node.js from https://nodejs.org/
    pause
    exit /b 1
)

echo Building frontend...
cd frontend
call npm install
if errorlevel 1 (
    echo Error: Failed to install frontend dependencies
    pause
    exit /b 1
)

call npm run build
if errorlevel 1 (
    echo Error: Failed to build frontend
    pause
    exit /b 1
)

cd ..

echo Applying sing-box Windows compatibility patch...
go run .\scripts\patch-sing-box-windows.go
if errorlevel 1 (
    echo Error: Failed to patch sing-box compatibility
    pause
    exit /b 1
)

echo Creating web/html directory...
if not exist "web\html" mkdir "web\html"

echo Copying frontend build files...
xcopy "frontend\dist\*" "web\html\" /E /Y /Q

echo Building backend...
set "CGO_ENABLED=0"
set "GOOS=windows"
set "GOARCH=amd64"

go build -ldflags="-w -s -checklinkname=0" -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale" -o sui.exe main.go
if errorlevel 1 (
    echo Error: Failed to build backend
    pause
    exit /b 1
)

if exist "NovaPanel-windows" rmdir /s /q "NovaPanel-windows"
mkdir "NovaPanel-windows"

copy /y "sui.exe" "NovaPanel-windows\" >nul
if not exist "NovaPanel-windows\sui.exe" (
    echo Error: Failed to copy backend binary
    pause
    exit /b 1
)

xcopy "windows\*" "NovaPanel-windows\" /E /I /Y /Q >nul
if errorlevel 4 (
    echo Error: Failed to assemble Windows output directory
    pause
    exit /b 1
)

echo Build completed successfully!
echo Output: NovaPanel-windows\
pause
