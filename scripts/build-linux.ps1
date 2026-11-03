$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$go = (Get-Command go.exe -ErrorAction SilentlyContinue).Source
if (-not $go -and (Test-Path 'C:\Program Files\Go\bin\go.exe')) {
    $go = 'C:\Program Files\Go\bin\go.exe'
}
if (-not $go) { throw 'Go 1.27 or newer is required.' }
if (-not (Get-Command pnpm -ErrorAction SilentlyContinue)) { throw 'pnpm is required to build the web UI.' }

# Use a release tag only when HEAD is exactly on that tag; local builds use 'dev'.
$version = 'dev'
try {
    $tag = (git -C $repoRoot describe --tags --exact-match 2>$null)
    if ($tag) { $version = $tag.Trim() }
} catch {}
$buildCommit = (git -C $repoRoot rev-parse HEAD).Trim()
$buildTime = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
$sourceFiles = @((Get-ChildItem (Join-Path $repoRoot 'cmd'), (Join-Path $repoRoot 'internal/secretstore'), (Join-Path $repoRoot 'web/src'), (Join-Path $repoRoot 'web/public') -Recurse -File), (Get-Item (Join-Path $repoRoot 'go.mod')), (Get-Item (Join-Path $repoRoot 'web/pnpm-lock.yaml'))) | ForEach-Object { $_ } | Sort-Object FullName
$sourceText = ($sourceFiles | ForEach-Object { $_.FullName.Substring($repoRoot.Length) + ':' + (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash }) -join "`n"
$hasher = [System.Security.Cryptography.SHA256]::Create()
$buildSource = [Convert]::ToHexString($hasher.ComputeHash([Text.Encoding]::UTF8.GetBytes($sourceText))).ToLowerInvariant()
$hasher.Dispose()
Write-Host "Building version: $version ($buildCommit, source $buildSource)"

Push-Location $repoRoot
try {
    pnpm --dir web typecheck
    if ($LASTEXITCODE -ne 0) { throw 'Vue typecheck failed.' }
    pnpm --dir web build
    if ($LASTEXITCODE -ne 0) { throw 'Vue production build failed.' }

    New-Item -ItemType Directory -Force -Path (Join-Path $repoRoot 'bin') | Out-Null
    $oldGOOS = $env:GOOS
    $oldGOARCH = $env:GOARCH
    try {
        $env:GOOS = 'linux'
        $env:GOARCH = 'amd64'
        & $go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$version -X main.buildCommit=$buildCommit -X main.buildTime=$buildTime -X main.buildSource=$buildSource" -o (Join-Path $repoRoot 'bin/viya-linux-amd64') ./cmd/viya
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


