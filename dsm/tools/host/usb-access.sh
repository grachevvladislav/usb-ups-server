#!/bin/sh
# Explicit administrative operation; not run by SPK lifecycle scripts.
set -eu
[ "$(id -u)" = 0 ] || { echo "Run with sudo" >&2; exit 1; }
ACTION="${1:-}"
VENDOR="${2:-}"
PRODUCT="${3:-}"
case "$ACTION" in grant|revoke|install-rule|remove-rule) ;; *) echo "Usage: sudo sh usb-access.sh grant|revoke|install-rule|remove-rule VENDOR PRODUCT" >&2; exit 1 ;; esac
printf '%s\n' "$VENDOR:$PRODUCT" | LC_ALL=C grep -Eq '^[0-9a-fA-F]{4}:[0-9a-fA-F]{4}$' || { echo "Invalid USB IDs" >&2; exit 1; }
VENDOR=$(printf '%s' "$VENDOR" | tr 'A-F' 'a-f')
PRODUCT=$(printf '%s' "$PRODUCT" | tr 'A-F' 'a-f')
id usb-ups-server >/dev/null 2>&1 || { echo "Install the SPK first" >&2; exit 1; }
if [ "$ACTION" = install-rule ] || [ "$ACTION" = remove-rule ]; then
    command -v udevadm >/dev/null || { echo "udevadm is unavailable" >&2; exit 1; }
    # DSM may have a working daemon while this directory has never been created,
    # or use a different control socket. Ask the daemon instead of guessing paths.
    if ! udevadm control --reload-rules; then
        echo "Cannot communicate with udev; no permission rule was changed" >&2
        exit 1
    fi
    mkdir -p /etc/udev/rules.d
    rule="/etc/udev/rules.d/99-usb-ups-server-$VENDOR-$PRODUCT.rules"
    if [ "$ACTION" = install-rule ]; then
        umask 022
        temporary=$(mktemp /etc/udev/rules.d/.usb-ups-server.XXXXXX)
        trap 'rm -f "$temporary"' EXIT HUP INT TERM
        printf 'SUBSYSTEM=="usb", ENV{DEVTYPE}=="usb_device", ATTR{idVendor}=="%s", ATTR{idProduct}=="%s", GROUP="usb-ups-server", MODE="0660"\n' "$VENDOR" "$PRODUCT" > "$temporary"
        chmod 644 "$temporary"
        mv -f "$temporary" "$rule"
        echo "Installed $rule. Replug the UPS USB cable to apply."
    else
        rm -f "$rule"
        echo "Removed $rule. Replug the UPS USB cable to restore default permissions."
    fi
    udevadm control --reload-rules
    exit 0
fi
command -v setfacl >/dev/null || { echo "setfacl is unavailable; collect DSM diagnostics before configuring permissions" >&2; exit 1; }
found=0
for device in /sys/bus/usb/devices/*; do
    [ -f "$device/idVendor" ] && [ -f "$device/idProduct" ] || continue
    [ "$(cat "$device/idVendor")" = "$VENDOR" ] && [ "$(cat "$device/idProduct")" = "$PRODUCT" ] || continue
    bus=$(cat "$device/busnum")
    num=$(cat "$device/devnum")
    node=$(printf '/dev/bus/usb/%03d/%03d' "$bus" "$num")
    [ -c "$node" ] || continue
    if [ "$ACTION" = grant ]; then setfacl -m u:usb-ups-server:rw "$node"; else setfacl -x u:usb-ups-server "$node"; fi
    echo "$ACTION: $node"
    found=1
done
[ "$found" = 1 ] || { echo "USB device not found" >&2; exit 1; }
