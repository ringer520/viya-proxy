$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$go = (Get-Command go.exe -ErrorAction SilentlyContinue).Source
if (-not $go -and (Test-Path 'C:\Program Files\Go\bin\go.exe')) {
    $go = 'C:\Program Files\Go\bin\go.exe'
}
if (-not $go) { throw 'Go 1.27 or newer is required.' }
if (-not (Get-Command pnpm -ErrorAction SilentlyContinue)) { throw 'pnpm is required to build the web UI.' }

# Inject version via ldflags; fall back to git describe or 'dev'.
$version = 'dev'
try {
    $tag = (git -C $repoRoot describe --tags --abbrev=0 2>$null)
    if ($tag) { $version = $tag.Trim() }
} catch {}
Write-Host "Building version: $version"

Push-Location $repoRoot
try {
    pnpm --dir web build
    if ($LASTEXITCODE -ne 0) { throw 'Vue production build failed.' }

    New-Item -ItemType Directory -Force -Path (Join-Path $repoRoot 'bin') | Out-Null
    $oldGOOS = $env:GOOS
    $oldGOARCH = $env:GOARCH
    try {
        $env:GOOS = 'linux'
        $env:GOARCH = 'amd64'
        & $go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$version" -o (Join-Path $repoRoot 'bin/viya-linux-amd64') ./cmd/viya
        if ($LASTEXITCODE -ne 0) { throw 'Linux Viya build failed.' }

        Push-Location (Join-Path $repoRoot 'build/caddy')
        try {
            & $go mod download
            if ($LASTEXITCODE -ne 0) { throw 'Caddy plugin dependencies could not be downloaded.' }
            & $go build -trimpath -buildvcs=false -o (Join-Path $repoRoot 'bin/caddy-linux-amd64') .
            if ($LASTEXITCODE -ne 0) { throw 'Linux Caddy build failed.' }
        }
        finally { Pop-Location }
    }
    finally {
        if ($null -eq $oldGOOS) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $oldGOOS }
        if ($null -eq $oldGOARCH) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $oldGOARCH }
    }
    Write-Host 'Built bin/viya-linux-amd64 and bin/caddy-linux-amd64.'
}
finally { Pop-Location }


