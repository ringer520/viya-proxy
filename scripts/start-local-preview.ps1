param([switch]$Build)
$ErrorActionPreference='Stop'
$repoRoot=Split-Path -Parent $PSScriptRoot
$previewRoot=Join-Path $repoRoot '.preview'
New-Item -ItemType Directory -Force (Join-Path $previewRoot 'data'),(Join-Path $previewRoot 'secrets') | Out-Null
if($Build){
 Push-Location $repoRoot
 try {
  pnpm --dir web run typecheck
  if($LASTEXITCODE -ne 0){throw 'Frontend type check failed'}
  pnpm --dir web run build
  if($LASTEXITCODE -ne 0){throw 'Frontend build failed'}
  go build -trimpath -o bin/viya-preview.exe ./cmd/viya
  if($LASTEXITCODE -ne 0){throw 'Viya local build failed'}
  Push-Location (Join-Path $repoRoot 'build/caddy')
  try {go build -trimpath -o ../../bin/caddy-preview.exe .;if($LASTEXITCODE -ne 0){throw 'Caddy local build failed'}} finally {Pop-Location}
 }finally{Pop-Location}
}
$env:VIYA_LISTEN='127.0.0.1:18765'
$env:VIYA_DATA_DIR=Join-Path $previewRoot 'data'
$env:VIYA_MASTER_KEY_FILE=Join-Path $previewRoot 'secrets/master.key'
$env:VIYA_CADDY_BIN=Join-Path $repoRoot 'bin/caddy-preview.exe'
$env:VIYA_CADDY_ADMIN='http://127.0.0.1:18766'
$env:VIYA_DEFAULT_HTTPS_PORT='18443'
$env:VIYA_DEFAULT_HTTP_PORT='18080'
$env:XDG_DATA_HOME=Join-Path $previewRoot 'data/.local/share'
$env:XDG_CONFIG_HOME=Join-Path $previewRoot 'data/.config'
Write-Host 'Viya local review: http://127.0.0.1:18765'
Write-Host 'Independent preview data: .preview/data (no production connection)'
& (Join-Path $repoRoot 'bin/viya-preview.exe')
