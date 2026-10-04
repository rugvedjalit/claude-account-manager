#requires -Version 5.1
<#
.SYNOPSIS
    Installs (or removes) the Claude Account Manager for the current Windows user.

.DESCRIPTION
    Copies claude-account.ps1 to  %LOCALAPPDATA%\claude-account\lib\
    and   claude-account.cmd to  %LOCALAPPDATA%\claude-account\bin\
    then adds the bin folder to the user's PATH so `claude-account` works in any new terminal.
    No admin rights needed. Nothing in the Claude Code installation or config is modified.

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\install.ps1
.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\install.ps1 -Uninstall
.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\install.ps1 -Uninstall -PurgeAccounts   # also deletes saved (encrypted) accounts
#>
[CmdletBinding()]
param(
    [switch]$Uninstall,
    [switch]$PurgeAccounts
)

$ErrorActionPreference = 'Stop'
$Store = Join-Path $env:LOCALAPPDATA 'claude-account'
$Bin   = Join-Path $Store 'bin'
$Lib   = Join-Path $Store 'lib'

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

$src = $PSScriptRoot
foreach ($f in @('claude-account.ps1', 'claude-account.cmd')) {
    if (-not (Test-Path (Join-Path $src $f))) { throw "Missing $f next to install.ps1 ($src)." }
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
New-Item -ItemType Directory -Force $Lib | Out-Null
Copy-Item (Join-Path $src 'claude-account.ps1') (Join-Path $Lib 'claude-account.ps1') -Force
Copy-Item (Join-Path $src 'claude-account.cmd') (Join-Path $Bin 'claude-account.cmd') -Force
Get-ChildItem $Lib, $Bin -File | Unblock-File -ErrorAction SilentlyContinue
Write-Host "  installed to $Store"

if (-not (Test-PathHas $Bin)) {
    $cur = Get-UserPath
    $new = if ([string]::IsNullOrWhiteSpace($cur)) { $Bin } else { $cur.TrimEnd(';') + ';' + $Bin }
    Set-UserPath $new
    Write-Host "  added to user PATH: $Bin"
}
else {
    Write-Host '  already on user PATH'
}
if (($env:Path -split ';') -notcontains $Bin) { $env:Path = "$env:Path;$Bin" }

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
