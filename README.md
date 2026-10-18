# USB UPS Server — Synology DSM & Docker NUT server

[Русский](README.ru.md) · **English** · [DSM installation](docs/INSTALL.md) · [Troubleshooting](docs/TROUBLESHOOTING.md)

Connect a USB UPS that Synology DSM does not recognize, read its status with
**Network UPS Tools (NUT)**, and make it available to a Synology NAS or another
NUT client. Use the native DSM package on the NAS, or run the Docker server on
a separate Linux machine with the UPS connected to it.

Useful when **Synology does not detect a USB UPS**, the built-in driver does not
support your device, or you need a **network UPS server for DSM / TrueNAS**.
The tested device is **RICHCOMM “UPS USB Mon V2.0”, USB `0925:1234`**, using
`nutdrv_qx` with the `armac` subdriver. A matching brand or USB ID alone does not
guarantee compatibility with every UPS revision.

## Choose how to run it

| | Native Synology package | Docker NUT server |
| --- | --- | --- |
| UPS USB cable | Connected to the NAS | Connected to a Linux Docker host |
| Interface | DSM desktop window; current DSM administrator session | Optional read-only status page |
| Requirements | DSM 7.2.2+, x86_64 or aarch64 | Linux, Docker Compose, USB access |
| Install | [DSM guide](docs/INSTALL.md) | [Docker guide](docs/DOCKER.md) |

The DSM package includes NUT 2.8.2, USB configuration, driver detection, live
voltage/load/battery readings, a driver watchdog, logs and diagnostics export.
It runs without Docker or Python on the NAS. Its window uses DSM components,
without an iframe or a separate application login.

**Development status:** USB readings and DSM network UPS discovery have been
verified on Xpenology DS3622xs+ / AMD N40L, DSM 7.2.2-72806 Update 10. Automatic
safe-state / shutdown on a power failure has **not** been verified. The package
publishes UPS data; DSM's own UPS client must be configured and tested separately.
ARM64 builds exist; DS423 hardware testing is pending.

## Install on DSM

1. Obtain or [build an SPK](docs/INSTALL.md#obtain-the-package) for your NAS CPU.
   There are currently no published GitHub release packages.
2. In **Package Center → Manual Install**, select the `.spk` and start it.
3. Open **USB UPS Server** from the DSM main menu over HTTPS.
4. In **Settings**, refresh the USB list and select the UPS. For the tested
   `0925:1234` device, the app selects `nutdrv_qx / armac`.
5. [Grant access to that USB device](docs/INSTALL.md#usb-access), enable
   monitoring and click **Apply**. Check for live data in **Overview**.
6. To protect a NAS, follow [DSM network UPS setup](docs/INSTALL.md#connect-dsm-to-the-nut-server).

## Common problems

| Symptom | Start here |
| --- | --- |
| UPS absent from the USB list | [USB cable, kernel and controller](docs/TROUBLESHOOTING.md#ups-not-in-the-usb-list) |
| `Permission denied`, works only as root | [USB permissions](docs/TROUBLESHOOTING.md#usb-permission-denied) |
| `Entity not found` with RICHCOMM `0925:1234` | [Driver and Armac profile](docs/TROUBLESHOOTING.md#richcomm-09251234-entity-not-found) |
| Data appears in the app, but DSM cannot connect | [NUT address, port and DSM client](docs/TROUBLESHOOTING.md#dsm-cannot-connect-to-the-ups-server) |
| No battery percentage or remaining runtime | [Unsupported readings](docs/TROUBLESHOOTING.md#battery-charge-or-runtime-is-missing) |
| Empty window or DSM session error after an update | [DSM window](docs/TROUBLESHOOTING.md#dsm-window-or-session-error) |
| N40L / Arc: UPS appears only after loading OHCI | [Xpenology controller diagnosis](docs/TROUBLESHOOTING.md#xpenology-n40l-and-missing-ohci) |

## Documentation and support

- [DSM installation — English](docs/INSTALL.md) / [Русский](docs/INSTALL.ru.md)
- [Troubleshooting — English](docs/TROUBLESHOOTING.md) / [Русский](docs/TROUBLESHOOTING.ru.md)
- [Docker, TrueNAS and environment variables](docs/DOCKER.md) / [Русский](docs/DOCKER.ru.md)
- [DSM architecture, build and hardware test notes (Russian)](docs/DSM.ru.md)
- [Report a bug or working UPS model](https://github.com/grachevvladislav/usb-ups-server/issues/new/choose)

Include the UPS model, USB VID:PID, NAS CPU/model, DSM version and diagnostic
output. Remove secrets and identifying information before posting. The issue
forms list exactly what is useful.

## License

[MIT](LICENSE). NUT and bundled runtime components retain their own licenses.
