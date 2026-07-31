#!/bin/bash
set -euo pipefail

UPS_NAME="${UPS_NAME:-ups}"
UPS_DESC="${UPS_DESC:-USB UPS}"
UPS_DRIVER="${UPS_DRIVER:-usbhid-ups}"
UPS_PORT="${UPS_PORT:-auto}"
UPS_SUBDRIVER="${UPS_SUBDRIVER:-}"
UPS_VENDORID="${UPS_VENDORID:-}"
UPS_PRODUCTID="${UPS_PRODUCTID:-}"
UPS_SERIAL="${UPS_SERIAL:-}"
UPS_EXTRA="${UPS_EXTRA:-}"

POLL_INTERVAL="${POLL_INTERVAL:-5}"
MAXRETRY="${MAXRETRY:-3}"
MAXAGE="${MAXAGE:-25}"
LISTEN_ADDR="${LISTEN_ADDR:-0.0.0.0}"
LISTEN_PORT="${LISTEN_PORT:-3493}"

MONITOR_USER="${MONITOR_USER:-monuser}"
MONITOR_PASS="${MONITOR_PASS:-secret}"
ADMIN_USER="${ADMIN_USER:-upsadmin}"
ADMIN_PASS="${ADMIN_PASS:-$(head -c 18 /dev/urandom | base64)}"


RUN_UPSMON="${RUN_UPSMON:-false}"
SHUTDOWN_CMD="${SHUTDOWN_CMD:-/usr/local/bin/entrypoint.sh noop-shutdown}"
FINALDELAY="${FINALDELAY:-5}"
DEADTIME="${DEADTIME:-15}"

DEBUG_LEVEL="${DEBUG_LEVEL:-0}"

# Read-only status page + JSON and Prometheus endpoints. Off by default; there
# is no authentication, so keep it on a trusted network.
WEB="${WEB:-false}"
WEB_PORT="${WEB_PORT:-8080}"

# Watchdog: the NUT driver does not restart itself. If it dies or wedges, upsd
# keeps serving stale data and clients silently lose protection.
WATCHDOG="${WATCHDOG:-true}"
WATCHDOG_INTERVAL="${WATCHDOG_INTERVAL:-15}"
WATCHDOG_FAILURES="${WATCHDOG_FAILURES:-3}"
WATCHDOG_MAX_RESTARTS="${WATCHDOG_MAX_RESTARTS:-3}"

log() { echo "[nut] $*"; }

case "${1:-}" in
  noop-shutdown)
    log "LOW BATTERY reached — SHUTDOWN_CMD is a no-op; set it to act."
    exit 0
    ;;
  detect|scan)
    exec /usr/local/bin/detect-ups
    ;;
  shell|bash|sh)
    exec /bin/bash
    ;;
esac

if [ -n "${UPS_SUBDRIVER}" ] && { [ -z "${UPS_VENDORID}" ] || [ -z "${UPS_PRODUCTID}" ]; }; then
  log "ERROR: UPS_SUBDRIVER requires UPS_VENDORID and UPS_PRODUCTID to be set as well."
  log "       Run 'docker compose run --rm ups detect' — it prints all three."
  exit 1
fi

mkdir -p /var/run/nut
chown root:nut /var/run/nut 2>/dev/null || true
chmod 770 /var/run/nut 2>/dev/null || true

# ---------------------------------------------------------------- ups.conf ---
{
  echo "maxretry = ${MAXRETRY}"
  echo "pollinterval = ${POLL_INTERVAL}"
  echo "user = root"
  echo
  echo "[${UPS_NAME}]"
  echo "  driver = ${UPS_DRIVER}"
  echo "  port = ${UPS_PORT}"
  echo "  desc = \"${UPS_DESC}\""
  [ -n "${UPS_SUBDRIVER}" ] && echo "  subdriver = ${UPS_SUBDRIVER}"
  [ -n "${UPS_VENDORID}" ]  && echo "  vendorid = ${UPS_VENDORID}"
  [ -n "${UPS_PRODUCTID}" ] && echo "  productid = ${UPS_PRODUCTID}"
  [ -n "${UPS_SERIAL}" ]    && echo "  serial = ${UPS_SERIAL}"
  [ "${DEBUG_LEVEL}" != "0" ] && echo "  debug_min = ${DEBUG_LEVEL}"
  [ -n "${UPS_EXTRA}" ] && printf '%s\n' "${UPS_EXTRA}"
} > /etc/nut/ups.conf

# --------------------------------------------------------------- upsd.conf ---
cat > /etc/nut/upsd.conf <<EOF
LISTEN ${LISTEN_ADDR} ${LISTEN_PORT}
MAXAGE ${MAXAGE}
EOF

# -------------------------------------------------------------- upsd.users ---
cat > /etc/nut/upsd.users <<EOF
[${ADMIN_USER}]
  password = ${ADMIN_PASS}
  actions = SET
  instcmds = ALL

[${MONITOR_USER}]
  password = ${MONITOR_PASS}
  upsmon slave
EOF

# ---------------------------------------------------------------- nut.conf ---
echo "MODE=netserver" > /etc/nut/nut.conf

# -------------------------------------------------------------- upsmon.conf --
cat > /etc/nut/upsmon.conf <<EOF
MONITOR ${UPS_NAME}@127.0.0.1 1 ${MONITOR_USER} ${MONITOR_PASS} master
MINSUPPLIES 1
SHUTDOWNCMD "${SHUTDOWN_CMD}"
POLLFREQ ${POLL_INTERVAL}
POLLFREQALERT ${POLL_INTERVAL}
HOSTSYNC 15
DEADTIME ${DEADTIME}
RBWARNTIME 43200
NOCOMMWARNTIME 300
FINALDELAY ${FINALDELAY}
EOF

chown root:nut /etc/nut/*.conf /etc/nut/upsd.users
chmod 640 /etc/nut/*.conf /etc/nut/upsd.users

log "driver=${UPS_DRIVER}${UPS_SUBDRIVER:+ subdriver=${UPS_SUBDRIVER}} ups=${UPS_NAME} listen=${LISTEN_ADDR}:${LISTEN_PORT}"
lsusb 2>/dev/null | sed 's/^/[nut]   usb: /' || log "lsusb failed — is /dev/bus/usb mounted into the container?"

cleanup() {
  log "stopping…"
  pkill -TERM upsmon 2>/dev/null || true
  pkill -TERM upsd   2>/dev/null || true
  pkill -f "busybox-extras httpd" 2>/dev/null || true
  upsdrvctl stop >/dev/null 2>&1 || true
  exit 0
}
trap cleanup TERM INT

GIVEUP_FLAG=/var/run/nut/.watchdog-giveup

restart_driver() {
  upsdrvctl -u root stop >/dev/null 2>&1 || true
  # A driver that died mid-transfer can leave the USB device claimed, and then
  # every later start fails with the device busy. Clear any leftover process
  # before trying again.
  pkill -f "/usr/lib/nut/${UPS_DRIVER}" 2>/dev/null || true
  sleep 2
  upsdrvctl -u root start
}

watchdog() {
  local fails=0 restart_failures=0
  while sleep "${WATCHDOG_INTERVAL}"; do
    if upsc "${UPS_NAME}@127.0.0.1" ups.status >/dev/null 2>&1; then
      fails=0
      restart_failures=0
      continue
    fi

    fails=$((fails + 1))
    [ "${fails}" -lt "${WATCHDOG_FAILURES}" ] && continue
    fails=0

    log "WATCHDOG: no fresh data from the UPS — restarting the driver"
    if restart_driver; then
      restart_failures=0
      continue
    fi

    restart_failures=$((restart_failures + 1))
    log "WATCHDOG: driver restart failed (${restart_failures}/${WATCHDOG_MAX_RESTARTS})"

    # Some failures cannot be fixed from inside a running container — a wedged
    # USB stack survives every upsdrvctl restart but not a fresh container.
    # Exiting non-zero hands the problem to the restart policy instead of
    # retrying forever while clients silently go unprotected.
    if [ "${restart_failures}" -ge "${WATCHDOG_MAX_RESTARTS}" ]; then
      log "WATCHDOG: giving up after ${restart_failures} attempts — exiting so the container is recreated"
      touch "${GIVEUP_FLAG}"
      kill -TERM "${UPSD_PID}" 2>/dev/null || true
      return
    fi
  done
}

# ------------------------------------------------------------------- start ---
log "starting driver…"
if ! upsdrvctl -u root start; then
  log "ERROR: the UPS driver failed to start."
  log "  * is the UPS passed through? (volumes: /dev/bus/usb, device_cgroup_rules: c 189:* rwm)"
  log "  * run 'docker compose run --rm ups detect' to probe for the right driver"
  log "  * set DEBUG_LEVEL=3 for verbose driver logs"
  exit 1
fi

log "starting upsd…"
upsd -u root -F &
UPSD_PID=$!

for _ in $(seq 1 30); do
  upsc "${UPS_NAME}@127.0.0.1" ups.status >/dev/null 2>&1 && break
  sleep 1
done

if upsc "${UPS_NAME}@127.0.0.1" >/tmp/ups.snapshot 2>/dev/null; then
  log "UPS is up:"
  sed 's/^/[nut]   /' /tmp/ups.snapshot
else
  log "WARNING: upsd is running but the UPS is not answering yet."
fi

if [ "${RUN_UPSMON}" = "true" ]; then
  log "starting upsmon (local host protection)…"
  upsmon -u root -D &
fi

if [ "${WEB}" = "true" ]; then
  log "starting web UI on port ${WEB_PORT} (/, /cgi-bin/api, /cgi-bin/metrics)…"
  # UPS_NAME is inherited by the CGI scripts through httpd's environment.
  busybox-extras httpd -f -p "${WEB_PORT}" -h /www &
fi

if [ "${WATCHDOG}" = "true" ]; then
  log "starting watchdog (checks every ${WATCHDOG_INTERVAL}s)…"
  watchdog &
fi

# `wait` returning non-zero must not trip `set -e` before the flag is checked.
UPSD_RC=0
wait "${UPSD_PID}" || UPSD_RC=$?

if [ -f "${GIVEUP_FLAG}" ]; then
  rm -f "${GIVEUP_FLAG}"
  log "exiting with failure so the restart policy recreates the container"
  exit 1
fi

exit "${UPSD_RC}"
