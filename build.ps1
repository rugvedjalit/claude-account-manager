# Cross-compiles claude-account for every supported platform into dist\.
# Needs Go 1.22+ (https://go.dev/dl). No other dependencies.
$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot 'go')
try {
    foreach ($t in @('windows/amd64', 'windows/arm64', 'linux/amd64', 'linux/arm64', 'darwin/amd64', 'darwin/arm64')) {
        $os, $arch = $t -split '/'
        $ext = if ($os -eq 'windows') { '.exe' } else { '' }
        $out = "..\dist\$os-$arch\claude-account$ext"
        $env:GOOS = $os; $env:GOARCH = $arch; $env:CGO_ENABLED = '0'
        go build -trimpath -ldflags="-s -w" -o $out .
        if ($LASTEXITCODE -ne 0) { throw "build failed for $t" }
        Write-Host "built $out"
    }
}
finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Pop-Location
}
