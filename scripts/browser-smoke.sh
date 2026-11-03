#!/bin/sh
set -eu
stage=$(mktemp -d)
panel_pid=''
cleanup() { if [ -n "$panel_pid" ]; then kill "$panel_pid" 2>/dev/null || true; wait "$panel_pid" 2>/dev/null || true; fi; rm -rf -- "$stage"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
mkdir -p output/playwright
go build -o "$stage/viya" ./cmd/viya
(cd build/caddy && go build -o "$stage/caddy" .)
export VIYA_LISTEN=127.0.0.1:18765 VIYA_CADDY_ADMIN=http://127.0.0.1:18766 VIYA_COOKIE_SECURE=false
export VIYA_DEFAULT_HTTP_PORT=18080 VIYA_DEFAULT_HTTPS_PORT=18443
export VIYA_DATA_DIR="$stage/data" VIYA_CADDY_BIN="$stage/caddy"
export XDG_DATA_HOME="$stage/data/.local/share" XDG_CONFIG_HOME="$stage/data/.config"
"$stage/viya" >output/playwright/server.log 2>&1 &
panel_pid=$!
python3 - <<'PY'
import json,time,urllib.request
for _ in range(60):
    try:
        with urllib.request.urlopen('http://127.0.0.1:18765/api/v1/health',timeout=1) as r:
            if json.load(r).get('caddy_ready'): break
    except Exception: pass
    time.sleep(1)
else: raise SystemExit('Smoke service did not become ready')
PY
npx --yes --package @playwright/cli@0.1.23 playwright-cli open --browser chrome http://127.0.0.1:18765
npx --yes --package @playwright/cli@0.1.23 playwright-cli snapshot
npx --yes --package @playwright/cli@0.1.23 playwright-cli run-code --filename scripts/browser-smoke.cjs
npx --yes --package @playwright/cli@0.1.23 playwright-cli close
