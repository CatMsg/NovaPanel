@echo off
setlocal enabledelayedexpansion
set NOVAS_DEVELOPMENT_DEFAULT_ADMIN=

echo ========================================
echo NovaPanel Windows Installer
echo ========================================

REM Check if running as Administrator
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo Error: This script must be run as Administrator
    echo Right-click on this file and select "Run as administrator"
    pause
    exit /b 1
)

cd /d "%~dp0"
REM Set installation directory
set "INSTALL_DIR=C:\Program Files\novas"
set "SERVICE_NAME=novas"
set "SERVICE_RUNNING=0"
set "INSTALL_EXIT_CODE=0"

sc query novas >nul 2>&1
if %errorLevel% equ 0 (
    sc stop novas >nul 2>&1
    timeout /t 5 /nobreak >nul
    sc query novas | find "RUNNING" >nul
    if !errorLevel! equ 0 exit /b 1
)

echo Installing NovaPanel to: %INSTALL_DIR%

REM Create installation directory
if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"
if not exist "%INSTALL_DIR%\db" mkdir "%INSTALL_DIR%\db"
if not exist "%INSTALL_DIR%\logs" mkdir "%INSTALL_DIR%\logs"
if not exist "%INSTALL_DIR%\cert" mkdir "%INSTALL_DIR%\cert"

REM Keep program files readable, but protect database credentials and private keys.
icacls "%INSTALL_DIR%" /grant "*S-1-5-32-545:(OI)(CI)RX" /T >nul
if !errorLevel! neq 0 (
    echo Error: Failed to set installation directory permissions.
    exit /b 1
)
icacls "%INSTALL_DIR%\db" /inheritance:r /remove:g "*S-1-5-32-545" /grant:r "*S-1-5-18:(OI)(CI)F" "*S-1-5-32-544:(OI)(CI)F" /T >nul
if !errorLevel! neq 0 (
    echo Error: Failed to protect the database directory.
    exit /b 1
)
icacls "%INSTALL_DIR%\cert" /inheritance:r /remove:g "*S-1-5-32-545" /grant:r "*S-1-5-18:(OI)(CI)F" "*S-1-5-32-544:(OI)(CI)F" /T >nul
if !errorLevel! neq 0 (
    echo Error: Failed to protect the certificate directory.
    exit /b 1
)
icacls "%INSTALL_DIR%\logs" /inheritance:r /remove:g "*S-1-5-32-545" /grant:r "*S-1-5-18:(OI)(CI)F" "*S-1-5-32-544:(OI)(CI)F" /T >nul
if !errorLevel! neq 0 (
    echo Error: Failed to protect the log directory.
    exit /b 1
)

REM Copy files
echo Copying files...
copy /Y "novas.exe" "%INSTALL_DIR%\" >nul
if !errorLevel! neq 0 (
    echo Error: Failed to copy novas.exe to the installation directory.
    exit /b 1
)
copy /Y "novas-windows.xml" "%INSTALL_DIR%\" >nul
if !errorLevel! neq 0 (
    echo Error: Failed to copy the Windows service configuration.
    exit /b 1
)
copy /Y "novas-windows.bat" "%INSTALL_DIR%\" >nul
if !errorLevel! neq 0 (
    echo Error: Failed to copy the Windows management script.
    exit /b 1
)

REM Verify the pinned WinSW release before using or replacing it.
set "WINSW_PATH=%INSTALL_DIR%\winsw.exe"
set "WINSW_READY=0"
powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0verify-winsw.ps1" -Path "%WINSW_PATH%" -DownloadIfMissing
if !errorLevel! equ 0 (
    set "WINSW_READY=1"
) else (
    echo Warning: WinSW could not be verified or downloaded. Service installation will be skipped.
    echo See https://github.com/winsw/winsw/releases for the pinned release.
)

REM Install Windows Service
set "SERVICE_INSTALLED=0"
if "!WINSW_READY!"=="1" (
    echo Installing Windows Service...
    cd /d "%INSTALL_DIR%"
    copy /Y "winsw.exe" "novas-service.exe" >nul
    if !errorLevel! equ 0 (
        copy /Y "novas-windows.xml" "novas-service.xml" >nul
        if !errorLevel! equ 0 (
            REM Install service
            novas-service.exe install
            if !errorLevel! equ 0 (
                echo Service installed successfully
            ) else (
                echo Service installation command failed; checking whether it is already registered...
            )
            sc qc %SERVICE_NAME% | find /i "%INSTALL_DIR%\novas-service.exe" >nul
            if !errorLevel! equ 0 set "SERVICE_INSTALLED=1"
        ) else (
            echo Warning: Failed to install the verified service configuration.
        )
    ) else (
        echo Warning: Failed to copy the verified service wrapper.
    )
)

REM Validate the database before starting the service
cd /d "%INSTALL_DIR%"
novas.exe upgrade-db
if %errorLevel% neq 0 (
    echo Database upgrade preparation failed. Installation stopped without resetting settings.
    exit /b 1
)
novas.exe checkdb
if %errorLevel% neq 0 (
    echo Database validation failed. Installation stopped without resetting settings.
    exit /b 1
)

REM Get network configuration
echo.
echo ========================================
echo Network Configuration
echo ========================================

REM Get local IP addresses
echo Available IP addresses:
for /f "tokens=2 delims=:" %%i in ('ipconfig ^| findstr /i "IPv4"') do (
    echo   %%i
)

REM Get panel configuration
echo.
set /p panel_port="Enter panel port (default: 2095): "
if "%panel_port%"=="" set "panel_port=2095"

set /p panel_path="Enter panel path (default: /app/): "
if "%panel_path%"=="" set "panel_path=/app/"

set /p sub_port="Enter subscription port (default: 2096): "
if "%sub_port%"=="" set "sub_port=2096"

set /p sub_path="Enter subscription path (default: /sub/): "
if "%sub_path%"=="" set "sub_path=/sub/"

REM Apply settings
echo.
echo Applying settings...
cd /d "%INSTALL_DIR%"
novas.exe setting -port %panel_port% -path "%panel_path%" -subPort %sub_port% -subPath "%sub_path%"
if !errorLevel! neq 0 (
    echo Error: Failed to save the panel and subscription settings.
    exit /b 1
)

REM Get admin credentials
echo.
echo ========================================
echo Admin Configuration
echo ========================================

set /p admin_username="Enter admin username (default: admin): "
if "%admin_username%"=="" set "admin_username=admin"

REM Set admin credentials
echo Setting admin credentials...
novas.exe admin -username "%admin_username%" -password-prompt
if %errorLevel% neq 0 (
    echo Error: Failed to set admin credentials
    pause
    exit /b 1
)

REM Start service
if "!SERVICE_INSTALLED!"=="1" (
    echo Starting NovaPanel service...
    net start %SERVICE_NAME%
    if !errorLevel! equ 0 (
        set "SERVICE_RUNNING=1"
        echo Service started successfully
    ) else (
        sc query %SERVICE_NAME% | find "RUNNING" >nul
        if !errorLevel! equ 0 (
            set "SERVICE_RUNNING=1"
            echo Service is already running.
        ) else (
            set "INSTALL_EXIT_CODE=1"
            echo Warning: Failed to start the service. Check the Windows service logs.
        )
    )
) else (
    echo Warning: Windows service is not registered. Run novas-windows.bat and choose "Run NovaPanel Manually".
)

REM Create desktop shortcut
echo Creating desktop shortcut...
set "DESKTOP=%USERPROFILE%\Desktop"
if exist "%DESKTOP%" (
    powershell -Command "& {$WshShell = New-Object -comObject WScript.Shell; $Shortcut = $WshShell.CreateShortcut('%DESKTOP%\NovaPanel.lnk'); $Shortcut.TargetPath = '%INSTALL_DIR%\novas-windows.bat'; $Shortcut.WorkingDirectory = '%INSTALL_DIR%'; $Shortcut.Description = 'NovaPanel Control Panel'; $Shortcut.Save()}"
    echo Desktop shortcut created
)

REM Create Start Menu shortcut
echo Creating Start Menu shortcut...
set "START_MENU=%APPDATA%\Microsoft\Windows\Start Menu\Programs"
if exist "%START_MENU%" (
    if not exist "%START_MENU%\NovaPanel" mkdir "%START_MENU%\NovaPanel"
    powershell -Command "& {$WshShell = New-Object -comObject WScript.Shell; $Shortcut = $WshShell.CreateShortcut('%START_MENU%\NovaPanel\NovaPanel Control Panel.lnk'); $Shortcut.TargetPath = '%INSTALL_DIR%\novas-windows.bat'; $Shortcut.WorkingDirectory = '%INSTALL_DIR%'; $Shortcut.Description = 'NovaPanel Control Panel'; $Shortcut.Save()}"
    echo Start Menu shortcut created
)

REM Create environment variable
echo Setting environment variable...
setx NOVAS_HOME "%INSTALL_DIR%" /M >nul

REM Show final configuration
echo.
echo ========================================
if "!SERVICE_RUNNING!"=="1" (
    echo Installation completed and the NovaPanel service is running.
) else (
    if "!SERVICE_INSTALLED!"=="1" (
        echo Installation files are ready, but the NovaPanel service did not start.
    ) else (
        echo Installation files are ready. Windows service is unavailable; use manual launch.
    )
)
echo ========================================
echo.
echo NovaPanel has been installed to: %INSTALL_DIR%
echo.
echo Configuration:
echo   Panel Port: %panel_port%
echo   Panel Path: %panel_path%
echo   Subscription Port: %sub_port%
echo   Subscription Path: %sub_path%
echo   Admin Username: %admin_username%
echo.
echo Access URLs:
for /f "tokens=2 delims=:" %%i in ('ipconfig ^| findstr /i "IPv4"') do (
    set "ip=%%i"
    set "ip=!ip: =!"
    echo   Panel: http://!ip!:%panel_port%%panel_path%
    echo   Subscription: http://!ip!:%sub_port%%sub_path%
)
echo.
echo Service name: %SERVICE_NAME%
echo.
echo Useful commands:
echo   net start %SERVICE_NAME%    - Start the service
echo   net stop %SERVICE_NAME%     - Stop the service
echo   sc query %SERVICE_NAME%     - Check service status
echo.
echo You can also use the desktop shortcut or Start Menu item.
echo.
pause
exit /b !INSTALL_EXIT_CODE!
