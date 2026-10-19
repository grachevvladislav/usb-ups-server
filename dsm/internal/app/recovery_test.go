package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNotificationTestRequiresAuthAndReportsDeliveryFailure(t *testing.T) {
	s, h, token := testServer(t)
	calls := 0
	s.Manager.notify = func(_ context.Context, event string) error {
		if event != "test" {
			t.Fatal(event)
		}
		calls++
		return errors.New("permission denied")
	}
	if w := request(h, "POST", "/api/test-notification", "{}", "", map[string]string{"X-UPS-Request": "1"}); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if calls != 0 {
		t.Fatal("unauthenticated notification")
	}
	w := request(h, "POST", "/api/test-notification", "{}", token, map[string]string{"X-UPS-Request": "1"})
	if w.Code != 502 || !strings.Contains(w.Body.String(), "permission denied") || calls != 1 {
		t.Fatal(w.Code, w.Body.String(), calls)
	}
}

func recoveryManager(t *testing.T) (*Manager, Config) {
	t.Helper()
	m, err := NewManager(fakeRuntime(t), t.TempDir(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	c := DefaultConfig()
	c.Enabled = true
	c.Port = freePort(t)
	c.VendorID = "0925"
	c.ProductID = "1234"
	m.devices = func() []USBDevice {
		return []USBDevice{{VendorID: c.VendorID, ProductID: c.ProductID, Path: "/dev/bus/usb/005/004", Access: "granted"}}
	}
	if err = m.Apply(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return m, c
}

func TestUSBDisconnectReconnectKeepsServerAndNotifiesOnce(t *testing.T) {
	m, _ := recoveryManager(t)
	ctx := context.Background()
	now := time.Now()
	serverPID := m.server.cmd.Process.Pid
	if event := m.recoverStep(ctx, now); event != "" {
		t.Fatal(event)
	}
	m.devices = func() []USBDevice { return nil }
	if event := m.recoverStep(ctx, now.Add(time.Second)); event != "" {
		t.Fatal(event)
	}
	if m.driver.alive() || !m.server.alive() {
		t.Fatal("disconnect stopped the server or kept the driver")
	}
	status := m.Status(ctx)
	if status["state"] != "usb_disconnected" || status["variables"] != nil || status["ok"] != false {
		t.Fatal(status)
	}
	if event := m.recoverStep(ctx, now.Add(12*time.Second)); event != "lost" {
		t.Fatalf("notification: %q", event)
	}
	if event := m.recoverStep(ctx, now.Add(time.Hour)); event != "" {
		t.Fatalf("repeated alert: %q", event)
	}
	// The USB node changes on replug; a persistent rule may still be missing.
	m.devices = func() []USBDevice {
		return []USBDevice{{VendorID: "0925", ProductID: "1234", Path: "/dev/bus/usb/005/005", Access: "denied"}}
	}
	m.recoverStep(ctx, now.Add(time.Hour+time.Second))
	if m.driver.alive() || m.Status(ctx)["state"] != "usb_permission_denied" {
		t.Fatal("permission failure was ignored")
	}
	m.devices = func() []USBDevice {
		return []USBDevice{{VendorID: "0925", ProductID: "1234", Path: "/dev/bus/usb/005/005", Access: "granted"}}
	}
	m.recoverStep(ctx, now.Add(time.Hour+10*time.Second))
	if event := m.recoverStep(ctx, now.Add(time.Hour+20*time.Second)); event != "restored" {
		t.Fatalf("restore: %q", event)
	}
	if event := m.recoverStep(ctx, now.Add(time.Hour+30*time.Second)); event != "" {
		t.Fatal("repeated restore", event)
	}
	if m.server.cmd.Process.Pid != serverPID || !m.server.alive() {
		t.Fatal("server was replaced during USB recovery")
	}
	if m.Status(ctx)["ok"] != true || !m.Config().Enabled {
		t.Fatal("monitoring did not recover automatically")
	}
}

func TestRecoveryContinuesAfterMoreThanThreeFailures(t *testing.T) {
	m, _ := recoveryManager(t)
	ctx := context.Background()
	now := time.Now()
	serverPID := m.server.cmd.Process.Pid
	m.driver.stop()
	m.driver = nil
	binary := filepath.Join(m.root, "nut/usr/lib/nut/usbhid-ups")
	contents, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		m.recoverStep(ctx, now.Add(time.Duration(i)*70*time.Second))
	}
	if m.restarts != 7 {
		t.Fatalf("recovery stopped after %d attempts", m.restarts)
	}
	if m.server.cmd.Process.Pid != serverPID {
		t.Fatal("server restarted")
	}
	if err = os.WriteFile(binary, contents, 0700); err != nil {
		t.Fatal(err)
	}
	m.recoverStep(ctx, now.Add(8*70*time.Second))
	if event := m.recoverStep(ctx, now.Add(9*70*time.Second)); event != "restored" {
		t.Fatal(event)
	}
	if m.Status(ctx)["ok"] != true {
		t.Fatal("failed to recover after long outage")
	}
	if retryDelay(1000000) != time.Minute {
		t.Fatal("retry delay is not capped")
	}
}

func TestLiveStaleDriverIsReplacedWithoutStoppingServer(t *testing.T) {
	m, _ := recoveryManager(t)
	ctx := context.Background()
	now := time.Now()
	serverPID, driverPID := m.server.cmd.Process.Pid, m.driver.cmd.Process.Pid
	if err := atomicWrite(filepath.Join(m.data, "conf/test-response"), []byte("ERR DATA-STALE\n")); err != nil {
		t.Fatal(err)
	}
	m.recoverStep(ctx, now)
	if m.driver.cmd.Process.Pid != driverPID {
		t.Fatal("driver killed before grace period")
	}
	status := m.Status(ctx)
	if status["ok"] != false || status["variables"] != nil || strings.Contains(status["error"].(string), "ERR") {
		t.Fatal(status)
	}
	m.recoverStep(ctx, now.Add(35*time.Second))
	if m.driver.alive() {
		t.Fatal("stale driver was not stopped")
	}
	m.recoverStep(ctx, now.Add(45*time.Second))
	if m.driver.cmd.Process.Pid == driverPID || m.server.cmd.Process.Pid != serverPID {
		t.Fatal("wrong process was restarted")
	}
}

func TestServerFailureDoesNotReplaceHealthyUSBDriver(t *testing.T) {
	m, _ := recoveryManager(t)
	driverPID := m.driver.cmd.Process.Pid
	m.server.stop()
	m.server = nil
	if err := os.Remove(filepath.Join(m.root, "nut/usr/sbin/upsd")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 5; i++ {
		m.recoverStep(context.Background(), now.Add(time.Duration(i)*70*time.Second))
	}
	if !m.driver.alive() || m.driver.cmd.Process.Pid != driverPID {
		t.Fatal("server failure restarted USB driver")
	}
	if m.Health()["connection_state"] != "server_unavailable" {
		t.Fatal(m.Health())
	}
}

func TestStartupWithoutUSBAndStopSuppressesRecovery(t *testing.T) {
	m, c := recoveryManager(t)
	m.Stop()
	m.devices = func() []USBDevice { return nil }
	m.Start(context.Background())
	m.recoverStep(context.Background(), time.Now())
	if !m.server.alive() || m.driver.alive() {
		t.Fatal("startup without USB must keep only the server")
	}
	if m.Config() != c {
		t.Fatal("startup rewrote configuration")
	}
	m.Stop()
	m.recoverStep(context.Background(), time.Now().Add(time.Hour))
	if m.server.alive() || m.driver.alive() {
		t.Fatal("watchdog restarted a stopped manager")
	}
}
