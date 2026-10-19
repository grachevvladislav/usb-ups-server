package app

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestLANDefaultOnlyForUnsavedConfiguration(t *testing.T) {
	s, _, _ := testServer(t)
	s.localIPs = func() []net.IP { return []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("10.0.0.20")} }
	r := httptest.NewRequest("GET", "https://nas.example:5001/api/settings", nil)
	r.Header.Set("X-UPS-Server-Addr", "192.168.1.20")
	result := s.settings(r, true)
	if result["config"].(Config).Listen != "192.168.1.20" || result["listen_autodetected"] != true {
		t.Fatal(result)
	}
	if s.Manager.Config().Listen != "127.0.0.1" {
		t.Fatal("GET settings changed the active listener")
	}
	c := DefaultConfig()
	if err := saveConfig(filepath.Join(s.Manager.data, "settings.json"), c); err != nil {
		t.Fatal(err)
	}
	result = s.settings(r, true)
	if result["config"].(Config).Listen != "127.0.0.1" || result["listen_autodetected"] != false {
		t.Fatal("saved listener overwritten", result)
	}
	if result["suggested_listen"] != "192.168.1.20" {
		t.Fatal("missing explicit suggestion")
	}
}
func TestLANSelectionDoesNotGuessAcrossInterfaces(t *testing.T) {
	ips := []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("10.0.0.20"), net.ParseIP("127.0.0.1")}
	for _, bad := range []string{"", "0.0.0.0", "192.168.1.99", "127.0.0.1"} {
		if got := chooseLANIPv4(bad, ips); got != "" {
			t.Fatal(bad, got)
		}
	}
	if got := chooseLANIPv4("", []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("192.168.1.20")}); got != "192.168.1.20" {
		t.Fatal(got)
	}
	if got := chooseLANIPv4("", []net.IP{net.ParseIP("169.254.1.1"), net.ParseIP("::1")}); got != "" {
		t.Fatal(got)
	}
}
func TestUSBPermissionDiagnosisIsSpecificAndDoesNotGuess(t *testing.T) {
	c := DefaultConfig()
	c.VendorID = "0925"
	c.ProductID = "1234"
	denied := USBDevice{VendorID: "0925", ProductID: "1234", Access: "denied", Serial: "a"}
	if err := checkUSBPermissions(c, []USBDevice{denied}); err == nil {
		t.Fatal("missing denial")
	} else {
		w := httptest.NewRecorder()
		apiError(w, 422, err)
		var data map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &data); e != nil {
			t.Fatal(e)
		}
		if data["code"] != "usb_permission_denied" || data["command"] != "sudo sh /var/packages/usb-ups-server/target/tools/usb-access.sh install-rule 0925 1234" {
			t.Fatal(data)
		}
	}
	for _, devices := range [][]USBDevice{nil, {{VendorID: "ffff", ProductID: "0000", Access: "denied"}}, {{VendorID: "0925", ProductID: "1234", Access: "unavailable"}}, {denied, {VendorID: "0925", ProductID: "1234", Access: "granted", Serial: "b"}}} {
		if err := checkUSBPermissions(c, devices); err != nil {
			t.Fatal("unsupported access diagnosis", err)
		}
	}
	c.Serial = "b"
	if err := checkUSBPermissions(c, []USBDevice{denied}); err != nil {
		t.Fatal("other serial matched")
	}
}
