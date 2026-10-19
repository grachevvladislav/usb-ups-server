package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type usbPermissionError struct{ Vendor, Product string }

func (e *usbPermissionError) Error() string {
	return fmt.Sprintf("Нет доступа к USB-устройству %s:%s у пользователя пакета", e.Vendor, e.Product)
}
func (e *usbPermissionError) command() string {
	return fmt.Sprintf("sudo sh /var/packages/usb-ups-server/target/tools/usb-access.sh install-rule %s %s", e.Vendor, e.Product)
}

// Opening usbfs checks effective access without claiming an interface or sending
// transfers. Do not infer effective ACLs from mode bits alone.
func usbAccess(path string) string {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err == nil {
		_ = f.Close()
		return "granted"
	}
	if errors.Is(err, os.ErrPermission) {
		return "denied"
	}
	return "unavailable"
}
func checkUSBPermissions(c Config, devices []USBDevice) error {
	if c.VendorID == "" || c.ProductID == "" {
		return nil
	}
	matched, denied := 0, 0
	for _, d := range devices {
		if !strings.EqualFold(d.VendorID, c.VendorID) || !strings.EqualFold(d.ProductID, c.ProductID) || (c.Serial != "" && c.Serial != d.Serial) {
			continue
		}
		matched++
		if d.Access == "denied" {
			denied++
		}
	}
	if matched > 0 && matched == denied {
		return &usbPermissionError{c.VendorID, c.ProductID}
	}
	return nil
}
