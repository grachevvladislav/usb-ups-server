#!/bin/sh
set -eu
if [ "$(id -un)" != usb-ups-server ]; then
    echo 'Run with: sudo -u usb-ups-server sh ./run.sh' >&2
    exit 1
fi
if [ "$(uname -m)" != aarch64 ]; then
    echo 'This build is for aarch64 DSM only.' >&2
    exit 1
fi
if ps -eo comm | awk '/^(nutdrv_qx|usbhid-ups|blazer_usb|richcomm_usb|upsd)$/ { found=1 } END { exit !found }'; then
    echo 'Stop UPS monitoring before probing; a NUT process is running.' >&2
    exit 1
fi
probe_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
runtime=/var/packages/usb-ups-server/target/nut
exec "$runtime/lib/ld-musl-aarch64.so.1" \
    --library-path "$runtime/lib:$runtime/usr/lib" "$probe_dir/usb-q1-probe"
