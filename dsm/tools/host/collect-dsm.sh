#!/bin/sh
# Read-only, no credentials or configuration contents. Run without sudo first.
set -u
echo '=== DSM version ==='
cat /etc.defaults/VERSION 2>/dev/null || true
echo '=== Model ==='
cat /proc/sys/kernel/syno_hw_version 2>/dev/null || true
echo '=== Architecture ==='
uname -m
echo '=== USB permissions tooling ==='
command -v setfacl || true
command -v udevadm || true
echo '=== Package user ==='
id usb-ups-server 2>/dev/null || true
echo '=== USB devices ==='
for device in /sys/bus/usb/devices/*; do
    [ -f "$device/idVendor" ] || continue
    printf '%s ' "$device"
    cat "$device/idVendor" "$device/idProduct" 2>/dev/null | tr '\n' ' '
    cat "$device/product" 2>/dev/null || true
    echo
done
echo '=== Device node permissions ==='
ls -l /dev/bus/usb/*/* 2>/dev/null || true
echo '=== UPS processes (names only) ==='
ps -eo pid,user,comm 2>/dev/null | awk 'NR==1 || /upsd|upsmon|usbhid|nutdrv|synoups/' || true
echo '=== Listening ports ==='
netstat -lnt 2>/dev/null | awk 'NR<=2 || /:3493[[:space:]]|:8787[[:space:]]/' || true
