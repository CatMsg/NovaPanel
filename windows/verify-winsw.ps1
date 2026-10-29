[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Path,
    [switch]$DownloadIfMissing
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$script:WinSWUri = 'https://github.com/winsw/winsw/releases/download/v2.12.0/WinSW-x64.exe'
$script:WinSWSha256 = '05b82d46ad331cc16bdc00de5c6332c1ef818df8ceefcd49c726553209b3a0da'

function Ensure-NovaPanelWinSW {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [switch]$DownloadIfMissing
    )

    if (Test-Path -LiteralPath $Path -PathType Leaf) {
        $actual = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -eq $script:WinSWSha256) {
            return
        }
    }

    if (-not $DownloadIfMissing) {
        throw "WinSW is missing or failed SHA-256 verification: $Path"
    }

    $directory = Split-Path -Parent $Path
    if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
        throw "WinSW destination directory does not exist: $directory"
    }

    $temporaryPath = "$Path.download.$PID"
    try {
        Invoke-WebRequest -Uri $script:WinSWUri -OutFile $temporaryPath -UseBasicParsing
        $downloadHash = (Get-FileHash -LiteralPath $temporaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($downloadHash -ne $script:WinSWSha256) {
            throw "Downloaded WinSW SHA-256 mismatch: $downloadHash"
        }
        Move-Item -LiteralPath $temporaryPath -Destination $Path -Force
    }
    finally {
        if (Test-Path -LiteralPath $temporaryPath) {
            Remove-Item -LiteralPath $temporaryPath -Force
        }
    }

    $installedHash = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($installedHash -ne $script:WinSWSha256) {
        throw "Installed WinSW SHA-256 mismatch: $installedHash"
    }
}

if ($MyInvocation.InvocationName -ne '.') {
    try {
        Ensure-NovaPanelWinSW -Path $Path -DownloadIfMissing:$DownloadIfMissing
    }
    catch {
        Write-Error $_
        exit 1
    }
}
