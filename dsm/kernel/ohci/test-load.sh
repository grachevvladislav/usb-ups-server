#!/bin/sh
# Explicit, temporary hardware test. Does not install anything into /lib/modules.
set -eu
cd "$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
[ "$(id -u)" = 0 ] || { echo 'Run with sudo sh test-load.sh'; exit 1; }
[ "$(uname -m)" = x86_64 ] && [ "$(uname -r)" = 4.4.302+ ] || { echo 'Wrong architecture or kernel'; exit 1; }
grep -q '^unique="synology_broadwellnk_3622xs+"$' /etc.defaults/synoinfo.conf || { echo 'This test bundle is for DS3622xs+ broadwellnk only'; exit 1; }
sha256sum -c SHA256SUMS
expected=$(modinfo -F vermagic /lib/modules/ehci-pci.ko)
for file in ohci-hcd.ko ohci-pci.ko; do
    actual=$(modinfo -F vermagic "./$file")
    [ "$actual" = "$expected" ] || { echo "Vermagic mismatch: $file"; exit 1; }
done
[ ! -d /sys/module/ohci_hcd ] && [ ! -d /sys/module/ohci_pci ] || { echo 'OHCI is already loaded; do not replace live modules'; exit 1; }
insmod ./ohci-hcd.ko
if ! insmod ./ohci-pci.ko; then
    echo 'OHCI PCI module failed to load; see dmesg' >&2
    rmmod ohci_hcd || true
    exit 1
fi
echo 'Temporary OHCI drivers loaded. Reconnect UPS USB, then refresh the application USB list.'
echo 'No boot files were changed. After stopping UPS monitoring, rollback: rmmod ohci_pci; rmmod ohci_hcd'
