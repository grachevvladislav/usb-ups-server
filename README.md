# usb-ups-server

Turn any Linux box with a USB-connected UPS into a **network UPS server** that a
Synology NAS — or any other [NUT](https://networkupstools.org/) client — can
monitor.

This container talks to the UPS over USB and re-publishes its state on the network
(NUT protocol, TCP 3493), so the NAS shuts down cleanly on a power cut even.

- Alpine-based, ~30 MB, NUT 2.8.2
- Configured entirely through environment variables — no config files to edit
- `detect` mode that probes your UPS and tells you which driver to use
- Works with Synology DSM out of the box (DSM's hardcoded `monuser`/`secret`
  login is the default)

## Requirements

- A Linux host with Docker and the UPS plugged into USB
- The host and the NAS on the same network

## Quick start

```bash
git clone https://github.com/<you>/usb-ups-server.git
cd usb-ups-server
cp .env.example .env
```

Find out which driver your UPS needs:

```bash
docker compose run --rm ups detect
```

It lists the USB devices it can see, tries every plausible NUT driver in turn,
and prints a ready-to-paste configuration, for example:

```
  ✔ nutdrv_qx:armac @ 0925:1234  (protocol: Q1 0.08)

Working configuration — put this in .env:
      UPS_DRIVER=nutdrv_qx
      UPS_SUBDRIVER=armac
      UPS_VENDORID=0925
      UPS_PRODUCTID=1234
```

(NUT requires the USB ids whenever a subdriver is given; `detect` prints them.)

Put those values in `.env`, then start it:

```bash
docker compose up -d --build
```

Check that the UPS is being read:

```bash
docker compose logs -f ups
```

```
[nut] UPS is up:
[nut]   battery.voltage: 13.6
[nut]   input.voltage: 227.0
[nut]   ups.load: 18
[nut]   ups.status: OL
```

`ups.status: OL` means "on line" (mains present). `OB` is "on battery", `LB` is
"low battery".

## Connecting a Synology NAS

On the NAS: **Control Panel → Hardware & Power → UPS**

1. Tick **Enable UPS support**
2. **UPS type**: `Synology UPS server` (on older DSM: `Network UPS`)
3. **Network UPS server IP**: the IP of the Docker host
4. Apply

Then verify — **Device Information** on that same page should show the UPS
model, battery status and remaining runtime. If it stays empty, the NAS is not
talking to the server; see below.

### Verifying the connection

From any machine on the network (needs the `nut-client` / `nut` package):

```bash
upsc ups@<docker-host-ip>
```

Or without installing anything, straight from the container:

```bash
docker exec usb-ups-server upsc ups@127.0.0.1
```

From the NAS itself over SSH — DSM ships the NUT client:

```bash
upsc ups@<docker-host-ip>
```

And to confirm DSM is actually connected, check that the server sees a client
session:

```bash
docker exec usb-ups-server upsc ups@127.0.0.1 ups.status
```

A live end-to-end test: pull the mains plug from the UPS for a few seconds.
`ups.status` flips to `OB` and the NAS shows "On battery" within a few seconds.

### If the NAS shows nothing

- **Firewall**: TCP 3493 must be reachable from the NAS. Test from the NAS with
  `nc -vz <docker-host-ip> 3493`.
- **Credentials**: DSM always logs in as `monuser` / `secret`. Leave
  `MONITOR_USER` / `MONITOR_PASS` at their defaults.
- **UPS name**: DSM expects the plain name `ups`. Leave `UPS_NAME=ups`.
- **Only one Synology can be the "server"**: on the NAS pick the *client* mode
  (`Synology UPS server` = "connect to a remote NUT server"), not "Enable
  network UPS server".

## Connecting TrueNAS

TrueNAS SCALE: **System Settings → Services → UPS → Configure**
(TrueNAS CORE: **Services → UPS**). Field names shift slightly between
versions.

| Field | Value |
| --- | --- |
| UPS Mode | `Slave` (newer versions: `Remote`) |
| Identifier | `ups` (must match `UPS_NAME`) |
| Remote Host | IP of the Docker host |
| Remote Port | `3493` |
| Monitor User | `monuser` |
| Monitor Password | `secret` |
| Shutdown Mode | `UPS reaches low battery` |

**Driver** and **Port** apply to Master mode only — ignore them here. Then
enable the service and tick "Start Automatically".

Unlike DSM, TrueNAS lets you pick any credentials; reusing `monuser`/`secret`
is fine, `upsd` handles multiple clients on one account.

Verify from the TrueNAS shell:

```bash
upsc ups@<docker-host-ip>
```

If TrueNAS and the Docker host run off the *same* UPS, set the shutdown mode to
"UPS reaches low battery" — otherwise a brief flicker starts a full shutdown.

## Configuration

All settings are environment variables (see `.env.example`).

| Variable | Default | Meaning |
| --- | --- | --- |
| `UPS_DRIVER` | `usbhid-ups` | NUT driver name |
| `UPS_SUBDRIVER` | — | Subdriver, `nutdrv_qx` only (`armac`, `cypress`, `krauler`, …) |
| `UPS_VENDORID` / `UPS_PRODUCTID` | — | Pin to one USB device when several are attached |
| `UPS_SERIAL` | — | Pin by serial number |
| `UPS_EXTRA` | — | Extra `ups.conf` lines, newline separated |
| `UPS_NAME` | `ups` | UPS name clients connect to |
| `UPS_DESC` | `USB UPS` | Free-text description |
| `LISTEN_PORT` | `3493` | Host port to publish |
| `MONITOR_USER` / `MONITOR_PASS` | `monuser` / `secret` | Client login (DSM hardcodes these) |
| `POLL_INTERVAL` | `5` | Seconds between UPS polls |
| `RUN_UPSMON` | `false` | Also run `upsmon` to protect the Docker host |
| `SHUTDOWN_CMD` | no-op | Command `upsmon` runs on low battery |
| `DEBUG_LEVEL` | `0` | Driver debug verbosity (1–5) |

## Shutting down the host

The container publishes UPS state; by default it does *not* power anything off.
To also shut down the Docker host on low battery, set `RUN_UPSMON=true` and give
the container a way to reach the host's init system. The simplest approach is to
have `upsmon` write a flag file that a small unit on the host watches:

```yaml
    environment:
      RUN_UPSMON: "true"
      SHUTDOWN_CMD: "touch /shutdown-flag/poweroff"
    volumes:
      - /run/ups-shutdown:/shutdown-flag
```

Anything that lets a container halt its host (a Docker socket, `nsenter`,
privileged mode) is a security trade-off — pick the one you're comfortable with.

## Choosing a driver

`detect` does this for you, but for reference:

| UPS family | Driver |
| --- | --- |
| APC, Eaton, CyberPower, most modern USB models | `usbhid-ups` |
| Armac, Powercom, Ippon, SVEN, many no-name "Q1/Megatec" units | `nutdrv_qx` + a subdriver |
| Older Megatec serial-over-USB | `blazer_usb` |
| Richcomm dry-contact | `richcomm_usb` |

The full list is NUT's
[hardware compatibility list](https://networkupstools.org/stable-hcl.html).

## Tested hardware

| UPS | USB ID | Configuration |
| --- | --- | --- |
| RICHCOMM "UPS USB Mon V2.0" (Armac-compatible) | `0925:1234` | `UPS_DRIVER=nutdrv_qx`, `UPS_SUBDRIVER=armac` |

Reports of other working models are welcome — open a PR adding a row.

## Troubleshooting

**`Driver failed to start` / `Device not supported`**

Run `docker compose run --rm ups detect`. If nothing matches, run the driver by
hand with full debug:

```bash
docker compose run --rm -e DEBUG_LEVEL=5 ups
```

**`lsusb` inside the container shows no devices**

`/dev/bus/usb` isn't mounted, or the cgroup rule is missing. Both are in
`docker-compose.yml`; some older Docker versions need `privileged: true`
instead.

**The driver used to work on the host but not in the container (or vice versa)**

Only one process may own the USB device. Stop any NUT service on the host:
`sudo systemctl stop nut-driver nut-server`.

**Driver fails with `Entity not found` on a `0925:1234` device**

That's `richcomm_usb` guessing the wrong USB endpoint. These units are not
Richcomm dry-contact devices despite the vendor string — use
`nutdrv_qx` + `armac`.

**NUT version matters.** Debian 12 ships NUT 2.8.0, which fails on several cheap
USB UPSes that 2.8.2 handles fine. That's why this image is Alpine-based.

## License

MIT — see [LICENSE](LICENSE).
