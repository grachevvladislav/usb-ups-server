# Build tools and target headers are isolated from the NAS. No privileged Docker.
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends build-essential bc kmod xz-utils flex bison libssl-dev libelf-dev python3 && rm -rf /var/lib/apt/lists/*
COPY cache/ds.broadwellnk-7.2.dev.txz cache/broadwellnk-gcc1220_glibc236_x86_64-GPL.txz cache/linux-4.4.x.txz /cache/
RUN mkdir -p /opt/target /opt/toolchain /work /output \
 && tar -xJf /cache/ds.broadwellnk-7.2.dev.txz -C /opt/target --strip-components=9 \
    usr/local/x86_64-pc-linux-gnu/x86_64-pc-linux-gnu/sys-root/usr/lib/modules/DSM-7.2 \
 && tar -xJf /cache/broadwellnk-gcc1220_glibc236_x86_64-GPL.txz -C /opt/toolchain --strip-components=1 \
 && tar -xJf /cache/linux-4.4.x.txz -C /work --strip-components=1 \
    linux-4.4.x/drivers/usb/host linux-4.4.x/COPYING
ENV PATH="/opt/toolchain/bin:${PATH}" ARCH=x86_64 CROSS_COMPILE=/opt/toolchain/bin/x86_64-pc-linux-gnu-
COPY build-in-container.sh verify.py /work/
ENTRYPOINT ["/bin/bash", "/work/build-in-container.sh"]
