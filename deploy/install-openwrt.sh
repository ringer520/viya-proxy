#!/bin/sh
# OpenWrt/procd installer. Data and master keys remain on persistent storage.
set -eu
[ "$(id -u)" -eq 0 ] || { echo 'Run as root.' >&2; exit 1; }
[ -r /etc/openwrt_release ] && [ -x /sbin/procd ] && command -v uci >/dev/null 2>&1 || { echo 'This installer requires OpenWrt with procd and UCI.' >&2; exit 1; }
case "$(uname -m)" in x86_64|amd64) ARCH=amd64;;aarch64|arm64) ARCH=arm64;;armv7l|armv7) ARCH=armv7;;*) echo 'Supported: amd64, arm64, armv7. MIPS is not supported.' >&2;exit 1;;esac
for tool in tar sha256sum awk df du; do command -v "$tool" >/dev/null 2>&1 || { echo "Missing tool: $tool" >&2;exit 1; };done
if [ ! -s /etc/ssl/certs/ca-certificates.crt ] && [ ! -s /etc/ssl/cert.pem ];then
 echo 'Install HTTPS certificates first: opkg update && opkg install ca-bundle (or apk add ca-certificates on apk-based OpenWrt).' >&2;exit 1
fi
BASE=https://github.com/ringer520/viya-proxy/releases/latest/download
[ -z "${VERSION:-}" ] || BASE="https://github.com/ringer520/viya-proxy/releases/download/$VERSION"
STAGE=$(mktemp -d /tmp/viya-install.XXXXXX)
BACKUP='';CHANGED=0;WAS_RUNNING=0;SUCCESS=0;STOPPED=0;HAD_SERVICE=0
cleanup(){
 result=$?
 if [ "$CHANGED" -eq 1 ] && [ "$SUCCESS" -eq 0 ];then
  /etc/init.d/viya stop || true
  for file in viya caddy;do [ ! -f "$BACKUP/$file" ] || { cp "$BACKUP/$file" "/opt/viya/bin/$file.rollback"; chmod 755 "/opt/viya/bin/$file.rollback"; mv -f "/opt/viya/bin/$file.rollback" "/opt/viya/bin/$file"; };done
  if [ -d "$BACKUP/data" ];then
   [ ! -d "$DATA_DIR" ] || mv "$DATA_DIR" "$BACKUP/failed-data"
   cp -a "$BACKUP/data" "$DATA_DIR"
  fi
  echo "Installation failed. Previous files restored where available. Backup: $BACKUP" >&2
 fi
 if [ "$SUCCESS" -eq 0 ];then
  if [ "$HAD_SERVICE" -eq 0 ] && [ "$CHANGED" -eq 1 ];then /etc/init.d/viya disable || true;fi
  if [ "$STOPPED" -eq 1 ] && [ "$WAS_RUNNING" -eq 1 ];then /etc/init.d/viya start || true;fi
 fi
 rm -rf "$STAGE"
 exit "$result"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
download(){ if command -v curl >/dev/null 2>&1;then curl -fL --retry 2 --connect-timeout 15 --max-time 600 --progress-bar "$1" -o "$2";elif command -v wget >/dev/null 2>&1;then wget -O "$2" "$1";else echo 'Install curl or wget first.' >&2;exit 1;fi; }
PKG=viya-linux-$ARCH
DOWNLOAD_FREE=$(df -Pk "$STAGE" | awk 'END{print $4}')
[ "$DOWNLOAD_FREE" -ge 131072 ] || { echo 'At least 128 MiB free in /tmp is required for download and extraction.' >&2;exit 1; }
echo "==> Downloading $PKG"
download "$BASE/$PKG.tar.gz" "$STAGE/$PKG.tar.gz"
download "$BASE/sha256sums.txt" "$STAGE/sha256sums.txt"
EXPECTED=$(awk -v f="$PKG.tar.gz" '$2==f{print $1;exit}' "$STAGE/sha256sums.txt")
[ "${#EXPECTED}" -eq 64 ] || { echo 'Release checksum missing.' >&2;exit 1; }
ACTUAL=$(sha256sum "$STAGE/$PKG.tar.gz" | awk '{print $1}')
[ "$EXPECTED" = "$ACTUAL" ] || { echo 'Checksum mismatch; installation cancelled.' >&2;exit 1; }
tar -tzf "$STAGE/$PKG.tar.gz" | awk -v p="$PKG/" 'index($0,p)!=1||$0~/(^|\/)\.\.(\/|$)/{bad=1} END{exit bad}' || { echo 'Invalid archive paths.' >&2;exit 1; }
tar -xzf "$STAGE/$PKG.tar.gz" -C "$STAGE"
for file in "bin/viya-linux-$ARCH" "bin/caddy-linux-$ARCH" deploy/openwrt/viya;do [ -f "$STAGE/$PKG/$file" ] && [ ! -L "$STAGE/$PKG/$file" ] || { echo "Package file missing/invalid: $file" >&2;exit 1; };done
DATA_DIR=$(uci -q get viya.main.data_dir || true)
[ -n "$DATA_DIR" ] || DATA_DIR=$(sed -n 's/^DATA_DIR=\(\/[-A-Za-z0-9_/.]*\)$/\1/p' /etc/init.d/viya 2>/dev/null || true)
DATA_DIR=${DATA_DIR:-/opt/viya/data}
case "$DATA_DIR" in */../*|*/..|*/./*|*/.) echo 'Invalid data path.' >&2;exit 1;;esac
case "$DATA_DIR" in /opt/viya/*|/mnt/*|/media/*) ;;*) echo 'Custom data path needs a manual backup/install. Use /opt/viya/data or a mounted /mnt or /media subdirectory.' >&2;exit 1;;esac
[ ! -L "$DATA_DIR" ] || { echo 'Data directory must not be a symlink.' >&2;exit 1; }
PORT=${VIYA_INSTALL_PORT:-8181}
case "$PORT" in ''|*[!0-9]*) echo 'Invalid panel port.' >&2;exit 1;;esac
[ "$PORT" -ge 1024 ] && [ "$PORT" -le 65535 ] || { echo 'Panel port must be 1024-65535.' >&2;exit 1; }
if [ ! -f /etc/init.d/viya ];then
 port_busy(){ netstat -lnt 2>/dev/null | awk -v p=":$PORT" '$4~p"$"{found=1}END{exit !found}'; }
 command -v netstat >/dev/null 2>&1 || { echo 'netstat is required for the port conflict check.' >&2;exit 1; }
 while port_busy;do
  echo "Panel port $PORT is occupied." >&2
  if [ -r /dev/tty ] && [ -w /dev/tty ];then
   printf 'Choose a new panel port (1024-65535): ' >/dev/tty; read -r PORT </dev/tty
   case "$PORT" in ''|*[!0-9]*) exit 1;;esac
   [ "$PORT" -ge 1024 ] && [ "$PORT" -le 65535 ] || exit 1
  else echo 'Set VIYA_INSTALL_PORT to an available port and retry.' >&2;exit 1;fi
 done
fi

# Ports 80/443 belong to Caddy, but a fresh install has no sites yet, so a
# conflict here does not break the panel: Caddy boots from an empty bootstrap
# config, the readiness probe only checks the admin API, and the install
# succeeds. The failure surfaces later, on the first config apply, as
# "configuration not applied" while every site stays unreachable.
#
# On OpenWrt this is not an edge case: LuCI's uhttpd owns :80 by default, so the
# default Web listener cannot bind unless LuCI moves aside. Report precisely who
# owns the port and repeat it after the install instead of aborting.
#
# OpenWrt ships no python3 and no ss, so unlike install-debian.sh this probes
# listening sockets with busybox netstat. A server port that is taken is by
# definition in the LISTEN state, which makes netstat a faithful substitute.
web_port_conflicts=""
# Prints "<families>|<owners>" for a listening socket, or nothing when free.
web_port_owner(){
 case "$1" in
  tcp) wpo_table=$(netstat -lntp 2>/dev/null || true);;
  udp) wpo_table=$(netstat -lnup 2>/dev/null || true);;
  *) return 1;;
 esac
 printf '%s\n' "$wpo_table" | awk -v want=":$2" '
  {
   if ($4 !~ (want "$")) next
   # Dual stack: a socket bound only to [::] still blocks Caddy, and vice versa.
   if (split($4, parts, ":") > 2) v6 = 1; else v4 = 1
   for (i = 5; i <= NF; i++) {
    if ($i ~ /^[0-9]+\//) {
     split($i, kv, "/")
     key = kv[1] " " kv[2]
     if (!(key in seen)) { seen[key] = 1; list[++count] = key }
    }
   }
  }
  END {
   if (!v4 && !v6) exit 1
   fam = v4 ? "IPv4" : ""
   if (v6) fam = (fam == "" ? "IPv6" : fam "/IPv6")
   who = ""
   for (i = 1; i <= count; i++) {
    # netstat -p prints "PID/program", so the name is the second half.
    split(list[i], kv, " ")
    # No systemd units here; name the owning service explicitly instead.
    tail = (kv[2] == "uhttpd") ? ", LuCI web interface" : ""
    item = (kv[2] == "" ? "unknown" : kv[2]) " (pid " kv[1] tail ")"
    who = (who == "" ? item : who ", " item)
   }
   if (who == "") who = "an unidentified process"
   printf "%s|%s\n", fam, who
  }'
}
if [ ! -f /etc/init.d/viya ] && command -v netstat >/dev/null 2>&1;then
 for spec in 'tcp 80' 'tcp 443' 'udp 443';do
  wpo_info=$(web_port_owner ${spec% *} ${spec#* } || true)
  [ -n "$wpo_info" ] || continue
  web_port_conflicts="${web_port_conflicts}Warning: $(printf '%s' "${spec% *}" | tr 'a-z' 'A-Z') port ${spec#* } on ${wpo_info%%|*} is already in use by ${wpo_info#*|}.
  Viya still installs, but no proxy site can serve traffic until this port is free or the Web listener is moved to another port.
"
 done
 if [ -n "$web_port_conflicts" ];then printf '%s\n' "$web_port_conflicts" >&2;fi
fi
mkdir -p /opt/viya/bin /etc/viya/secrets
NEED=$(du -sk "$STAGE/$PKG/bin" | awk '{print $1}')
if [ -d "$DATA_DIR" ];then NEED=$((NEED + $(du -sk "$DATA_DIR" | awk '{print $1}')));fi
# Keep both old binaries and a consistent stopped-service data copy on upgrades.
NEED=$((NEED * 2 + 8192));FREE=$(df -Pk /opt/viya | awk 'END{print $4}')
[ "$FREE" -ge "$NEED" ] || { echo "Insufficient persistent storage: need about $NEED KiB, available $FREE KiB." >&2;exit 1; }
BACKUP=/opt/viya/backups/install-$(date -u +%Y%m%d-%H%M%S)
mkdir -p "$BACKUP";chmod 700 "$BACKUP"
if [ -f /etc/init.d/viya ];then
 HAD_SERVICE=1
 /etc/init.d/viya running >/dev/null 2>&1 && WAS_RUNNING=1 || true
 cp /etc/init.d/viya "$BACKUP/init-script"
 /etc/init.d/viya stop
 STOPPED=1
fi
for file in viya caddy;do [ ! -f "/opt/viya/bin/$file" ] || cp -a "/opt/viya/bin/$file" "$BACKUP/$file";done
[ ! -d "$DATA_DIR" ] || cp -a "$DATA_DIR" "$BACKUP/data"
[ ! -d /etc/viya/secrets ] || cp -a /etc/viya/secrets "$BACKUP/secrets"
CHANGED=1
for file in viya caddy;do
 cp "$STAGE/$PKG/bin/$file-linux-$ARCH" "/opt/viya/bin/$file.new";chmod 755 "/opt/viya/bin/$file.new";mv -f "/opt/viya/bin/$file.new" "/opt/viya/bin/$file"
done
if [ ! -f /etc/init.d/viya ];then
 cp "$STAGE/$PKG/deploy/openwrt/viya" /etc/init.d/viya
 if [ ! -f /etc/config/viya ];then
  printf "config viya 'main'\n\toption listen '0.0.0.0:%s'\n\toption data_dir '/opt/viya/data'\n" "$PORT" > /etc/config/viya
 fi
fi
chmod 755 /etc/init.d/viya
/etc/init.d/viya enable
/etc/init.d/viya start
LISTEN=$(uci -q get viya.main.listen || true)
if command -v netstat >/dev/null 2>&1;then
 ACTUAL_LISTEN=$(netstat -lntp 2>/dev/null | awk '$7 ~ /\/viya$/ {print $4;exit}')
 [ -z "$ACTUAL_LISTEN" ] || LISTEN=$ACTUAL_LISTEN
fi
LISTEN=${LISTEN:-127.0.0.1:8181};PORT=${LISTEN##*:}
PROBE_HOST=${LISTEN%:*};case "$PROBE_HOST" in 0.0.0.0|'') PROBE_HOST=127.0.0.1;;'::'|'[::]') PROBE_HOST='[::1]';;esac
READY=0
for attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15;do
 if command -v curl >/dev/null 2>&1;then HEALTH=$(curl -fsS --max-time 3 "http://$PROBE_HOST:$PORT/api/v1/health" || true)
 else HEALTH=$(wget -qO- "http://$PROBE_HOST:$PORT/api/v1/health" || true);fi
 if printf '%s' "$HEALTH" | grep -q '"caddy_ready":true';then READY=1;break;fi
 sleep 1
done
[ "$READY" -eq 1 ] || { echo 'Readiness check failed. Check logread -e viya.' >&2;exit 1; }
SUCCESS=1
echo '==> Viya installed, healthy and enabled at boot.'
echo "==> Panel: http://<router-LAN-IP>:$PORT (existing custom service/listen configuration is preserved)."
echo "==> Upgrade backup: $BACKUP"
if [ -n "$web_port_conflicts" ];then
 echo "==================== ACTION REQUIRED ====================" >&2
 printf '%s\n' "$web_port_conflicts" >&2
 echo "The panel is reachable and sites are saved, but none of them can serve" >&2
 echo "traffic until the ports above are released or the Web listener is moved." >&2
 echo "Find the owner:  netstat -lntp | grep ':80 '" >&2
 echo "Free port 80:    uci set uhttpd.main.listen_http='0.0.0.0:8080' '[::]:8080'" >&2
 echo "                 uci commit uhttpd && /etc/init.d/uhttpd restart" >&2
 echo "Move Viya:       panel -> Web services -> listener ports" >&2
 echo "========================================================" >&2
fi
