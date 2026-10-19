# Проверка USB-пакетов RICHCOMM на ARM64 DSM

Утилита отправляет только запрос показаний `Q1\r` на `0925:1234`.
Проверяет отчёты длиной 4, 8 и 64 байта с нулевым заполнением.
Не меняет настройки пакета, не сбрасывает USB и не отправляет команды управления ИБП.
Использует libusb из установленного USB UPS Server.

1. Выключить мониторинг в приложении и закрыть ручной запуск драйвера.
2. Распаковать архив. Для запуска использовать подкаталог данных пакета:
   `/tmp` на NAS может быть смонтирован с `noexec`. При этом загрузчик musl
   выдаёт `Not a valid dynamic program`, даже если ELF-файл корректен.
3. Если архив распакован в `/tmp`, выполнить:

```sh
sudo cp -R /tmp/usb-q1-probe-aarch64 /var/packages/usb-ups-server/var/
sudo chmod -R a+rX /var/packages/usb-ups-server/var/usb-q1-probe-aarch64
sudo -u usb-ups-server sh /var/packages/usb-ups-server/var/usb-q1-probe-aarch64/run.sh 2>&1 | tee ~/ups-q1-probe.txt
```

Проверка ограничена по времени: обычно несколько секунд, максимум около минуты.
Отчёт `ups-q1-probe.txt` содержит ответы ИБП в hex и ASCII.

Сборка (Docker, из корня репозитория):

```sh
mkdir -p dist/usb-q1-probe-aarch64
docker run --rm --platform linux/arm64 \
  -v "$PWD/dsm/tools/usb-q1-probe:/src:ro" \
  -v "$PWD/dist/usb-q1-probe-aarch64:/out" alpine:3.22 sh -ec \
  'apk add --no-cache gcc musl-dev libusb-dev; cc -std=c99 -Wall -Wextra -Werror -O2 -I/usr/include/libusb-1.0 /src/probe.c -lusb-1.0 -o /out/usb-q1-probe'
```

Исходный код утилиты распространяется по MIT License проекта.
