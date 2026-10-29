$ErrorActionPreference = 'Stop'
$verifier = Join-Path $PSScriptRoot 'verify-winsw.ps1'
$temporaryDirectory = Join-Path ([System.IO.Path]::GetTempPath()) "novapanel-winsw-test-$PID"
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null

try {
    $downloadedPath = Join-Path $temporaryDirectory 'winsw.exe'
    . $verifier -Path $downloadedPath -DownloadIfMissing
    Ensure-NovaPanelWinSW -Path $downloadedPath -DownloadIfMissing

    $validCopy = Join-Path $temporaryDirectory 'winsw-valid-copy.exe'
    Copy-Item -LiteralPath $downloadedPath -Destination $validCopy
    Ensure-NovaPanelWinSW -Path $validCopy

    $corruptCopy = Join-Path $temporaryDirectory 'winsw-corrupt-copy.exe'
    [System.IO.File]::WriteAllBytes($corruptCopy, [byte[]](0, 1, 2, 3, 4))
    $before = (Get-FileHash -LiteralPath $corruptCopy -Algorithm SHA256).Hash
    $rejected = $false
    try {
        Ensure-NovaPanelWinSW -Path $corruptCopy
    }
    catch {
        $rejected = $true
    }
    if (-not $rejected) {
        throw 'Corrupt WinSW executable was accepted.'
    }
    $after = (Get-FileHash -LiteralPath $corruptCopy -Algorithm SHA256).Hash
    if ($after -ne $before) {
        throw 'Verifier modified the corrupt executable after rejecting it.'
    }

    $batchChecks = @(
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'copy /Y "novas\.exe" "%INSTALL_DIR%\\" >nul\s+if !errorLevel! neq 0'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'copy /Y "novas-windows\.xml" "%INSTALL_DIR%\\" >nul\s+if !errorLevel! neq 0'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'copy /Y "novas-windows\.bat" "%INSTALL_DIR%\\" >nul\s+if !errorLevel! neq 0'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'copy /Y "winsw\.exe" "novas-service\.exe" >nul'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'novas-service\.exe install\s+if !errorLevel! equ 0'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'novas-windows.bat')
            Pattern = 'novas-service\.exe install\s+if !errorLevel! equ 0'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'novas-windows.bat')
            Pattern = 'novas-service\.exe uninstall\s+if !errorLevel! equ 0'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'uninstall-windows.bat')
            Pattern = 'sc query %SERVICE_NAME% >nul 2>&1\s+set "SERVICE_QUERY_RESULT=!errorLevel!"\s+if not "!SERVICE_QUERY_RESULT!"=="0" if not "!SERVICE_QUERY_RESULT!"=="1060"'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'uninstall-windows.bat')
            Pattern = '"%INSTALL_DIR%\\novas-service\.exe" uninstall >nul 2>&1\s+sc query %SERVICE_NAME% >nul 2>&1\s+set "SERVICE_QUERY_RESULT=!errorLevel!"\s+if not "!SERVICE_QUERY_RESULT!"=="1060"'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'uninstall-windows.bat')
            Pattern = 'if not "!SERVICE_QUERY_RESULT!"=="1060" \([\s\S]*?No application files, user data, shortcuts, or firewall rules have been removed\.[\s\S]*?exit /b 1\s+\)\s+echo Service removed successfully\.[\s\S]*?REM Remove desktop shortcut'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'sc qc %SERVICE_NAME% \| find /i "%INSTALL_DIR%\\novas-service\.exe" >nul\s+if !errorLevel! equ 0 set "SERVICE_INSTALLED=1"'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'if "!SERVICE_INSTALLED!"=="1" \(\s+echo Starting NovaPanel service'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'novas\.exe setting -port %panel_port% -path "%panel_path%" -subPort %sub_port% -subPath "%sub_path%"\s+if !errorLevel! neq 0 \(\s+echo Error: Failed to save the panel and subscription settings\.\s+exit /b 1'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'net start %SERVICE_NAME%\s+if !errorLevel! equ 0 \(\s+set "SERVICE_RUNNING=1"'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'if "!SERVICE_RUNNING!"=="1" \(\s+echo Installation completed and the NovaPanel service is running\.'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'if "!SERVICE_INSTALLED!"=="1" \(\s+echo Installation files are ready, but the NovaPanel service did not start\.'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'echo Installation files are ready\. Windows service is unavailable; use manual launch\.'
        },
        @{
            Path = (Join-Path $PSScriptRoot 'install-windows.bat')
            Pattern = 'pause\s+exit /b !INSTALL_EXIT_CODE!'
        }
    )
    foreach ($check in $batchChecks) {
        $batch = Get-Content -LiteralPath $check.Path -Raw
        if ($batch -notmatch $check.Pattern) {
            throw "Batch service exit-code check is not safely delayed: $($check.Path)"
        }
    }

    Write-Output 'WinSW integrity and Windows batch service exit-code checks passed.'
}
finally {
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force
}
