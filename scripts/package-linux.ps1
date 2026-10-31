$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
& (Join-Path $PSScriptRoot 'build-linux.ps1')
if ($LASTEXITCODE -ne 0) { throw 'Linux build failed.' }

$stamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$releaseRoot = Join-Path $repoRoot 'release'
$packageName = "viya-linux-amd64-$stamp"
$packageRoot = Join-Path $releaseRoot $packageName
New-Item -ItemType Directory -Force -Path (Join-Path $packageRoot 'bin'), (Join-Path $packageRoot 'deploy/systemd'), (Join-Path $packageRoot 'scripts'), (Join-Path $packageRoot 'docs') | Out-Null
Copy-Item (Join-Path $repoRoot 'bin/viya-linux-amd64') (Join-Path $packageRoot 'bin/viya-linux-amd64')
Copy-Item (Join-Path $repoRoot 'bin/caddy-linux-amd64') (Join-Path $packageRoot 'bin/caddy-linux-amd64')
Copy-Item (Join-Path $repoRoot 'deploy/install-debian.sh') (Join-Path $packageRoot 'deploy/install-debian.sh')
Copy-Item (Join-Path $repoRoot 'deploy/systemd/viya.service') (Join-Path $packageRoot 'deploy/systemd/viya.service')
Copy-Item (Join-Path $repoRoot 'scripts/backup-linux.sh') (Join-Path $packageRoot 'scripts/backup-linux.sh')
Copy-Item (Join-Path $repoRoot 'scripts/restore-linux.sh') (Join-Path $packageRoot 'scripts/restore-linux.sh')
Copy-Item (Join-Path $repoRoot 'README.md') (Join-Path $packageRoot 'README.md')
Copy-Item (Join-Path $repoRoot 'LICENSE') (Join-Path $packageRoot 'LICENSE')

$archive = Join-Path $releaseRoot "$packageName.tar.gz"
& tar.exe -czf $archive -C $releaseRoot $packageName
if ($LASTEXITCODE -ne 0) { throw 'Could not create the Linux release archive.' }
Write-Host "Created $archive"

