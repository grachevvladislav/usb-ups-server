# Synology USB UPS troubleshooting

[Home](../README.md) · [Installation](INSTALL.md) · [Русский](TROUBLESHOOTING.ru.md)

Follow the chain: **USB enumeration → permissions → driver → fresh NUT data →
DSM connection → safe-state test**. Settings at a later layer do not repair an
underlying USB controller problem.

## UPS not in the USB list

Refresh the USB list, reconnect the USB data cable and try another cable/port.
Leave the UPS power outputs connected. On the NAS, collect:

```sh
sh /var/packages/usb-ups-server/target/tools/collect-dsm.sh
sudo dmesg | tail -n 80
```

If the UPS VID:PID is absent from the system too, diagnose the cable, controller
or kernel driver first. NUT detection and permission changes cannot enumerate
an absent USB device. A USB `error -110` is a timeout, not a definitive diagnosis;
match its timestamp/port to the UPS, rather than the NAS boot flash drive.

## USB permission denied

If the device exists but the driver reports `Permission denied` / `Access denied`,
grant access only to its IDs. For the tested `0925:1234`:

```sh
sudo sh /var/packages/usb-ups-server/target/tools/usb-access.sh install-rule 0925 1234
```

Reconnect USB. If udev is unavailable, include the complete helper error and
system diagnostics. Version 0.1.1 fixed a false failure when `/etc/udev/rules.d`
did not yet exist. The `grant` action is temporary and requires `setfacl`.
Do not make all of `/dev/bus/usb` world-writable or run the app as root.

## RICHCOMM 0925:1234: Entity not found

The tested “UPS USB Mon V2.0” has a RICHCOMM vendor string but needs
`nutdrv_qx`, subdriver `armac`, vendor `0925`, product `1234`, empty serial.
`richcomm_usb` is the wrong driver for this tested unit. Its USB serial is
unreliable, so the app's profile leaves it blank. Other revisions may differ;
use detection/logs and do not fake IDs to force a mismatched driver.

## Device busy or address already in use

Another DSM driver, package, container or NUT instance may own the USB device.
Use the diagnostic process/port list to identify it before planning a switch.
Do not blindly stop unrelated UPS services.

`Address already in use` concerns the server port. Identify the owner of `3493`.
Port `3494` can temporarily test readings, but DSM's client needs `3493`.

## DSM cannot connect to the UPS server

When app readings are fresh, check:

- DSM mode **Synology UPS server**, using the server's actual IP. For the native
  package on the same NAS, use its own LAN IP.
- NUT must listen on that address. A listener on `127.0.0.1` cannot accept LAN-IP
  requests. `0.0.0.0` means all IPv4 interfaces; it is not a server destination IP.
- The tested DSM 7.2.2 rejects `127.0.0.1` in its server field but accepts its LAN IP.
- TCP port `3493`, UPS name `ups`, monitor user `monuser`, password `secret`.
- Firewall access for intended clients only. Do not disable the firewall or
  expose NUT to the Internet.

From a machine with a NUT client:

```sh
upsc ups@SERVER_IP:3493
```

Replace `SERVER_IP`. DSM may provide `/usr/syno/bin/upsc`. A successful query
proves that NUT responds, not that DSM safe-state / shutdown is configured.

## Battery charge or runtime is missing

Simple Q1/Megatec UPS devices may not report `battery.charge` or `battery.runtime`.
The tested RICHCOMM reports battery voltage but not percentage/runtime. These
values are not guessed from voltage. Missing values alone do not indicate a
connection failure when other readings and timestamps update.

## DSM window or session error

Close the old app window, refresh DSM over HTTPS and reopen from the main menu.
Use a current DSM administrator session. Version 0.1.3 had a CSP frame problem;
0.1.4 used a same-origin iframe; 0.1.5 removed frames. Version 0.1.6 fixed and
verified current-session SynoToken authentication on the first NAS.
Do not disable CSP or authentication. Include the exact app version and error.
The standalone 8787 key is not used for the DSM window.

## Xpenology N40L and missing OHCI

The tested AMD N40L had OHCI controllers `1002:4397` without a driver. Working
EHCI alone did not enumerate the UPS. This is specific to the tested Xpenology
configuration, not a requirement for every Synology NAS.

```sh
uname -r
lsmod | grep -Ei 'ohci|ehci|uhci'
lspci -nnk | grep -A 3 -i 'USB controller'
modinfo ehci-pci | grep -E 'filename|vermagic'
grep '^unique=' /etc.defaults/synoinfo.conf
```

Tool availability varies. `Module ohci-pci not found` means the module is absent.
Matching `uname -r` alone does not establish kernel-module compatibility.
For **broadwellnk / DSM 7.2 / 4.4.302+**, see the separate
[OHCI build and temporary test guide (Russian)](../dsm/kernel/ohci/README.ru.md).
Modules are not included in the SPK. Persistent Arc loading and reboot recovery
are not yet verified on the first NAS.

## What to include in an issue

UPS model and VID:PID; NAS model/CPU; exact DSM and app versions; SPK or Docker;
symptom and recent changes. In the app use **Log → Download diagnostics**.
Without the app:

```sh
sh /var/packages/usb-ups-server/target/tools/collect-dsm.sh
sudo tail -n 100 /var/packages/usb-ups-server/var/service.log
sudo tail -n 100 /var/packages/usb-ups-server/var/nut.log
```

Diagnostics may contain IP addresses, serial numbers, hardware details and logs.
Review/redact before posting publicly. Never attach `admin-token`, DSM cookies,
SynoToken, passwords, `server.key` or the complete package data directory.

[Report an issue](https://github.com/grachevvladislav/usb-ups-server/issues/new/choose).
