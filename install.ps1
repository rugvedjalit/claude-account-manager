#requires -Version 5.1
<#
.SYNOPSIS
    Installs (or removes) the Claude Account Manager binary for the current Windows user.

.DESCRIPTION
    Copies dist\windows-<arch>\claude-account.exe to %LOCALAPPDATA%\claude-account\bin\
    and adds that folder to the user's PATH. No admin rights needed. Nothing in the
    Claude Code installation or config is modified. Accounts saved by the older
    PowerShell version are picked up unchanged.

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\install.ps1
.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\install.ps1 -Uninstall
.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\install.ps1 -Uninstall -PurgeAccounts
#>
[CmdletBinding()]
param(
    [switch]$Uninstall,
    [switch]$PurgeAccounts
)

$ErrorActionPreference = 'Stop'
$Store = Join-Path $env:LOCALAPPDATA 'claude-account'
$Bin   = Join-Path $Store 'bin'
$Lib   = Join-Path $Store 'lib'   # used by the old PowerShell version

function Get-UserPath { [string][Environment]::GetEnvironmentVariable('Path', 'User') }
function Set-UserPath { param([string]$Value) [Environment]::SetEnvironmentVariable('Path', $Value, 'User') }
function Test-PathHas { param([string]$Dir) ((Get-UserPath) -split ';' | Where-Object { $_.TrimEnd('\') -ieq $Dir.TrimEnd('\') }).Count -gt 0 }

if ($Uninstall) {
    Write-Host 'Removing Claude Account Manager...'
    if (Test-PathHas $Bin) {
        $kept = (Get-UserPath) -split ';' | Where-Object { $_ -and ($_.TrimEnd('\') -ine $Bin.TrimEnd('\')) }
        Set-UserPath ($kept -join ';')
        Write-Host "  removed from user PATH: $Bin"
    }
    foreach ($d in @($Bin, $Lib, (Join-Path $Store 'tmp'))) { if (Test-Path $d) { Remove-Item -Recurse -Force $d; Write-Host "  deleted $d" } }
    if ($PurgeAccounts) {
        foreach ($d in @((Join-Path $Store 'accounts'), (Join-Path $Store 'backups'), (Join-Path $Store 'config.json'))) {
            if (Test-Path $d) { Remove-Item -Recurse -Force $d; Write-Host "  deleted $d" }
        }
        if ((Test-Path $Store) -and -not (Get-ChildItem -Force $Store)) { Remove-Item -Force $Store }
    }
    else {
        Write-Host "  saved accounts kept in $Store (re-run with -PurgeAccounts to delete them)"
    }
    Write-Host 'Done. Claude Code itself and its current login are untouched.'
    return
}

Write-Host 'Installing Claude Account Manager...'

$arch = if ($env:PROCESSOR_ARCHITECTURE -ieq 'ARM64') { 'arm64' } else { 'amd64' }
$src = $null
$downloaded = $null
# $PSScriptRoot is empty when the script is piped in with `irm ... | iex`.
if ($PSScriptRoot) { $local = Join-Path $PSScriptRoot "dist\windows-$arch\claude-account.exe"; if (Test-Path $local) { $src = $local } }
if (-not $src) {
    # No local build: download the binary for this platform from the latest GitHub release.
    $url = "https://github.com/rugvedjalit/claude-account-manager/releases/latest/download/claude-account-windows-$arch.exe"
    $downloaded = Join-Path ([IO.Path]::GetTempPath()) ("claude-account-" + [guid]::NewGuid().ToString('N') + '.exe')
    Write-Host "  downloading $url"
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
        $ProgressPreference = 'SilentlyContinue'
        Invoke-WebRequest -Uri $url -OutFile $downloaded -UseBasicParsing
    }
    catch { throw "Download failed ($($_.Exception.Message)). Check your connection or build from source with build.ps1." }
    $src = $downloaded
}

$claude = Get-Command claude -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($claude) {
    $ver = try { (& $claude.Source --version 2>$null | Out-String).Trim() } catch { 'unknown version' }
    Write-Host "  Claude Code: $ver ($($claude.Source))"
}
else {
    Write-Warning 'Claude Code (`claude`) was not found on PATH. Install it before running `claude-account setup`: https://code.claude.com/docs/en/setup'
}

New-Item -ItemType Directory -Force $Bin | Out-Null
# Remove the old PowerShell launcher so the .exe is the only `claude-account` on PATH.
foreach ($old in @((Join-Path $Bin 'claude-account.cmd'), (Join-Path $Lib 'claude-account.ps1'))) { if (Test-Path $old) { Remove-Item -Force $old } }
if ((Test-Path $Lib) -and -not (Get-ChildItem -Force $Lib)) { Remove-Item -Force $Lib }
Copy-Item $src (Join-Path $Bin 'claude-account.exe') -Force
if ($downloaded) { Remove-Item -Force $downloaded -ErrorAction SilentlyContinue }
Unblock-File (Join-Path $Bin 'claude-account.exe') -ErrorAction SilentlyContinue
Write-Host "  installed $Bin\claude-account.exe"

if (-not (Test-PathHas $Bin)) {
    $cur = Get-UserPath
    $new = if ([string]::IsNullOrWhiteSpace($cur)) { $Bin } else { $cur.TrimEnd(';') + ';' + $Bin }
    Set-UserPath $new
    Write-Host "  added to user PATH: $Bin"
}
else {
    Write-Host '  already on user PATH'
}

Write-Host ''
Write-Host 'Installed.' -ForegroundColor Green
Write-Host ''
Write-Host 'Open a NEW terminal (so PATH is reloaded), then:'
Write-Host '    claude-account setup      # one-time: save Account 1, add Account 2'
Write-Host '    claude-account status'
Write-Host '    claude-account switch'
Write-Host ''
Write-Host 'To use it in THIS terminal right away:'
Write-Host "    `$env:Path += `";$Bin`""
