package app

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The configured USB identity survives enumeration changes. Never pin a bus/device
// number in ups.conf and never reset a controller shared with other USB devices.
func (m *Manager) usbState(c Config) (string, string) {
	if c.VendorID == "" || c.ProductID == "" {
		return "", ""
	}
	var paths []string
	denied := true
	for _, d := range m.devices() {
		if !strings.EqualFold(d.VendorID, c.VendorID) || !strings.EqualFold(d.ProductID, c.ProductID) || (c.Serial != "" && d.Serial != c.Serial) {
			continue
		}
		paths = append(paths, d.Path)
		if d.Access != "denied" {
			denied = false
		}
	}
	if len(paths) == 0 {
		return "", "usb_disconnected"
	}
	if denied {
		return strings.Join(paths, ","), "usb_permission_denied"
	}
	return strings.Join(paths, ","), ""
}

func connectionMessage(state string) string {
	switch state {
	case "usb_disconnected":
		return "USB-кабель ИБП отключён или устройство недоступно. После подключения мониторинг восстановится автоматически."
	case "usb_permission_denied":
		return "Нет прав доступа к USB-устройству ИБП. Откройте настройки и обновите список USB для получения инструкции."
	case "server_unavailable":
		return "NUT-сервер недоступен. Выполняется автоматическое восстановление."
	default:
		return "Нет свежих данных от ИБП. Выполняется автоматическое восстановление связи."
	}
}

func retryDelay(attempt int) time.Duration {
	if attempt > 4 {
		return time.Minute
	}
	if attempt < 1 {
		attempt = 1
	}
	return time.Duration(5<<(attempt-1)) * time.Second
}

// One bounded recovery step, without a readiness wait. upsd stays alive while
// its driver is absent/stale, so clients receive NUT's real error, never fake OL/OB.
func (m *Manager) recoverStep(ctx context.Context, now time.Time) (event string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped || !m.cfg.Enabled || ctx.Err() != nil {
		return ""
	}
	healthy := false
	defer func() {
		if healthy {
			m.lastSuccess = now
			m.lostSince = time.Time{}
			if m.outageNotified {
				event = "restored"
				m.outageNotified = false
			}
		} else {
			if m.lostSince.IsZero() {
				m.lostSince = now
			}
			if !m.outageNotified && now.Sub(m.lostSince) >= 10*time.Second {
				event = "lost"
				m.outageNotified = true
			}
		}
	}()
	// Restart only our dead server. Never stop a healthy server for USB recovery.
	if !m.server.alive() && !now.Before(m.serverNextAttempt) {
		m.serverRestarts++
		m.serverNextAttempt = now.Add(retryDelay(m.serverRestarts))
		var err error
		m.server, err = m.spawn("usr/sbin/upsd", "-F")
		if err != nil {
			m.lastError = err.Error()
		}
	} else if m.server.alive() {
		m.serverRestarts = 0
	}
	path, usbError := m.usbState(m.cfg)
	if usbError != "" {
		m.driver.stop()
		m.driver = nil
		m.usbPath = path
		m.connectionState = usbError
		m.lastError = connectionMessage(usbError)
		return ""
	}
	if path != m.usbPath {
		m.driver.stop()
		m.driver = nil
		m.usbPath = path
		m.nextAttempt = now.Add(5 * time.Second)
		// A new USB enumeration deserves a prompt retry, even after a long outage.
		m.restarts = 0
	}
	if !m.server.alive() {
		m.connectionState = "server_unavailable"
		return ""
	}
	if m.driver.alive() && m.server.alive() {
		if _, err := readUPS(ctx, m.cfg); err == nil {
			healthy = true
			m.failures = 0
			m.restarts = 0
			m.lastError = ""
			m.connectionState = "connected"
			m.nextAttempt = time.Time{}
			return ""
		} else {
			m.lastError = err.Error()
		}
		// Let the driver initialize/reconnect before replacing a live process.
		if now.Sub(m.driverStarted) < 30*time.Second || m.lostSince.IsZero() || now.Sub(m.lostSince) < 20*time.Second {
			m.connectionState = "reconnecting"
			return ""
		}
	}
	m.connectionState = "reconnecting"
	if !m.server.alive() {
		m.connectionState = "server_unavailable"
	}
	if now.Before(m.nextAttempt) {
		return ""
	}
	if m.driver.alive() {
		m.driver.stop()
		m.driver = nil
		m.nextAttempt = now.Add(retryDelay(m.restarts + 1))
		fmt.Fprintln(m.log, "watchdog: stopped stale driver; waiting before reconnect")
		return ""
	}
	m.driver.stop()
	m.driver = nil
	m.restarts++
	m.nextAttempt = now.Add(retryDelay(m.restarts))
	fmt.Fprintf(m.log, "watchdog: driver recovery attempt %d; next retry no earlier than %s\n", m.restarts, m.nextAttempt.UTC().Format(time.RFC3339))
	var err error
	m.driver, err = m.spawn("usr/lib/nut/"+m.cfg.Driver, "-a", "ups", "-F")
	m.driverStarted = now
	if err != nil {
		m.lastError = err.Error()
	}
	return ""
}

func (m *Manager) Status(ctx context.Context) map[string]any {
	c := m.Config()
	health := m.Health()
	result := map[string]any{"health": health, "ok": false, "state": "disabled"}
	if !c.Enabled {
		return result
	}
	path, state := m.usbState(c)
	if state == "" && path != health["usb_path"] {
		state = "reconnecting"
	}
	if state == "" && health["driver_running"] != true {
		state = "reconnecting"
	}
	if state == "" {
		vars, err := readUPS(ctx, c)
		if err == nil {
			result["ok"] = true
			result["state"] = "connected"
			result["variables"] = vars
			result["observed_at"] = time.Now().UTC()
			return result
		}
		result["detail"] = err.Error()
		state = "reconnecting"
		if health["server_running"] != true {
			state = "server_unavailable"
		}
	}
	result["state"] = state
	result["error"] = connectionMessage(state)
	return result
}
