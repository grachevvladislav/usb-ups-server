# Install USB UPS Server on Synology DSM 7.2.2+

[Home](../README.md) · [Русский](INSTALL.ru.md) · [Troubleshooting](TROUBLESHOOTING.md)

## Requirements

DSM 7.2.2+, an administrator account over HTTPS, an x86_64 or aarch64 NAS and
a USB data cable connected to the UPS. Initial USB permissions require your own
SSH session with `sudo`. Select the package for the actual CPU, especially with
Xpenology. No Go, Python or Docker installation is needed on the NAS.

This is a development package. UPS readings and DSM server discovery have been
tested on the first x86_64 NAS. Safe-state / shutdown has not been verified.

## Obtain the package

No SPK releases are published on GitHub yet. Obtain a test build from the
maintainer or build locally with Go 1.23+ and Python 3:

```sh
git clone https://github.com/grachevvladislav/usb-ups-server.git
cd usb-ups-server
python3 dsm/tools/build.py --arch x86_64
python3 dsm/tools/check_spk.py dist/usb-ups-server-*-x86_64.spk
```

For ARM64 use `aarch64` in both commands. If multiple builds exist in `dist/`,
use the exact filename instead of `*`. DS423 / RTD1619B uses this ARM64 build.
The filename uses the CPU name `aarch64`; its SPK metadata declares the Synology
package family `armv8`, as required by the [official mapping](https://help.synology.com/developer-guide/appendix/platarchs.html). The builder verifies pinned runtime
components by SHA-256. 32-bit ARM is not supported. ARM64 hardware testing is pending.

## Install or update

1. Open **Package Center → Manual Install**, choose the `.spk` for your CPU and
   finish installation. Verify the source if DSM displays a third-party warning.
2. Open **USB UPS Server** from the DSM main menu over HTTPS. Your existing DSM
   administrator session is used.
3. After an update, close the old app window, refresh DSM and reopen the app.

Updating restarts the package service and refreshes its nginx configuration;
settings are retained. A NAS reboot is not needed.

## Configure the UPS

1. Connect the UPS USB cable. In **Settings**, refresh the USB list and select it.
2. The tested RICHCOMM “UPS USB Mon V2.0” `0925:1234` uses `nutdrv_qx / armac`.
   For another device, use driver detection with monitoring disabled; it can take
   up to five minutes.
3. Grant USB access as described below.
4. Use the NAS **LAN IP** as the NUT address, port `3493`. On initial setup the
   app suggests the current DSM connection address when it belongs to a local
   NAS interface. If several addresses are ambiguous, enter one manually.
   Saved addresses are retained. If occupied, `3494` can temporarily test readings;
   DSM's own client needs `3493`.
5. Enable monitoring and click **Apply**. Validation takes up to 30 seconds;
   the app attempts to restore previous settings on failure.
6. Check **Overview** for fresh readings. `OL` means mains, `OB` battery and
   `LB` low battery. Only measurements reported by the UPS are displayed.

One USB device must have one active UPS driver. Plan a switch if DSM or another
NUT instance already owns it; do not stop unrelated UPS services blindly.

## USB access

When USB access is denied, the app displays a dialog with a command for the
selected device. For the tested device, run on the NAS over SSH:

```sh
sudo sh /var/packages/usb-ups-server/target/tools/usb-access.sh install-rule 0925 1234
```

For another UPS replace both USB IDs. Reconnect the **USB cable** and refresh.
The rule grants access only to the matching device. Do not make the entire USB
bus world-writable or run the app as root.

If udev is unavailable, collect diagnostics first. A temporary grant is possible
when `setfacl` exists:

```sh
sudo sh /var/packages/usb-ups-server/target/tools/usb-access.sh grant 0925 1234
```

It expires when the device is reconnected. Rule persistence across DSM updates
needs verification. Xpenology N40L may also need an OHCI controller driver;
see [controller diagnosis](TROUBLESHOOTING.md#xpenology-n40l-and-missing-ohci).

## Connect DSM to the NUT server

**`127.0.0.1` will probably not work in DSM's UPS server field.** The tested
DSM 7.2.2 rejects it. A NUT listener bound only to localhost cannot accept
requests to the NAS LAN IP, even when local readings work. Use **the same NAS
LAN IP** in both the app and DSM UPS settings.

1. In the app set **NUT server address** to the NAS LAN IP, port `3493`, and apply.
   With Docker, use the Linux host IP instead.
2. Open **Control Panel → Hardware & Power → UPS**.
3. Enable UPS support, select **Synology UPS server**, enter the server IP and apply.
   With the native package on the same NAS, use that NAS's own LAN IP.
4. Verify the connection and configure DSM's safe-state behavior for your system.

The tested DSM 7.2.2 UI rejects `127.0.0.1` but accepts its own LAN IP. NUT's
listening address must match. Binding to a LAN address makes NUT available on
that interface; allow TCP 3493 only from intended clients. The package does not
add firewall rules. DSM compatibility uses UPS name `ups`, user `monuser` and
password `secret`. Do not enable DSM's separate network UPS server role here.

DSM discovery is verified on the first NAS. Power-failure monitoring and shutdown
must still be tested on a test system before relying on protection.

## Verify and remove

Check fresh readings, DSM's connected status and recovery after USB reconnect.
A controlled test disconnects only the UPS **input mains**, leaving its output
connected: expect `OL → OB → OL`. Do not drain a production UPS to `LB` for diagnosis.

Before removal, migrate NUT clients and remove any installed USB rule:

```sh
sudo sh /var/packages/usb-ups-server/target/tools/usb-access.sh remove-rule 0925 1234
```

Reconnect USB, then uninstall through Package Center. Do not delete DSM system
NUT files. See [technical notes](DSM.ru.md) for storage and recovery details.
