# NovaPanel Windows Files

This directory contains all Windows-specific files for NovaPanel.

## Available Files:

- **s-ui-windows.xml**: Windows Service configuration (compatibility filename)
- **install-windows.bat**: Installation script
- **s-ui-windows.bat**: Control panel (compatibility filename)
- **uninstall-windows.bat**: Uninstallation script
- **build-windows.bat**: Simple build script for CMD
- **build-windows.ps1**: Advanced build script for PowerShell

## Usage:

To install NovaPanel on Windows:
1. Run `install-windows.bat` as Administrator
2. Follow the installation wizard
3. Use `s-ui-windows.bat` for management (compatibility filename)

To build from source:
- With CMD: `build-windows.bat`
- With PowerShell: `.\build-windows.ps1`

Both build scripts automatically patch the current sing-box Windows interface compatibility issue before compiling.
They then mirror the release workflow by building with `CGO_ENABLED=0`, the same backend tags, and `-ldflags="-w -s -checklinkname=0"`.

The output is a `NovaPanel-windows\` directory that contains `sui.exe` plus the contents of `windows\`, matching the release package layout.
