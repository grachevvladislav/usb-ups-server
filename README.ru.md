# usb-ups-server

Превращает любой Linux-хост с подключённым по USB ИБП в **сетевой UPS-сервер**,
к которому может подключиться Synology NAS (или любой другой клиент
[NUT](https://networkupstools.org/)).

Контейнер общается с ИБП по USB и публикует его состояние в сеть (протокол NUT,
TCP 3493) — NAS корректно выключится при пропадании питания.

- База Alpine, ~30 МБ, NUT 2.8.2
- Вся настройка — через переменные окружения
- Режим `detect`: сам подбирает драйвер для вашего ИБП
- С Synology DSM работает «из коробки» (жёстко зашитый в DSM логин
  `monuser`/`secret` — значение по умолчанию)

## Что нужно

- Linux-хост с Docker и воткнутым в USB ИБП
- Хост и NAS в одной сети

## Быстрый старт

```bash
git clone https://github.com/grachevvladislav/usb-ups-server.git
cd usb-ups-server
cp .env.example .env
```

Определить драйвер:

```bash
docker compose run --rm ups detect
```

Скрипт покажет видимые USB-устройства, переберёт все подходящие драйверы NUT и
выдаст готовую конфигурацию:

```
  ✔ nutdrv_qx:armac @ 0925:1234  (protocol: Q1 0.08)

Working configuration — put this in .env:
      UPS_DRIVER=nutdrv_qx
      UPS_SUBDRIVER=armac
      UPS_VENDORID=0925
      UPS_PRODUCTID=1234
```

(Если задан субдрайвер, NUT обязательно требует и USB-идентификаторы — `detect`
их печатает.)

Впишите их в `.env` и запускайте:

```bash
docker compose up -d --build
```

Проверка:

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

`OL` — питание от сети, `OB` — работа от батареи, `LB` — батарея разряжена.

## Подключение Synology

На NAS: **Панель управления → Оборудование и питание → ИБП**

1. Включить **Поддержка ИБП**
2. **Тип ИБП**: `Сервер Synology UPS` (в старых DSM — `Сетевой ИБП`)
3. **IP-адрес сервера сетевого ИБП**: адрес Docker-хоста
4. Применить

### Как проверить, что связь есть

На той же странице DSM нажмите **Информация об устройстве** — должны появиться
модель ИБП, заряд и остаток времени работы. Пусто — связи нет.

С любой машины в сети (нужен пакет `nut-client`):

```bash
upsc ups@<ip-докер-хоста>
```

Прямо из контейнера, ничего не устанавливая:

```bash
docker exec usb-ups-server upsc ups@127.0.0.1
```

По SSH с самого NAS (в DSM клиент NUT уже есть):

```bash
upsc ups@<ip-докер-хоста>
```

Боевая проверка: выдерните вилку ИБП из розетки на несколько секунд.
`ups.status` станет `OB`, а DSM в течение нескольких секунд покажет «Работа от
батареи».

### Если NAS ничего не видит

- **Файрвол**: с NAS должен быть доступен TCP 3493 — проверьте
  `nc -vz <ip-хоста> 3493`.
- **Логин**: DSM всегда ходит под `monuser`/`secret`. Не меняйте
  `MONITOR_USER`/`MONITOR_PASS`.
- **Имя ИБП**: DSM ожидает имя `ups`. Оставьте `UPS_NAME=ups`.
- **Режим**: на NAS нужен режим *клиента* (`Сервер Synology UPS` = подключиться
  к удалённому серверу), а не «включить сетевой UPS-сервер».

## Подключение TrueNAS

TrueNAS SCALE: **System Settings → Services → UPS → Configure**
(TrueNAS CORE: **Services → UPS**). Названия полей между версиями немного
отличаются.

| Поле | Значение |
| --- | --- |
| UPS Mode | `Slave` (в новых версиях — `Remote`) |
| Identifier | `ups` (должен совпадать с `UPS_NAME`) |
| Remote Host | IP Docker-хоста |
| Remote Port | `3493` |
| Monitor User | `monuser` |
| Monitor Password | `secret` |
| Shutdown Mode | `UPS reaches low battery` |

Поля **Driver** и **Port** нужны только в режиме Master — здесь их игнорируйте.
Затем включите сервис и отметьте «Start Automatically».

В отличие от DSM, TrueNAS позволяет задать любые учётные данные; переиспользовать
`monuser`/`secret` нормально — `upsd` держит несколько клиентов на одном аккаунте.

Проверка из шелла TrueNAS:

```bash
upsc ups@<ip-докер-хоста>
```

Если TrueNAS и Docker-хост питаются от одного ИБП, ставьте режим выключения
«UPS reaches low battery» — иначе кратковременное моргание света запустит
полное выключение.

## Настройки

Полный список переменных — в `.env.example` и в
[README.md](README.md#configuration).

### Вотчдог

Драйвер NUT сам не перезапускается: если он умрёт, `upsd` продолжит отдавать
устаревшие данные, и клиенты незаметно останутся без защиты. Встроенный вотчдог
(`WATCHDOG=true`, включён по умолчанию) опрашивает ИБП и перезапускает драйвер
после `WATCHDOG_INTERVAL × WATCHDOG_FAILURES` секунд без свежих данных — полное
восстановление занимает около минуты.

## Проверенное железо

| ИБП | USB ID | Конфигурация |
| --- | --- | --- |
| RICHCOMM «UPS USB Mon V2.0» (совместим с Armac) | `0925:1234` | `UPS_DRIVER=nutdrv_qx`, `UPS_SUBDRIVER=armac` |

## Частые проблемы

**Драйвер не стартует / `Device not supported`** — запустите
`docker compose run --rm ups detect`, а для подробного лога:
`docker compose run --rm -e DEBUG_LEVEL=5 ups`.

**`lsusb` в контейнере пуст** — не проброшен `/dev/bus/usb` или нет cgroup-правила
(оба уже есть в `docker-compose.yml`); на старых версиях Docker может
понадобиться `privileged: true`.

**На хосте драйвер работал, в контейнере — нет** — устройство может держать
только один процесс: `sudo systemctl stop nut-driver nut-server`.

**Ошибка `Entity not found` на устройстве `0925:1234`** — это `richcomm_usb`
ошибается с USB-endpoint. Несмотря на строку вендора, это не dry-contact
устройство: нужен `nutdrv_qx` + `armac`.

**Версия NUT важна**: в Debian 12 идёт NUT 2.8.0, который не справляется с рядом
дешёвых USB-ИБП — в 2.8.2 они работают. Поэтому образ собран на Alpine.

## Лицензия

MIT — см. [LICENSE](LICENSE).
