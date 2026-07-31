FROM alpine:3.22

RUN apk add --no-cache \
      nut \
      libusb \
      usbutils \
      bash \
      tzdata \
 && rm -f /etc/nut/*.conf /etc/nut/*.users

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
COPY detect-ups.sh /usr/local/bin/detect-ups
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/detect-ups

EXPOSE 3493/tcp

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD upsc "${UPS_NAME:-ups}@127.0.0.1" ups.status >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
