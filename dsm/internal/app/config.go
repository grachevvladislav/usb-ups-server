package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
)

var drivers = []string{"usbhid-ups", "nutdrv_qx", "blazer_usb", "richcomm_usb", "nutdrv_atcl_usb", "tripplite_usb", "riello_usb", "bcmxcp_usb"}
var subdrivers = []string{"armac", "cypress", "ippon", "krauler", "phoenix", "fabula", "hunnox", "sgs", "snr", "ablerex"}
var hexID = regexp.MustCompile(`^[0-9a-fA-F]{4}$`)
var serialID = regexp.MustCompile(`^[a-zA-Z0-9 ._:+/-]{1,128}$`)

type Config struct {
	Enabled      bool   `json:"enabled"`
	Driver       string `json:"driver"`
	Subdriver    string `json:"subdriver"`
	VendorID     string `json:"vendor_id"`
	ProductID    string `json:"product_id"`
	Serial       string `json:"serial"`
	Listen       string `json:"listen"`
	Port         int    `json:"port"`
	PollInterval int    `json:"poll_interval"`
}

func DefaultConfig() Config {
	return Config{Driver: "usbhid-ups", Listen: "127.0.0.1", Port: 3493, PollInterval: 5}
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func (c Config) Validate() error {
	if !contains(drivers, c.Driver) {
		return errors.New("unsupported driver")
	}
	if c.Subdriver != "" && (c.Driver != "nutdrv_qx" || !contains(subdrivers, c.Subdriver)) {
		return errors.New("unsupported subdriver")
	}
	if (c.VendorID == "") != (c.ProductID == "") {
		return errors.New("specify both USB IDs")
	}
	if c.VendorID != "" && (!hexID.MatchString(c.VendorID) || !hexID.MatchString(c.ProductID)) {
		return errors.New("USB IDs must contain four hexadecimal digits")
	}
	if c.Subdriver != "" && c.VendorID == "" {
		return errors.New("subdriver requires USB IDs")
	}
	if c.Serial != "" && !serialID.MatchString(c.Serial) {
		return errors.New("invalid serial number")
	}
	if net.ParseIP(c.Listen) == nil || net.ParseIP(c.Listen).To4() == nil {
		return errors.New("listen must be an IPv4 address")
	}
	if c.Port < 1024 || c.Port > 65535 {
		return errors.New("port must be 1024–65535")
	}
	if c.PollInterval < 2 || c.PollInterval > 60 {
		return errors.New("poll interval must be 2–60 seconds")
	}
	return nil
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".ups-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	return err
}

func saveConfig(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'))
}
func readConfig(path string) (Config, error) {
	c := DefaultConfig()
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	if e == nil {
		e = c.Validate()
	}
	return c, e
}

func writeNUT(dir, state string, c Config) error {
	text := fmt.Sprintf("pollinterval = %d\nmaxretry = 1\n[ups]\n  driver = %s\n  port = auto\n  desc = \"USB UPS\"\n", c.PollInterval, c.Driver)
	for _, p := range [][2]string{{"subdriver", c.Subdriver}, {"vendorid", c.VendorID}, {"productid", c.ProductID}, {"serial", c.Serial}} {
		if p[1] != "" {
			text += fmt.Sprintf("  %s = \"%s\"\n", p[0], p[1])
		}
	}
	for name, body := range map[string]string{
		"ups.conf":   text,
		"upsd.conf":  fmt.Sprintf("LISTEN %s %d\nMAXAGE 25\nSTATEPATH %s\n", c.Listen, c.Port, state),
		"upsd.users": "[monuser]\n  password = secret\n  upsmon secondary\n",
	} {
		if err := atomicWrite(filepath.Join(dir, name), []byte(body)); err != nil {
			return err
		}
	}
	return nil
}
