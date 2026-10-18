#!/bin/bash
set -euo pipefail
KERNEL=/opt/target/build
SOURCE=/work/drivers/usb/host
export LD_LIBRARY_PATH=/opt/toolchain/lib
[ -f "$KERNEL/Module.symvers" ]
[ -f "$KERNEL/include/generated/utsrelease.h" ]
grep -q '"4.4.302+"' "$KERNEL/include/generated/utsrelease.h"
# Use Synology's unchanged generated configuration and symbol table, while
# building just the two previously disabled drivers as external modules.
printf '%s\n' \
    'obj-m += ohci-hcd.o ohci-pci.o' \
    'ccflags-y += -DCONFIG_USB_OHCI_HCD_MODULE=1 -DCONFIG_USB_OHCI_HCD_PCI_MODULE=1' \
    > "$SOURCE/Makefile"
# Synology's old GCC option was removed in the published GCC 12 toolchain.
sed -i 's/--param=allow-store-data-races=0/--allow-store-data-races/g' "$KERNEL/Makefile"
make -C "$KERNEL" M="$SOURCE" -j2 modules 2>&1 | tee /output/build.log
python3 /work/verify.py "$KERNEL" "$SOURCE" /output
cp "$SOURCE/ohci-hcd.ko" "$SOURCE/ohci-pci.ko" /output/
cp /work/COPYING /output/COPYING
mkdir -p /output/source
for file in ohci-hcd.c ohci-pci.c ohci.h ohci-hub.c ohci-dbg.c ohci-mem.c ohci-q.c pci-quirks.h Makefile; do
    cp "$SOURCE/$file" /output/source/
done
echo 'OHCI modules built and checked. Hardware testing is still required.'
