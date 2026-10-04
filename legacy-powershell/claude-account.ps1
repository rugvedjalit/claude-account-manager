#requires -Version 5.1
<#
.SYNOPSIS
    Claude Account Manager - use several Claude accounts with the Claude Code CLI on Windows.

.DESCRIPTION
    Claude Code (native Windows build) keeps its login in two places:
        <config dir>\.credentials.json      OAuth access + refresh token (plaintext, by Claude Code's design)
        ~\.claude.json  ->  "oauthAccount"  account profile (email, org, uuids)
    Everything else (projects\, sessions, history.jsonl, settings) is account-independent.

    This tool keeps an encrypted (Windows DPAPI, current user) copy of each account's login state
    and swaps it into the live files on demand. Nothing else in the Claude Code config directory is
    touched, so `claude --resume` keeps seeing every session.

    Commands: setup | switch [n|name] | status | list | login <n> | remove <n> | rename <n> <name> | sync | test | help
#>
[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Argv
)

$ErrorActionPreference = 'Stop'
try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch { }
try { Add-Type -AssemblyName System.Security -ErrorAction Stop } catch { }

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
$Script:ToolVersion  = '1.0.0'
$Script:TestedClaude = '2.1.283'
$Script:MaxSlots     = 9
$Script:Check        = [string][char]0x2713
$Script:Cross        = [string][char]0x2717

$Script:StoreDir     = if ($env:CLAUDE_ACCOUNT_HOME) { $env:CLAUDE_ACCOUNT_HOME } else { Join-Path $env:LOCALAPPDATA 'claude-account' }
$Script:AccountsDir  = Join-Path $Script:StoreDir 'accounts'
$Script:BackupDir    = Join-Path $Script:StoreDir 'backups'
$Script:TmpRoot      = Join-Path $Script:StoreDir 'tmp'
$Script:ManifestPath = Join-Path $Script:StoreDir 'config.json'
$Script:Entropy      = [Text.Encoding]::UTF8.GetBytes('claude-account-manager/v1')

# Live Claude Code files (same rules Claude Code uses; cross-checked against `claude auth status`)
$Script:ClaudeConfigDir = if ($env:CLAUDE_CONFIG_DIR) { $env:CLAUDE_CONFIG_DIR } else { Join-Path $HOME '.claude' }
$Script:LiveCredPath    = Join-Path $Script:ClaudeConfigDir '.credentials.json'
$Script:LiveClaudeJson  = if ($env:CLAUDE_CONFIG_DIR) { Join-Path $env:CLAUDE_CONFIG_DIR '.claude.json' } else { Join-Path $HOME '.claude.json' }
$Script:ClaudeExe       = $null
$Script:ClaudeVersion   = $null

# ---------------------------------------------------------------------------
# Output helpers
# ---------------------------------------------------------------------------
function Write-Ok    { param([string]$Msg) Write-Host "$Script:Check $Msg" -ForegroundColor Green }
function Write-Warn  { param([string]$Msg) Write-Host "! $Msg" -ForegroundColor Yellow }
function Write-Fail  { param([string]$Msg) Write-Host "$Script:Cross $Msg" -ForegroundColor Red }
function Write-Dim   { param([string]$Msg) Write-Host $Msg -ForegroundColor DarkGray }
function Write-Title {
    param([string]$Text)
    Write-Host ''
    Write-Host $Text -ForegroundColor Cyan
    Write-Host ('-' * $Text.Length) -ForegroundColor Cyan
    Write-Host ''
}
function Stop-WithError {
    param([string]$Message, [string]$Hint = $null, [int]$Code = 1)
    Write-Host ''
    Write-Fail $Message
    if ($Hint) { Write-Host ''; Write-Host $Hint }
    Write-Host ''
    exit $Code
}
function Read-YesNo {
    param([string]$Prompt, [bool]$Default = $true)
    $suffix = if ($Default) { '[Y/n]' } else { '[y/N]' }
    while ($true) {
        $answer = Read-Host "$Prompt $suffix"
        if ([string]::IsNullOrWhiteSpace($answer)) { return $Default }
        switch -Regex ($answer.Trim().ToLower()) {
            '^(y|yes)$' { return $true }
            '^(n|no)$'  { return $false }
        }
    }
}
function Format-When {
    param($DateTime)
    if ($null -eq $DateTime) { return 'unknown' }
    $span = $DateTime - (Get-Date)
    $abs  = [Math]::Abs($span.TotalDays)
    $unit = if ($abs -ge 1) { ('{0:N1} days' -f $abs) } else { ('{0:N0} hours' -f [Math]::Abs($span.TotalHours)) }
    if ($span.TotalSeconds -lt 0) { return "expired $unit ago ($($DateTime.ToString('yyyy-MM-dd HH:mm')))" }
    return "valid for $unit (until $($DateTime.ToString('yyyy-MM-dd HH:mm')))"
}

# ---------------------------------------------------------------------------
# Claude Code detection / invocation
# ---------------------------------------------------------------------------
function Resolve-Claude {
    if ($Script:ClaudeExe) { return }
    $cmd = Get-Command claude -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $cmd) {
        Stop-WithError 'Claude Code is not installed (no `claude` executable found on PATH).' `
            "Install it first: https://code.claude.com/docs/en/setup`nThen open a new terminal and run this command again."
    }
    $Script:ClaudeExe = $cmd.Source
    $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    try   { $raw = (& $Script:ClaudeExe --version 2>$null | Out-String).Trim() }
    catch { $raw = '' }
    finally { $ErrorActionPreference = $prev }
    if ($raw -match '(\d+\.\d+\.\d+)') { $Script:ClaudeVersion = $Matches[1] } else { $Script:ClaudeVersion = 'unknown' }
}

function Invoke-Claude {
    # Runs the real Claude Code binary. Extra env vars apply to this process only while the
    # child runs, then are restored. Secrets are never passed on the command line.
    param([string[]]$Arguments, [hashtable]$Env = @{}, [switch]$Interactive)
    Resolve-Claude
    $saved = @{}
    foreach ($k in $Env.Keys) {
        $saved[$k] = [Environment]::GetEnvironmentVariable($k, 'Process')
        [Environment]::SetEnvironmentVariable($k, $Env[$k], 'Process')
    }
    $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    try {
        if ($Interactive) {
            # Hand the real console to Claude Code (no redirection), so its login URL,
            # code prompt and browser hand-off are visible and answerable.
            $p = Start-Process -FilePath $Script:ClaudeExe -ArgumentList $Arguments -NoNewWindow -Wait -PassThru
            return @{ ExitCode = $p.ExitCode; Output = $null }
        }
        $out = & $Script:ClaudeExe @Arguments 2>$null | Out-String
        return @{ ExitCode = $LASTEXITCODE; Output = $out }
    }
    finally {
        $ErrorActionPreference = $prev
        foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k], 'Process') }
    }
}

function Get-AuthStatus {
    # `claude auth status --json` (exit 1 + loggedIn:false when logged out). Local read, no network.
    param([string]$ConfigDir)
    $envs = @{}
    if ($ConfigDir) { $envs['CLAUDE_CONFIG_DIR'] = $ConfigDir }
    $r = Invoke-Claude -Arguments @('auth', 'status', '--json') -Env $envs
    if (-not $r.Output) { return $null }
    $text = $r.Output.Trim()
    $start = $text.IndexOf('{')
    if ($start -lt 0) { return $null }
    try { return ($text.Substring($start) | ConvertFrom-Json) } catch { return $null }
}

function Assert-ClaudeSupported {
    Resolve-Claude
    $r = Invoke-Claude -Arguments @('auth', '--help')
    if ($r.ExitCode -ne 0 -or -not $r.Output -or $r.Output -notmatch 'login' -or $r.Output -notmatch 'status') {
        Stop-WithError "Claude Code $($Script:ClaudeVersion) is not supported: it has no 'claude auth login/status' commands." `
            "Update Claude Code (tested with $($Script:TestedClaude)) and try again."
    }
    $st = Get-AuthStatus
    if ($st -and $st.PSObject.Properties['configDirectory'] -and $st.configDirectory) {
        $dir = [string]$st.configDirectory
        if ($dir.TrimEnd('\') -ne $Script:ClaudeConfigDir.TrimEnd('\')) {
            # Trust what the installed binary reports.
            $Script:ClaudeConfigDir = $dir
            $Script:LiveCredPath    = Join-Path $dir '.credentials.json'
            if ($env:CLAUDE_CONFIG_DIR) { $Script:LiveClaudeJson = Join-Path $dir '.claude.json' }
        }
    }
    if ($Script:ClaudeVersion -ne 'unknown') {
        try {
            $v = [version]$Script:ClaudeVersion; $t = [version]$Script:TestedClaude
            if ($v.Major -ne $t.Major) {
                Write-Warn "Claude Code $($Script:ClaudeVersion) detected; this tool was verified against $($Script:TestedClaude). The credential layout may differ."
            }
        } catch { }
    }
}

function Test-ClaudeRunning {
    return @(Get-Process -Name 'claude' -ErrorAction SilentlyContinue)
}

# ---------------------------------------------------------------------------
# JSON surgery on ~\.claude.json
# Only the top-level "oauthAccount" member is ever replaced; the rest of the file is
# preserved byte-for-byte (no deserialize/re-serialize round trip).
# ---------------------------------------------------------------------------
function Get-JsonValueEnd {
    param([string]$Text, [int]$Start)
    $n = $Text.Length
    $c = $Text[$Start]
    if ($c -eq '"') {
        $i = $Start + 1; $esc = $false
        while ($i -lt $n) {
            $ch = $Text[$i]
            if ($esc) { $esc = $false }
            elseif ($ch -eq '\') { $esc = $true }
            elseif ($ch -eq '"') { return $i + 1 }
            $i++
        }
        throw 'Malformed JSON: unterminated string.'
    }
    if ($c -eq '{' -or $c -eq '[') {
        $i = $Start; $depth = 0; $inStr = $false; $esc = $false
        while ($i -lt $n) {
            $ch = $Text[$i]
            if ($inStr) {
                if ($esc) { $esc = $false }
                elseif ($ch -eq '\') { $esc = $true }
                elseif ($ch -eq '"') { $inStr = $false }
            }
            else {
                if ($ch -eq '"') { $inStr = $true }
                elseif ($ch -eq '{' -or $ch -eq '[') { $depth++ }
                elseif ($ch -eq '}' -or $ch -eq ']') { $depth--; if ($depth -eq 0) { return $i + 1 } }
            }
            $i++
        }
        throw 'Malformed JSON: unbalanced brackets.'
    }
    # scalar (number / true / false / null)
    $i = $Start
    while ($i -lt $n) {
        $ch = $Text[$i]
        if ($ch -eq ',' -or $ch -eq '}' -or $ch -eq ']' -or [char]::IsWhiteSpace($ch)) { return $i }
        $i++
    }
    return $n
}

function Find-JsonTopLevelMember {
    # Returns @{ KeyStart; ValueStart; ValueEnd } for a depth-1 key, or $null.
    param([string]$Text, [string]$Key)
    $n = $Text.Length; $i = 0; $depth = 0; $inStr = $false; $esc = $false; $tokenStart = -1
    while ($i -lt $n) {
        $c = $Text[$i]
        if ($inStr) {
            if ($esc) { $esc = $false }
            elseif ($c -eq '\') { $esc = $true }
            elseif ($c -eq '"') {
                $inStr = $false
                if ($depth -eq 1) {
                    $str = $Text.Substring($tokenStart + 1, $i - $tokenStart - 1)
                    if ($str -eq $Key) {
                        $j = $i + 1
                        while ($j -lt $n -and [char]::IsWhiteSpace($Text[$j])) { $j++ }
                        if ($j -lt $n -and $Text[$j] -eq ':') {
                            $k = $j + 1
                            while ($k -lt $n -and [char]::IsWhiteSpace($Text[$k])) { $k++ }
                            $end = Get-JsonValueEnd -Text $Text -Start $k
                            return @{ KeyStart = $tokenStart; ValueStart = $k; ValueEnd = $end }
                        }
                    }
                }
            }
            $i++; continue
        }
        if ($c -eq '"') { $inStr = $true; $tokenStart = $i }
        elseif ($c -eq '{' -or $c -eq '[') { $depth++ }
        elseif ($c -eq '}' -or $c -eq ']') { $depth-- }
        $i++
    }
    return $null
}

function Set-JsonTopLevelMember {
    param([string]$Text, [string]$Key, [string]$ValueJson)
    $m = Find-JsonTopLevelMember -Text $Text -Key $Key
    if ($m) {
        return $Text.Substring(0, $m.ValueStart) + $ValueJson + $Text.Substring($m.ValueEnd)
    }
    $open = $Text.IndexOf('{')
    if ($open -lt 0) { return "{`n  `"$Key`": $ValueJson`n}`n" }
    $j = $open + 1
    while ($j -lt $Text.Length -and [char]::IsWhiteSpace($Text[$j])) { $j++ }
    $sep = if ($j -lt $Text.Length -and $Text[$j] -eq '}') { '' } else { ',' }
    return $Text.Substring(0, $open + 1) + "`n  `"$Key`": $ValueJson$sep" + $Text.Substring($open + 1)
}

function Write-FileAtomic {
    param([string]$Path, [string]$Text)
    $dir = Split-Path -Parent $Path
    if ($dir -and -not (Test-Path $dir)) { New-Item -ItemType Directory -Force $dir | Out-Null }
    $tmp = "$Path.cam-tmp"
    [IO.File]::WriteAllText($tmp, $Text, (New-Object Text.UTF8Encoding($false)))
    if (Test-Path $Path) {
        try { [IO.File]::Replace($tmp, $Path, [NullString]::Value) }
        catch { [IO.File]::Copy($tmp, $Path, $true); Remove-Item -Force $tmp -ErrorAction SilentlyContinue }
    }
    else { Move-Item -Force $tmp $Path }
}

function Backup-LiveFile {
    param([string]$Path, [string]$Tag)
    if (-not (Test-Path $Path)) { return }
    New-Item -ItemType Directory -Force $Script:BackupDir | Out-Null
    $name = Split-Path -Leaf $Path
    $dest = Join-Path $Script:BackupDir ("{0}.{1}.{2}" -f $name, (Get-Date).ToString('yyyyMMdd-HHmmss'), $Tag)
    Copy-Item $Path $dest -Force
    Get-ChildItem $Script:BackupDir -Filter "$name.*" | Sort-Object LastWriteTime -Descending | Select-Object -Skip 10 | Remove-Item -Force -ErrorAction SilentlyContinue
}

# ---------------------------------------------------------------------------
# Credential parsing / live state
# ---------------------------------------------------------------------------
function ConvertFrom-EpochMs {
    param($Ms)
    if ($null -eq $Ms) { return $null }
    try { return [DateTimeOffset]::FromUnixTimeMilliseconds([long]$Ms).LocalDateTime } catch { return $null }
}

function Get-CredentialInfo {
    # Validates the Claude Code credential file layout and extracts non-secret metadata.
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return $null }
    try { $o = $Text | ConvertFrom-Json } catch { return $null }
    if ($null -eq $o) { return $null }
    $oauth = $o.PSObject.Properties['claudeAiOauth']
    if (-not $oauth -or -not $oauth.Value) { return $null }
    $v = $oauth.Value
    if (-not $v.PSObject.Properties['accessToken'] -or -not $v.PSObject.Properties['refreshToken']) { return $null }
    $sub = $null;  if ($v.PSObject.Properties['subscriptionType'])      { $sub  = $v.subscriptionType }
    $exp = $null;  if ($v.PSObject.Properties['expiresAt'])             { $exp  = ConvertFrom-EpochMs $v.expiresAt }
    $rexp = $null; if ($v.PSObject.Properties['refreshTokenExpiresAt']) { $rexp = ConvertFrom-EpochMs $v.refreshTokenExpiresAt }
    return @{ SubscriptionType = $sub; AccessExpires = $exp; RefreshExpires = $rexp }
}

function Get-OAuthProfile {
    param([string]$Json)
    if ([string]::IsNullOrWhiteSpace($Json) -or $Json.Trim() -eq 'null') { return $null }
    try { $o = $Json | ConvertFrom-Json } catch { return $null }
    if ($null -eq $o) { return $null }
    $get = { param($n) if ($o.PSObject.Properties[$n]) { $o.$n } else { $null } }
    return @{
        AccountUuid = & $get 'accountUuid'
        Email       = & $get 'emailAddress'
        OrgName     = & $get 'organizationName'
        OrgUuid     = & $get 'organizationUuid'
        DisplayName = & $get 'displayName'
    }
}

function Get-IdentityKey {
    param($Profile)
    if ($null -eq $Profile) { return $null }
    if ($Profile.AccountUuid) { return [string]$Profile.AccountUuid }
    if ($Profile.Email) { return ('email:' + ([string]$Profile.Email).ToLower()) }
    return $null
}

function Get-LiveState {
    $st = @{ HasCredentials = $false; CredentialsText = $null; CredInfo = $null
             OAuthAccountJson = $null; Profile = $null; Identity = $null; Email = $null }
    if (Test-Path $Script:LiveCredPath) {
        $st.CredentialsText = [IO.File]::ReadAllText($Script:LiveCredPath)
        $st.CredInfo = Get-CredentialInfo $st.CredentialsText
        $st.HasCredentials = ($null -ne $st.CredInfo)
    }
    if (Test-Path $Script:LiveClaudeJson) {
        $text = [IO.File]::ReadAllText($Script:LiveClaudeJson)
        $m = Find-JsonTopLevelMember -Text $text -Key 'oauthAccount'
        if ($m) {
            $st.OAuthAccountJson = $text.Substring($m.ValueStart, $m.ValueEnd - $m.ValueStart)
            $st.Profile = Get-OAuthProfile $st.OAuthAccountJson
        }
    }
    $st.Identity = Get-IdentityKey $st.Profile
    if ($st.Profile) { $st.Email = $st.Profile.Email }
    return $st
}

# ---------------------------------------------------------------------------
# Encrypted store (DPAPI, current user) + manifest
# ---------------------------------------------------------------------------
function Protect-Text {
    param([string]$Text)
    $bytes = [Text.Encoding]::UTF8.GetBytes($Text)
    return [Security.Cryptography.ProtectedData]::Protect($bytes, $Script:Entropy, [Security.Cryptography.DataProtectionScope]::CurrentUser)
}
function Unprotect-Text {
    param([byte[]]$Bytes)
    $plain = [Security.Cryptography.ProtectedData]::Unprotect($Bytes, $Script:Entropy, [Security.Cryptography.DataProtectionScope]::CurrentUser)
    return [Text.Encoding]::UTF8.GetString($plain)
}
function Get-SlotPath { param([int]$Slot) Join-Path $Script:AccountsDir ("account-{0}.dpapi" -f $Slot) }

function New-Manifest { return [pscustomobject]@{ version = 1; active = $null; accounts = [pscustomobject]@{} } }

function Read-Manifest {
    if (-not (Test-Path $Script:ManifestPath)) { return (New-Manifest) }
    try { $m = [IO.File]::ReadAllText($Script:ManifestPath) | ConvertFrom-Json }
    catch { Stop-WithError "The account manager config is corrupt: $($Script:ManifestPath)" 'Delete it and run: claude-account setup' }
    if (-not $m.PSObject.Properties['accounts'] -or $null -eq $m.accounts) { $m | Add-Member -MemberType NoteProperty -Name accounts -Value ([pscustomobject]@{}) -Force }
    if (-not $m.PSObject.Properties['active']) { $m | Add-Member -MemberType NoteProperty -Name active -Value $null -Force }
    return $m
}
function Save-Manifest {
    param($Manifest)
    New-Item -ItemType Directory -Force $Script:StoreDir | Out-Null
    Write-FileAtomic -Path $Script:ManifestPath -Text ($Manifest | ConvertTo-Json -Depth 6)
}
function Get-Slots {
    param($Manifest)
    $out = @()
    foreach ($p in $Manifest.accounts.PSObject.Properties) {
        $n = 0
        if ([int]::TryParse($p.Name, [ref]$n)) { $out += $n }
    }
    return @($out | Sort-Object)
}
function Get-SlotEntry {
    param($Manifest, [int]$Slot)
    $p = $Manifest.accounts.PSObject.Properties[[string]$Slot]
    if ($p) { return $p.Value } else { return $null }
}
function Get-SlotName {
    param($Manifest, [int]$Slot)
    $e = Get-SlotEntry $Manifest $Slot
    if ($e -and $e.PSObject.Properties['name'] -and $e.name) { return [string]$e.name }
    return "Account $Slot"
}
function Find-SlotByIdentity {
    param($Manifest, [string]$Identity)
    if (-not $Identity) { return $null }
    foreach ($s in (Get-Slots $Manifest)) {
        $e = Get-SlotEntry $Manifest $s
        if ($e.PSObject.Properties['identity'] -and [string]$e.identity -eq $Identity) { return $s }
    }
    return $null
}
function Resolve-SlotArg {
    # Accepts a slot number or an account name (case-insensitive).
    param($Manifest, [string]$Arg)
    $n = 0
    if ([int]::TryParse($Arg, [ref]$n)) { return $n }
    foreach ($s in (Get-Slots $Manifest)) {
        if ((Get-SlotName $Manifest $s).ToLower() -eq $Arg.ToLower()) { return $s }
    }
    return $null
}
function Get-NextFreeSlot {
    param($Manifest)
    $used = Get-Slots $Manifest
    for ($i = 1; $i -le $Script:MaxSlots; $i++) { if ($used -notcontains $i) { return $i } }
    return $null
}

function Save-Slot {
    param($Manifest, [int]$Slot, [string]$CredentialsText, [string]$OAuthAccountJson, [string]$Name)
    $info = Get-CredentialInfo $CredentialsText
    if (-not $info) { throw 'Credential data is not in the expected Claude Code format (claudeAiOauth.accessToken/refreshToken).' }
    $profile = Get-OAuthProfile $OAuthAccountJson
    $payload = [ordered]@{
        format           = 1
        credentials      = $CredentialsText
        oauthAccountJson = $OAuthAccountJson
        savedAt          = (Get-Date).ToString('o')
    }
    New-Item -ItemType Directory -Force $Script:AccountsDir | Out-Null
    [IO.File]::WriteAllBytes((Get-SlotPath $Slot), (Protect-Text ($payload | ConvertTo-Json -Compress)))

    $existing = Get-SlotEntry $Manifest $Slot
    $entry = [ordered]@{
        name             = if ($Name) { $Name } elseif ($existing -and $existing.PSObject.Properties['name']) { $existing.name } else { "Account $Slot" }
        identity         = Get-IdentityKey $profile
        email            = if ($profile) { $profile.Email } else { $null }
        orgName          = if ($profile) { $profile.OrgName } else { $null }
        subscriptionType = $info.SubscriptionType
        savedAt          = if ($existing -and $existing.PSObject.Properties['savedAt']) { $existing.savedAt } else { (Get-Date).ToString('o') }
        updatedAt        = (Get-Date).ToString('o')
    }
    $Manifest.accounts | Add-Member -MemberType NoteProperty -Name ([string]$Slot) -Value ([pscustomobject]$entry) -Force
    Save-Manifest $Manifest
}

function Read-Slot {
    param([int]$Slot)
    $path = Get-SlotPath $Slot
    if (-not (Test-Path $path)) { throw "No saved credentials for account $Slot (missing $path)." }
    try { $json = Unprotect-Text ([IO.File]::ReadAllBytes($path)) }
    catch { throw "Cannot decrypt the saved credentials for account $Slot. DPAPI data can only be read by the Windows user who created it. Run: claude-account login $Slot" }
    $o = $json | ConvertFrom-Json
    return @{ CredentialsText = $o.credentials; OAuthAccountJson = $o.oauthAccountJson; SavedAt = $o.savedAt
              CredInfo = (Get-CredentialInfo $o.credentials); Profile = (Get-OAuthProfile $o.oauthAccountJson) }
}

function Remove-Slot {
    param($Manifest, [int]$Slot)
    $path = Get-SlotPath $Slot
    if (Test-Path $path) {
        try { [IO.File]::WriteAllBytes($path, (New-Object byte[] ((Get-Item $path).Length))) } catch { }
        Remove-Item -Force $path
    }
    $Manifest.accounts.PSObject.Properties.Remove([string]$Slot)
    if ($Manifest.active -eq $Slot) { $Manifest.active = $null }
    Save-Manifest $Manifest
}

# ---------------------------------------------------------------------------
# Core operations
# ---------------------------------------------------------------------------
function Sync-LiveToStore {
    # Copies the live login (which Claude Code may have refreshed) back into its slot.
    param($Manifest, $Live, [switch]$Quiet)
    if (-not $Live.HasCredentials -or -not $Live.Identity) { return $null }
    $slot = Find-SlotByIdentity $Manifest $Live.Identity
    if ($null -eq $slot) {
        if (-not $Quiet) { Write-Warn "The current Claude Code login ($($Live.Email)) is not a saved account, so it was not saved. Run 'claude-account setup' to add it." }
        return $null
    }
    Save-Slot -Manifest $Manifest -Slot $slot -CredentialsText $Live.CredentialsText -OAuthAccountJson $Live.OAuthAccountJson
    return $slot
}

function Set-LiveFromSlot {
    param([int]$Slot)
    $blob = Read-Slot $Slot
    Backup-LiveFile -Path $Script:LiveClaudeJson -Tag 'pre-switch'
    Backup-LiveFile -Path $Script:LiveCredPath  -Tag 'pre-switch'
    Write-FileAtomic -Path $Script:LiveCredPath -Text $blob.CredentialsText
    if ($blob.OAuthAccountJson) {
        $text = if (Test-Path $Script:LiveClaudeJson) { [IO.File]::ReadAllText($Script:LiveClaudeJson) } else { "{`n}`n" }
        $new  = Set-JsonTopLevelMember -Text $text -Key 'oauthAccount' -ValueJson $blob.OAuthAccountJson
        $null = $new | ConvertFrom-Json   # must still parse before we commit it
        Write-FileAtomic -Path $Script:LiveClaudeJson -Text $new
    }
    return $blob
}

function Invoke-IsolatedLogin {
    # Runs `claude auth login` against a throw-away CLAUDE_CONFIG_DIR so the current live login
    # is never logged out. Returns the captured credential text + oauthAccount JSON.
    param([string]$Email)
    New-Item -ItemType Directory -Force $Script:TmpRoot | Out-Null
    $dir  = Join-Path $Script:TmpRoot ('login-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force $dir | Out-Null
    $cred = Join-Path $dir '.credentials.json'
    $cj   = Join-Path $dir '.claude.json'
    try {
        $args = @('auth', 'login')
        if ($Email) { $args += @('--email', $Email) }
        $r = Invoke-Claude -Arguments $args -Env @{ CLAUDE_CONFIG_DIR = $dir } -Interactive
        if ($r.ExitCode -ne 0) { throw "Claude Code login did not complete (exit code $($r.ExitCode))." }
        if (-not (Test-Path $cred)) { throw 'Login finished but Claude Code wrote no credentials file.' }
        $credText = [IO.File]::ReadAllText($cred)
        if (-not (Get-CredentialInfo $credText)) { throw 'Claude Code wrote credentials in an unrecognized format.' }
        $oauthJson = $null
        if (Test-Path $cj) {
            $text = [IO.File]::ReadAllText($cj)
            $m = Find-JsonTopLevelMember -Text $text -Key 'oauthAccount'
            if ($m) { $oauthJson = $text.Substring($m.ValueStart, $m.ValueEnd - $m.ValueStart) }
        }
        if (-not (Get-OAuthProfile $oauthJson)) {
            # Fallback: build a minimal profile from `claude auth status`.
            $st = Get-AuthStatus -ConfigDir $dir
            if ($st -and $st.PSObject.Properties['email'] -and $st.email) {
                $oauthJson = ([ordered]@{ emailAddress = $st.email; organizationUuid = $st.orgId; organizationName = $st.orgName } | ConvertTo-Json -Compress)
            }
        }
        return @{ CredentialsText = $credText; OAuthAccountJson = $oauthJson }
    }
    finally {
        try { if (Test-Path $cred) { [IO.File]::WriteAllText($cred, '') } } catch { }
        try { Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue } catch { }
    }
}

function Assert-NotRunning {
    param([switch]$Force)
    if ($env:CLAUDECODE -and -not $Force) {
        Stop-WithError 'You are running this from inside a Claude Code session.' `
            "Exit Claude Code first, then run the command from a normal terminal.`n(Use --force to override.)"
    }
    $procs = Test-ClaudeRunning
    if ($procs.Count -gt 0 -and -not $Force) {
        Write-Warn ("Claude Code appears to be running (PID {0})." -f (($procs | ForEach-Object { $_.Id }) -join ', '))
        Write-Host  '  A running session keeps its old login in memory and may write it back to disk when it refreshes its token.'
        if (-not (Read-YesNo '  Switch anyway?' $false)) { Write-Host ''; Write-Host 'Aborted. Exit Claude Code and try again.'; exit 2 }
    }
}

# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------
function Write-HelpSection { param([string]$Text) Write-Host ''; Write-Host $Text -ForegroundColor Yellow }
function Write-HelpRow {
    param([string]$Command, [string]$Description, [string]$Note)
    Write-Host '  ' -NoNewline
    Write-Host $Command.PadRight(34) -ForegroundColor Cyan -NoNewline
    Write-Host $Description -NoNewline
    if ($Note) { Write-Host "  $Note" -ForegroundColor DarkGray -NoNewline }
    Write-Host ''
}

function Show-Help {
    Write-Title "Claude Account Manager $($Script:ToolVersion)"

    # Current state at a glance (never fails the help screen).
    try {
        $manifest = Read-Manifest
        $slots = @(Get-Slots $manifest)
        $live = Get-LiveState
        $current = Find-SlotByIdentity $manifest $live.Identity
        Write-Host 'Right now:  ' -NoNewline
        if ($null -ne $current)      { Write-Host "$(Get-SlotName $manifest $current) ($($live.Email))" -ForegroundColor Green -NoNewline }
        elseif ($live.HasCredentials) { Write-Host "$($live.Email) - not saved yet, run: claude-account save" -ForegroundColor Yellow -NoNewline }
        else                          { Write-Host 'not logged in' -ForegroundColor Yellow -NoNewline }
        Write-Host "   |   Saved accounts: $($slots.Count)" -ForegroundColor DarkGray
        if ($slots.Count -gt 0) {
            foreach ($s in $slots) {
                $e = Get-SlotEntry $manifest $s
                $mark = if ($s -eq $current) { '*' } else { ' ' }
                Write-Host ("            {0} [{1}] {2,-14} {3}" -f $mark, $s, (Get-SlotName $manifest $s), $e.email) -ForegroundColor DarkGray
            }
        }
    } catch { }

    Write-HelpSection 'EVERY DAY'
    Write-HelpRow 'claude'                    'Start Claude Code with the active account' '(unchanged)'
    Write-HelpRow 'claude-account switch'     'Pick another account from a menu'           '(exit Claude Code first)'
    Write-HelpRow 'claude-account switch 2'   'Switch straight to account 2 (or a name)'
    Write-HelpRow 'claude --resume'           'Continue the same conversation on the new account'

    Write-HelpSection 'ADD / REMOVE ACCOUNTS'
    Write-HelpRow 'claude-account setup'      'First-time setup, or add another account via browser login'
    Write-HelpRow 'claude-account save'       'Add the account Claude Code is logged in as right now' '(after /login)'
    Write-HelpRow 'claude-account remove 2'   'Forget account 2'
    Write-HelpRow 'claude-account rename 2 Personal' 'Name an account; then: claude-account switch Personal'

    Write-HelpSection 'CHECK / FIX'
    Write-HelpRow 'claude-account status'     'Active account, saved accounts, when each login expires'
    Write-HelpRow 'claude-account list'       'Short list of saved accounts'
    Write-HelpRow 'claude-account login 2'    'Log account 2 in again' '(when status says EXPIRED)'
    Write-HelpRow 'claude-account test'       'Send one tiny prompt to prove the active account works'

    Write-HelpSection 'OPTIONS'
    Write-HelpRow '--force   (or -y)'         'Skip confirmation questions'
    Write-HelpRow '--email you@example.com'   'Pre-fill the email on the login page' '(setup, login)'

    Write-HelpSection 'EXAMPLE: account 1 hits its usage limit'
    Write-Host '  1. Exit Claude Code'
    Write-Host '  2. ' -NoNewline; Write-Host 'claude-account switch' -ForegroundColor Cyan -NoNewline; Write-Host '   and pick 2'
    Write-Host '  3. ' -NoNewline; Write-Host 'claude --resume' -ForegroundColor Cyan -NoNewline;       Write-Host '         same conversation, now on account 2'
    Write-Host ''
}

function Invoke-Setup {
    param([hashtable]$Flags)
    Assert-ClaudeSupported
    Write-Title 'Claude Account Manager - Setup'
    Write-Host "Claude Code $($Script:ClaudeVersion) detected at $($Script:ClaudeExe)"
    Write-Host "Config directory: $($Script:ClaudeConfigDir)"
    Write-Host ''

    $manifest = Read-Manifest
    $slots = @(Get-Slots $manifest)
    $live = Get-LiveState

    if ($slots.Count -eq 0) {
        Write-Host 'Step 1: Account 1' -ForegroundColor White
        if ($live.HasCredentials -and $live.Identity) {
            Write-Host "  You are currently logged in to Claude Code as $($live.Email)."
            if (Read-YesNo '  Save this login as Account 1?' $true) {
                Save-Slot -Manifest $manifest -Slot 1 -CredentialsText $live.CredentialsText -OAuthAccountJson $live.OAuthAccountJson
                $manifest.active = 1; Save-Manifest $manifest
                Write-Ok "Account 1 saved ($($live.Email))"
            }
            else {
                Write-Host ''
                Write-Host '  A browser window will open. Sign in with the account you want as Account 1.'
                Write-Host '  Your current Claude Code login will be replaced by it.'
                Read-Host '  Press Enter to continue' | Out-Null
                $cap = Invoke-IsolatedLogin -Email $Flags.Email
                Save-Slot -Manifest $manifest -Slot 1 -CredentialsText $cap.CredentialsText -OAuthAccountJson $cap.OAuthAccountJson
                Set-LiveFromSlot 1 | Out-Null
                $manifest.active = 1; Save-Manifest $manifest
                Write-Ok "Account 1 saved and activated ($((Get-OAuthProfile $cap.OAuthAccountJson).Email))"
            }
        }
        else {
            Write-Host '  No Claude Code login found. Starting the normal Claude Code login flow...'
            Write-Host '  (a browser window will open; sign in with your FIRST account)'
            Write-Host ''
            $r = Invoke-Claude -Arguments @('auth', 'login') -Interactive
            if ($r.ExitCode -ne 0) { Stop-WithError 'Claude Code login did not complete.' 'Run: claude-account setup   to try again.' }
            $live = Get-LiveState
            if (-not $live.HasCredentials) { Stop-WithError 'Login finished but no credentials were written by Claude Code.' "Expected: $($Script:LiveCredPath)" }
            Save-Slot -Manifest $manifest -Slot 1 -CredentialsText $live.CredentialsText -OAuthAccountJson $live.OAuthAccountJson
            $manifest.active = 1; Save-Manifest $manifest
            Write-Ok "Account 1 saved ($($live.Email))"
        }
        Write-Host ''
    }
    else {
        Write-Host 'Already configured:'
        foreach ($s in $slots) { $e = Get-SlotEntry $manifest $s; Write-Host ("  [{0}] {1}  {2}" -f $s, (Get-SlotName $manifest $s), $e.email) }
        Write-Host ''
        # Keep the saved copy of the live login fresh while we are here, or adopt it if it is new.
        if ($live.HasCredentials -and $live.Identity) {
            if ($null -eq (Sync-LiveToStore -Manifest $manifest -Live $live -Quiet)) {
                Save-LiveAsNewSlot -Manifest $manifest -Live $live -Flags $Flags | Out-Null
                Write-Host ''
            }
        }
    }

    $first = $true
    while ($true) {
        $next = Get-NextFreeSlot $manifest
        if ($null -eq $next) { Write-Warn "All $($Script:MaxSlots) account slots are used."; break }
        $default = $first -and ((Get-Slots $manifest).Count -lt 2)
        if (-not (Read-YesNo 'Do you want to add another Claude account?' $default)) { break }
        $first = $false
        Write-Host ''
        Write-Host "Step: Account $next" -ForegroundColor White
        Write-Host '  A browser window will open for the Claude login flow.'
        Write-Host '  Sign in with the OTHER account. If the browser is already signed in to claude.ai'
        Write-Host '  with an account you have saved, use a private/incognito window or sign out there first.'
        Write-Host '  Your current Claude Code login is NOT touched by this step.'
        Read-Host '  Press Enter to open the login' | Out-Null
        Write-Host ''
        try { $cap = Invoke-IsolatedLogin -Email $Flags.Email }
        catch { Write-Fail $_.Exception.Message; Write-Host ''; continue }
        $prof = Get-OAuthProfile $cap.OAuthAccountJson
        $id   = Get-IdentityKey $prof
        $dup  = Find-SlotByIdentity $manifest $id
        if ($null -ne $dup) {
            Save-Slot -Manifest $manifest -Slot $dup -CredentialsText $cap.CredentialsText -OAuthAccountJson $cap.OAuthAccountJson
            Write-Warn "That is the same account as $(Get-SlotName $manifest $dup) ($($prof.Email)). Its saved login was refreshed instead of adding a new account."
        }
        else {
            Save-Slot -Manifest $manifest -Slot $next -CredentialsText $cap.CredentialsText -OAuthAccountJson $cap.OAuthAccountJson
            Write-Ok "Account $next saved ($($prof.Email))"
        }
        Write-Host ''
    }

    Write-Host ''
    Write-Ok 'Setup complete.'
    Write-Host ''
    Show-Status -Brief
    Write-Host ''
    Write-Host 'Next: just run `claude`. To change account: claude-account switch'
}

function Invoke-Switch {
    param([string[]]$Positional, [hashtable]$Flags)
    Assert-ClaudeSupported
    $manifest = Read-Manifest
    $slots = @(Get-Slots $manifest)
    if ($slots.Count -eq 0) { Stop-WithError 'No accounts are configured.' "Run:`n`n  claude-account setup`n`nto save your accounts." }

    $live = Get-LiveState
    $current = Find-SlotByIdentity $manifest $live.Identity
    $currentLabel = if ($null -ne $current) { Get-SlotName $manifest $current } elseif ($live.HasCredentials) { "(unsaved login: $($live.Email))" } else { '(not logged in)' }

    $target = $null
    if ($Positional.Count -ge 1) {
        $target = Resolve-SlotArg $manifest $Positional[0]
        if ($null -eq $target) { Stop-WithError "Unknown account '$($Positional[0])'." 'Run: claude-account list' }
    }
    else {
        Write-Title 'Claude Account Manager'
        Write-Host "Current account: $currentLabel"
        Write-Host ''
        foreach ($s in $slots) {
            $e = Get-SlotEntry $manifest $s
            $mark = if ($s -eq $current) { ' (active)' } else { '' }
            Write-Host ("[{0}] {1}  {2}{3}" -f $s, (Get-SlotName $manifest $s), $e.email, $mark)
        }
        Write-Host ''
        $answer = Read-Host 'Select account'
        if ([string]::IsNullOrWhiteSpace($answer)) { Write-Host 'Cancelled.'; exit 0 }
        $target = Resolve-SlotArg $manifest $answer.Trim()
        if ($null -eq $target) { Stop-WithError "Unknown account '$answer'." }
    }

    if ($slots -notcontains $target) {
        Stop-WithError "Account $target is not configured." "Run:`n`n  claude-account setup`n`nto add another account."
    }
    $name = Get-SlotName $manifest $target

    if ($current -eq $target) {
        Sync-LiveToStore -Manifest $manifest -Live $live -Quiet | Out-Null
        $manifest.active = $target; Save-Manifest $manifest
        Write-Host ''
        Write-Ok "$name is already the active account ($($live.Email))."
        exit 0
    }

    Assert-NotRunning -Force:([bool]$Flags.Force)

    # 1. Save the outgoing login (Claude Code may have refreshed its tokens since we stored it).
    Sync-LiveToStore -Manifest $manifest -Live $live | Out-Null

    # 2. Check the incoming login before touching anything.
    try { $blob = Read-Slot $target } catch { Stop-WithError $_.Exception.Message }
    if ($blob.CredInfo.RefreshExpires -and $blob.CredInfo.RefreshExpires -lt (Get-Date)) {
        Stop-WithError "$name's saved login has expired (refresh token $(Format-When $blob.CredInfo.RefreshExpires))." `
            "Re-authenticate it with:`n`n  claude-account login $target"
    }

    # 3. Swap.
    try { Set-LiveFromSlot $target | Out-Null }
    catch { Stop-WithError "Switching failed: $($_.Exception.Message)" "Backups of the previous files are in:`n  $($Script:BackupDir)" }
    $manifest.active = $target; Save-Manifest $manifest

    # 4. Verify with Claude Code itself.
    $st = Get-AuthStatus
    $expected = if ($blob.Profile) { [string]$blob.Profile.Email } else { $null }
    $stEmail = if ($st -and $st.PSObject.Properties['email']) { [string]$st.email } else { '' }
    $ok = $st -and $st.loggedIn -and (-not $expected -or $stEmail.ToLower() -eq $expected.ToLower())
    Write-Host ''
    if ($ok) {
        Write-Ok "Switched to $name ($stEmail)"
        if ($blob.CredInfo.AccessExpires -and $blob.CredInfo.AccessExpires -lt (Get-Date)) {
            Write-Dim '  (access token is expired; Claude Code refreshes it automatically on next start)'
        }
    }
    else {
        $seen = if ($st) { "loggedIn=$($st.loggedIn) email=$stEmail" } else { 'no status output' }
        Stop-WithError "Files were switched to $name but Claude Code does not report it as logged in ($seen)." `
            "Try:`n`n  claude-account login $target`n`nBackups of the previous files are in:`n  $($Script:BackupDir)"
    }
}

function Show-Status {
    param([switch]$Brief)
    if (-not $Brief) { Assert-ClaudeSupported; Write-Title 'Claude Account Manager' }
    $manifest = Read-Manifest
    $slots = @(Get-Slots $manifest)
    $live = Get-LiveState
    $current = Find-SlotByIdentity $manifest $live.Identity

    if ($null -ne $current) {
        Write-Host "Active account: $(Get-SlotName $manifest $current) ($($live.Email))"
        if (-not $Brief) { Sync-LiveToStore -Manifest $manifest -Live $live -Quiet | Out-Null }
    }
    elseif ($live.HasCredentials) { Write-Host "Active account: unsaved login ($($live.Email)) - run 'claude-account save' to add it" -ForegroundColor Yellow }
    else { Write-Host 'Active account: none (Claude Code is logged out)' -ForegroundColor Yellow }
    Write-Host ''

    if ($slots.Count -eq 0) { Write-Host 'No accounts configured. Run: claude-account setup' -ForegroundColor Yellow }
    foreach ($s in $slots) {
        $e = Get-SlotEntry $manifest $s
        $name = Get-SlotName $manifest $s
        $state = 'configured'
        $detail = ''
        try {
            $blob = Read-Slot $s
            $rx = $blob.CredInfo.RefreshExpires
            if ($rx) {
                if ($rx -lt (Get-Date)) { $state = 'EXPIRED'; $detail = "login expired $($rx.ToString('yyyy-MM-dd')) - run: claude-account login $s" }
                elseif ($rx -lt (Get-Date).AddDays(3)) { $detail = "refresh token $(Format-When $rx) - use it soon or run: claude-account login $s" }
                else { $detail = "refresh token $(Format-When $rx)" }
            }
        }
        catch { $state = 'UNREADABLE'; $detail = $_.Exception.Message }
        $active = if ($s -eq $current) { '  <- active' } else { '' }
        $sub = if ($e.subscriptionType) { ", $($e.subscriptionType)" } else { '' }
        Write-Host ("{0}: {1}  ({2}{3}){4}" -f $name, $state, $e.email, $sub, $active)
        if ($detail -and -not $Brief) { Write-Dim "    $detail" }
    }
    Write-Host ''
    if ($Script:ClaudeExe) { Write-Host "Claude Code: detected ($($Script:ClaudeVersion), $($Script:ClaudeExe))" }
    if (-not $Brief) {
        Write-Dim "Live credentials: $($Script:LiveCredPath)"
        Write-Dim "Encrypted store:  $($Script:AccountsDir)"
    }
}

function Show-List {
    $manifest = Read-Manifest
    $live = Get-LiveState
    $current = Find-SlotByIdentity $manifest $live.Identity
    $slots = @(Get-Slots $manifest)
    if ($slots.Count -eq 0) { Write-Host 'No accounts configured. Run: claude-account setup'; return }
    foreach ($s in $slots) {
        $e = Get-SlotEntry $manifest $s
        $mark = if ($s -eq $current) { '*' } else { ' ' }
        Write-Host ("{0} [{1}] {2,-16} {3}" -f $mark, $s, (Get-SlotName $manifest $s), $e.email)
    }
}

function Invoke-Login {
    param([string[]]$Positional, [hashtable]$Flags)
    Assert-ClaudeSupported
    $manifest = Read-Manifest
    if ($Positional.Count -lt 1) { Stop-WithError 'Usage: claude-account login <n> [--email address]' }
    $slot = Resolve-SlotArg $manifest $Positional[0]
    if ($null -eq $slot -or $slot -lt 1 -or $slot -gt $Script:MaxSlots) { Stop-WithError "Unknown account '$($Positional[0])'." }
    $entry = Get-SlotEntry $manifest $slot
    $name = Get-SlotName $manifest $slot
    $live = Get-LiveState
    $isActive = ($null -ne $live.Identity) -and $entry -and $entry.PSObject.Properties['identity'] -and ([string]$entry.identity -eq $live.Identity)

    Write-Title "Re-authenticate $name"
    if ($entry -and $entry.email) { Write-Host "  Sign in as $($entry.email) in the browser window that opens." }
    else { Write-Host "  Sign in with the account you want to save as $name." }
    Write-Host '  If the browser is signed in to a different Claude account, use a private window.'
    Read-Host '  Press Enter to open the login' | Out-Null
    $email = if ($Flags.Email) { $Flags.Email } elseif ($entry -and $entry.email) { [string]$entry.email } else { $null }
    try { $cap = Invoke-IsolatedLogin -Email $email } catch { Stop-WithError $_.Exception.Message }
    $prof = Get-OAuthProfile $cap.OAuthAccountJson
    $id = Get-IdentityKey $prof
    if ($entry -and $entry.PSObject.Properties['identity'] -and $entry.identity -and $id -ne [string]$entry.identity -and -not $Flags.Force) {
        Stop-WithError "You signed in as $($prof.Email), but $name is $($entry.email)." "Sign in with the right account, or use --force to replace $name with $($prof.Email)."
    }
    $other = Find-SlotByIdentity $manifest $id
    if ($null -ne $other -and $other -ne $slot) { Stop-WithError "$($prof.Email) is already saved as $(Get-SlotName $manifest $other)." }
    Save-Slot -Manifest $manifest -Slot $slot -CredentialsText $cap.CredentialsText -OAuthAccountJson $cap.OAuthAccountJson
    Write-Ok "$name re-authenticated ($($prof.Email))"
    if ($isActive) {
        Assert-NotRunning -Force:([bool]$Flags.Force)
        Set-LiveFromSlot $slot | Out-Null
        $manifest.active = $slot; Save-Manifest $manifest
        Write-Ok "Live Claude Code login updated to the new $name credentials"
    }
}

function Invoke-Remove {
    param([string[]]$Positional, [hashtable]$Flags)
    $manifest = Read-Manifest
    if ($Positional.Count -lt 1) { Stop-WithError 'Usage: claude-account remove <n>' }
    $slot = Resolve-SlotArg $manifest $Positional[0]
    if ($null -eq $slot -or (Get-Slots $manifest) -notcontains $slot) { Stop-WithError "Account '$($Positional[0])' is not configured." 'Run: claude-account list' }
    $name = Get-SlotName $manifest $slot
    $e = Get-SlotEntry $manifest $slot
    if (-not $Flags.Force -and -not (Read-YesNo "Forget $name ($($e.email))?" $false)) { Write-Host 'Cancelled.'; return }
    Remove-Slot -Manifest $manifest -Slot $slot
    Write-Ok "$name removed from the account manager."
    $live = Get-LiveState
    if ($live.Identity -and $e.PSObject.Properties['identity'] -and [string]$e.identity -eq $live.Identity) {
        Write-Dim '  Claude Code itself is still logged in with it. Use `claude auth logout` if you also want that gone.'
    }
}

function Invoke-Rename {
    param([string[]]$Positional)
    $manifest = Read-Manifest
    if ($Positional.Count -lt 2) { Stop-WithError 'Usage: claude-account rename <n> <new name>' }
    $slot = Resolve-SlotArg $manifest $Positional[0]
    if ($null -eq $slot -or (Get-Slots $manifest) -notcontains $slot) { Stop-WithError "Account '$($Positional[0])' is not configured." }
    $newName = ($Positional[1..($Positional.Count - 1)] -join ' ').Trim()
    if (-not $newName) { Stop-WithError 'The new name cannot be empty.' }
    $n = 0
    if ([int]::TryParse($newName, [ref]$n)) { Stop-WithError 'The name cannot be just a number (it would clash with slot numbers).' }
    $old = Get-SlotName $manifest $slot
    $entry = Get-SlotEntry $manifest $slot
    $entry | Add-Member -MemberType NoteProperty -Name name -Value $newName -Force
    Save-Manifest $manifest
    Write-Ok "Renamed '$old' to '$newName'"
}

function Save-LiveAsNewSlot {
    # Adopts the current Claude Code login (signed in outside this tool) into the next free slot.
    param($Manifest, $Live, [hashtable]$Flags)
    $next = Get-NextFreeSlot $Manifest
    if ($null -eq $next) { Stop-WithError "All $($Script:MaxSlots) account slots are used." 'Remove one with: claude-account remove <n>' }
    Write-Host "Claude Code is currently logged in as $($Live.Email), which is not a saved account."
    if (-not $Flags.Force -and -not (Read-YesNo "Save it as Account $next?" $true)) { Write-Host 'Not saved.'; return $null }
    Save-Slot -Manifest $Manifest -Slot $next -CredentialsText $Live.CredentialsText -OAuthAccountJson $Live.OAuthAccountJson
    $Manifest.active = $next; Save-Manifest $Manifest
    Write-Ok "Account $next saved ($($Live.Email)) and marked active"
    return $next
}

function Invoke-Sync {
    param([hashtable]$Flags)
    Assert-ClaudeSupported
    $manifest = Read-Manifest
    $live = Get-LiveState
    if (-not $live.HasCredentials -or -not $live.Identity) { Stop-WithError 'Claude Code is not logged in; nothing to save.' 'Run: claude auth login   or   claude-account switch' }
    $slot = Sync-LiveToStore -Manifest $manifest -Live $live -Quiet
    if ($null -ne $slot) { Write-Ok "Saved the live login into $(Get-SlotName $manifest $slot) ($($live.Email))"; return }
    if ($null -eq (Save-LiveAsNewSlot -Manifest $manifest -Live $live -Flags $Flags)) { exit 1 }
}

function Invoke-Test {
    Assert-ClaudeSupported
    $manifest = Read-Manifest
    $live = Get-LiveState
    $current = Find-SlotByIdentity $manifest $live.Identity
    $label = if ($null -ne $current) { Get-SlotName $manifest $current } else { 'the current login' }
    if (-not $live.HasCredentials) { Stop-WithError 'Claude Code is not logged in.' 'Run: claude-account switch   or   claude-account setup' }
    Write-Host "Sending a one-line test prompt as $label ($($live.Email))..."
    $r = Invoke-Claude -Arguments @('-p', 'Reply with the single word OK and nothing else.', '--max-turns', '1')
    $out = if ($r.Output) { $r.Output.Trim() } else { '' }
    if ($r.ExitCode -eq 0 -and $out) { Write-Ok "$label works. Reply: $out" }
    else {
        Stop-WithError "$label did not work (exit code $($r.ExitCode))." `
            "If the message above mentions authentication or an expired token, re-authenticate with:`n`n  claude-account login $current`n`nIf it mentions a usage limit, switch to another account:`n`n  claude-account switch"
    }
}

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
function Main {
    param([string[]]$Argv)
    $flags = @{ Force = $false; Email = $null }
    $positional = @()
    for ($i = 0; $i -lt $Argv.Count; $i++) {
        $a = $Argv[$i]
        if ($a -match '^(--force|-f|--yes|-y)$') { $flags.Force = $true; continue }
        if ($a -match '^--email=(.+)$') { $flags.Email = $Matches[1]; continue }
        if ($a -eq '--email' -and $i + 1 -lt $Argv.Count) { $flags.Email = $Argv[$i + 1]; $i++; continue }
        if ($a -match '^(-h|--help|/\?)$') { Show-Help; exit 0 }
        $positional += $a
    }
    $cmd = if ($positional.Count -gt 0) { $positional[0].ToLower() } else { 'help' }
    $rest = @(); if ($positional.Count -gt 1) { $rest = @($positional[1..($positional.Count - 1)]) }

    switch ($cmd) {
        'setup'   { Invoke-Setup  -Flags $flags }
        'add'     { Invoke-Setup  -Flags $flags }
        'switch'  { Invoke-Switch -Positional $rest -Flags $flags }
        'use'     { Invoke-Switch -Positional $rest -Flags $flags }
        'status'  { Show-Status }
        'list'    { Show-List }
        'ls'      { Show-List }
        'login'   { Invoke-Login  -Positional $rest -Flags $flags }
        'remove'  { Invoke-Remove -Positional $rest -Flags $flags }
        'rm'      { Invoke-Remove -Positional $rest -Flags $flags }
        'rename'  { Invoke-Rename -Positional $rest }
        'sync'    { Invoke-Sync -Flags $flags }
        'save'    { Invoke-Sync -Flags $flags }
        'test'    { Invoke-Test }
        'version' { Write-Host "claude-account $($Script:ToolVersion)" }
        'help'    { Show-Help }
        default   { Write-Fail "Unknown command '$cmd'."; Show-Help; exit 1 }
    }
}

if (-not $env:CLAUDE_ACCOUNT_NO_MAIN) {
    if ($null -eq $Argv) { $Argv = @() }
    try { Main -Argv $Argv }
    catch {
        Write-Host ''
        Write-Fail $_.Exception.Message
        if ($env:CLAUDE_ACCOUNT_DEBUG) { Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray }
        exit 1
    }
}
