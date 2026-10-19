# Проверка USB-пакетов RICHCOMM на ARM64 DSM

Утилита отправляет только запрос показаний `Q1\r` на `0925:1234`.
Проверяет отчёты длиной 4, 8 и 64 байта с нулевым заполнением.
Не меняет настройки пакета, не сбрасывает USB и не отправляет команды управления ИБП.
Использует libusb из установленного USB UPS Server.

1. Выключить мониторинг в приложении и закрыть ручной запуск драйвера.
2. Распаковать архив в папку, доступную пользователю `usb-ups-server`.
3. Из распакованной папки выполнить:

```sh
sudo -u usb-ups-server sh ./run.sh 2>&1 | tee ~/ups-q1-probe.txt
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
