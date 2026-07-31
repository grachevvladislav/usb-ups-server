#!/bin/bash
set -uo pipefail

PROBE_TIMEOUT="${PROBE_TIMEOUT:-15}"
PROBE_ALL="${PROBE_ALL:-false}"
SKIP_SCANNER="${SKIP_SCANNER:-false}"
CONF=/etc/nut/ups.conf
mkdir -p /var/run/nut && chmod 770 /var/run/nut

echo "=== USB devices visible to the container ==="
if ! lsusb 2>/dev/null; then
  echo "lsusb failed — is /dev/bus/usb mounted into the container?"
  exit 1
fi

if [ -n "${UPS_VENDORID:-}" ] && [ -n "${UPS_PRODUCTID:-}" ]; then
  DEVICES="${UPS_VENDORID}:${UPS_PRODUCTID}"
else
  DEVICES=$(lsusb 2>/dev/null | sed -n 's/.* ID \([0-9a-f]\{4\}\):\([0-9a-f]\{4\}\).*/\1:\2/p' | grep -v '^1d6b:' | sort -u)
fi

if [ "$SKIP_SCANNER" != "true" ]; then
  echo
  echo "=== nut-scanner ==="
  timeout 30 nut-scanner -U -q 2>&1 | sed 's/^/  /' || echo "  (nothing found)"
fi

AUTOSCAN_DRIVERS="usbhid-ups blazer_usb richcomm_usb nutdrv_atcl_usb tripplite_usb riello_usb bcmxcp_usb"
QX_SUBDRIVERS="armac cypress ippon krauler phoenix fabula hunnox sgs snr ablerex"

FOUND_DRV=""
FOUND_SUB=""
FOUND_DEV=""

probe() {  # driver, subdriver, vendorid:productid
  local drv="$1" sub="$2" dev="${3:-}" label out proto reason
  [ -x "/usr/lib/nut/${drv}" ] || return 1
  label="${drv}${sub:+:${sub}}${dev:+ @ ${dev}}"

  {
    echo "user = root"
    echo "[probe]"
    echo "  driver = ${drv}"
    echo "  port = auto"
    [ -n "$sub" ] && echo "  subdriver = ${sub}"
    if [ -n "$dev" ]; then
      echo "  vendorid = ${dev%%:*}"
      echo "  productid = ${dev##*:}"
    fi
  } > "$CONF"

  out=$(timeout "${PROBE_TIMEOUT}" "/usr/lib/nut/${drv}" -a probe -DD -u root 2>&1)
  if echo "$out" | grep -q "Driver initialization completed"; then
    proto=$(echo "$out" | grep -m1 "Using protocol" | sed 's/.*Using protocol: //')
    echo "  ✔ ${label}${proto:+  (protocol: ${proto})}"
    if [ -z "$FOUND_DRV" ]; then
      FOUND_DRV="$drv"; FOUND_SUB="$sub"; FOUND_DEV="$dev"
    fi
    return 0
  fi
  reason=$(echo "$out" | grep -m1 -iE "not supported|no supported|can't |unable|failed|timed out|mandatory" | sed 's/^ *[0-9.]*[[:space:]]*//')
  echo "  ✘ ${label}${reason:+  — ${reason}}"
  return 1
}

stop_early() { [ "$PROBE_ALL" != "true" ] && [ -n "$FOUND_DRV" ]; }

echo
echo "=== probing drivers (up to ${PROBE_TIMEOUT}s each) ==="
for drv in $AUTOSCAN_DRIVERS; do
  stop_early && break
  probe "$drv" "" ""
done

for dev in $DEVICES; do
  stop_early && break
  for sub in $QX_SUBDRIVERS; do
    stop_early && break
    probe nutdrv_qx "$sub" "$dev"
  done
done

echo
if [ -n "$FOUND_DRV" ]; then
  echo "Working configuration — put this in .env:"
  echo "      UPS_DRIVER=${FOUND_DRV}"
  if [ -n "$FOUND_SUB" ]; then
    echo "      UPS_SUBDRIVER=${FOUND_SUB}"
    echo "      UPS_VENDORID=${FOUND_DEV%%:*}"
    echo "      UPS_PRODUCTID=${FOUND_DEV##*:}"
  fi
else
  echo "No driver managed to talk to the UPS."
  echo "Check the NUT hardware compatibility list: https://networkupstools.org/stable-hcl.html"
fi
