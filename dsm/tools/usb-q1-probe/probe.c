/* SPDX-License-Identifier: MIT
 * Diagnostic only: send Q1\r status queries with different report lengths.
 * No detach, USB reset, configuration change or UPS control commands.
 */
#include <libusb.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

static void dump(const unsigned char *data, int length)
{
    for (int i = 0; i < length; i++) printf("%02x ", data[i]);
    printf(" | ");
    for (int i = 0; i < length; i++)
        putchar(data[i] >= 32 && data[i] < 127 ? data[i] : '.');
    putchar('\n');
}

int main(void)
{
    libusb_context *ctx = NULL;
    libusb_device **devices = NULL, *selected = NULL;
    libusb_device_handle *handle = NULL;
    struct libusb_config_descriptor *config = NULL;
    int result = 1, count = 0, claimed = 0;
    unsigned int in_size = 0, out_size = 0;
    setbuf(stdout, NULL);
    if (geteuid() == 0) {
        fputs("Run as usb-ups-server, not root.\n", stderr);
        return 1;
    }
    if (libusb_init(&ctx) != 0) return 1;
    ssize_t total = libusb_get_device_list(ctx, &devices);
    if (total < 0) goto done;
    for (ssize_t i = 0; i < total; i++) {
        struct libusb_device_descriptor d;
        if (libusb_get_device_descriptor(devices[i], &d) == 0 &&
            d.idVendor == 0x0925 && d.idProduct == 0x1234) {
            selected = devices[i];
            count++;
        }
    }
    if (count != 1) {
        printf("Expected exactly one 0925:1234 device; found %d.\n", count);
        goto done;
    }
    printf("Q1 report-length probe, USB %03u/%03u, uid=%lu\n",
        libusb_get_bus_number(selected), libusb_get_device_address(selected),
        (unsigned long)geteuid());
    int rc = libusb_open(selected, &handle);
    if (rc != 0) { printf("open: %s\n", libusb_error_name(rc)); goto done; }
    rc = libusb_get_active_config_descriptor(selected, &config);
    if (rc != 0) { printf("descriptor: %s\n", libusb_error_name(rc)); goto done; }
    if (config->bNumInterfaces != 1 || config->interface[0].num_altsetting != 1) {
        puts("Unexpected USB interfaces; no queries sent.");
        goto done;
    }
    const struct libusb_interface_descriptor *iface = &config->interface[0].altsetting[0];
    if (iface->bInterfaceNumber != 0 || iface->bAlternateSetting != 0) goto done;
    for (int i = 0; i < iface->bNumEndpoints; i++) {
        const struct libusb_endpoint_descriptor *ep = &iface->endpoint[i];
        printf("endpoint=%02x attributes=%02x max_packet=%u interval=%u\n",
            ep->bEndpointAddress, ep->bmAttributes, ep->wMaxPacketSize, ep->bInterval);
        if ((ep->bmAttributes & LIBUSB_TRANSFER_TYPE_MASK) != LIBUSB_TRANSFER_TYPE_INTERRUPT) continue;
        if (ep->bEndpointAddress == 0x82) in_size = ep->wMaxPacketSize;
        if (ep->bEndpointAddress == 0x02) out_size = ep->wMaxPacketSize;
    }
    if (in_size != 64 || out_size != 64) {
        puts("Expected interrupt endpoints 82/02, 64 bytes each; no queries sent.");
        goto done;
    }
    rc = libusb_claim_interface(handle, 0);
    if (rc != 0) { printf("claim: %s\n", libusb_error_name(rc)); goto done; }
    claimed = 1;
    const int sizes[] = {4, 8, 64};
    for (unsigned int test = 0; test < sizeof(sizes) / sizeof(sizes[0]); test++) {
        unsigned char packet[64] = {0xa3, 'Q', '1', '\r'};
        unsigned char reply[64];
        int transferred = 0;
        /* Bounded drain of replies left by an earlier query. */
        for (int i = 0; i < 4; i++) {
            rc = libusb_interrupt_transfer(handle, 0x82, reply, sizeof(reply), &transferred, 100);
            if (rc != 0) break;
            printf("pending %d: ", transferred); dump(reply, transferred);
        }
        printf("Q1 OUT length=%d: ", sizes[test]); dump(packet, sizes[test]);
        rc = libusb_interrupt_transfer(handle, 0x02, packet, sizes[test], &transferred, 2000);
        printf("write=%s transferred=%d\n", libusb_error_name(rc), transferred);
        if (rc != 0 || transferred != sizes[test]) continue;
        int saw_status = 0;
        for (int i = 0; i < 8; i++) {
            memset(reply, 0, sizeof(reply));
            rc = libusb_interrupt_transfer(handle, 0x82, reply, sizeof(reply), &transferred, 1500);
            printf("read=%s transferred=%d", libusb_error_name(rc), transferred);
            if (transferred > 0) { printf(": "); dump(reply, transferred); } else putchar('\n');
            if (rc != 0 || transferred == 0) break;
            if (memchr(reply, '(', (size_t)transferred)) saw_status = 1;
            if (memchr(reply, '\r', (size_t)transferred)) {
                if (saw_status) {
                    printf("Status-like reply received with OUT length=%d; inspect raw data.\n", sizes[test]);
                    result = 0;
                    goto done;
                }
                break;
            }
        }
    }
done:
    if (claimed) libusb_release_interface(handle, 0);
    if (config) libusb_free_config_descriptor(config);
    if (handle) libusb_close(handle);
    if (devices) libusb_free_device_list(devices, 1);
    libusb_exit(ctx);
    return result;
}
