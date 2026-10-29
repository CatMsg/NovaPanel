# NovaPanel Windows Files

This directory contains all Windows-specific files for NovaPanel.

## Available Files:

- **novas-windows.xml**: Windows Service configuration
- **install-windows.bat**: Installation script
- **novas-windows.bat**: Control panel
- **uninstall-windows.bat**: Uninstallation script
- **verify-winsw.ps1**: Verifies the pinned WinSW v2.12.0 service wrapper before installation
- **test-winsw-verifier.ps1**: Windows CI regression test for the pinned WinSW hash and installer batch behavior checks
- **build-windows.bat**: Simple build script for CMD
- **build-windows.ps1**: Advanced build script for PowerShell

## Usage:

To install NovaPanel on Windows:
1. Run `install-windows.bat` as Administrator
2. Follow the installation wizard
3. Use `novas-windows.bat` for management

The installer starts the service only after the verified WinSW executable is
registered to the NovaPanel installation path. If verification or registration
fails, it clearly reports that the service is unavailable and leaves manual
launch available from the control script. If saving panel settings or starting
an installed service fails, the installer returns a non-zero exit code instead
of reporting full success.

To build from source:
- With CMD: `build-windows.bat`
- With PowerShell: `.\build-windows.ps1`

Both build scripts verify the pinned local Sing-Box source and its upstream Windows
`MyInterfaces()` implementation before compiling. The check is read-only and never
modifies dependency source or the global module cache.
They then mirror the release workflow by building with `CGO_ENABLED=0`, the same backend tags, and `-ldflags="-w -s -checklinkname=0"`.

The output is a `NovaPanel-windows\` directory that contains `novas.exe` plus the contents of `windows\`, matching the release package layout.
