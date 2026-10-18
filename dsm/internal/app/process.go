package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (p *child) stop() {
	if p == nil {
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}
func (p *child) alive() bool {
	if p == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// Only children started here are stopped. No global pkill or DSM service changes.
type Manager struct {
	mu                 sync.Mutex
	root, data         string
	cfg                Config
	driver, server     *child
	log                io.Writer
	lastError          string
	failures, restarts int
}

func NewManager(root, data string, log io.Writer) (*Manager, error) {
	for _, d := range []string{data, filepath.Join(data, "conf"), filepath.Join(data, "run")} {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, err
		}
		if err := os.Chmod(d, 0700); err != nil {
			return nil, err
		}
	}
	c, err := readConfig(filepath.Join(data, "settings.json"))
	if err != nil {
		return nil, err
	}
	return &Manager{root: root, data: data, cfg: c, log: log}, nil
}
func (m *Manager) Config() Config { m.mu.Lock(); defer m.mu.Unlock(); return m.cfg }
func (m *Manager) command(conf, run, binary string, args ...string) (*exec.Cmd, error) {
	loaders, err := filepath.Glob(filepath.Join(m.root, "nut/lib/ld-musl-*.so.1"))
	if err != nil || len(loaders) != 1 {
		return nil, errors.New("NUT runtime missing: expected one musl loader")
	}
	bin := filepath.Join(m.root, "nut", binary)
	if _, err = os.Stat(bin); err != nil {
		return nil, err
	}
	lib := filepath.Join(m.root, "nut/lib") + ":" + filepath.Join(m.root, "nut/usr/lib")
	identity, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil {
		return nil, err
	}
	// Alpine defaults to user "nut", which need not exist in DSM.
	args = append(args, "-u", identity.Username)
	if strings.HasPrefix(binary, "usr/lib/nut/") {
		group, e := user.LookupGroupId(strconv.Itoa(os.Getgid()))
		if e != nil {
			return nil, e
		}
		args = append(args, "-g", group.Name)
	}
	a := append([]string{"--library-path", lib, bin}, args...)
	cmd := exec.Command(loaders[0], a...)
	cmd.Env = append(os.Environ(), "NUT_CONFPATH="+conf, "NUT_STATEPATH="+run, "NUT_ALTPIDPATH="+run, "NUT_PIDPATH="+run)
	cmd.Stdout = m.log
	cmd.Stderr = m.log
	return cmd, nil
}
func (m *Manager) spawn(binary string, args ...string) (*child, error) {
	cmd, err := m.command(filepath.Join(m.data, "conf"), filepath.Join(m.data, "run"), binary, args...)
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	p := &child{cmd: cmd, done: make(chan struct{})}
	go func() { err := cmd.Wait(); fmt.Fprintf(m.log, "%s exited: %v\n", binary, err); close(p.done) }()
	return p, nil
}
func (m *Manager) stopLocked() { m.server.stop(); m.driver.stop(); m.server = nil; m.driver = nil }
func (m *Manager) startLocked(ctx context.Context, c Config) error {
	if !c.Enabled {
		return nil
	}
	host := c.Listen
	check, err := net.Listen("tcp4", fmt.Sprintf("%s:%d", host, c.Port))
	if err != nil {
		return fmt.Errorf("NUT port unavailable; check DSM UPS server: %w", err)
	}
	_ = check.Close()
	if err = writeNUT(filepath.Join(m.data, "conf"), filepath.Join(m.data, "run"), c); err != nil {
		return err
	}
	m.driver, err = m.spawn("usr/lib/nut/"+c.Driver, "-a", "ups", "-F")
	if err != nil {
		return err
	}
	m.server, err = m.spawn("usr/sbin/upsd", "-F")
	if err != nil {
		m.stopLocked()
		return err
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			m.stopLocked()
			return ctx.Err()
		case <-deadline.C:
			m.stopLocked()
			return errors.New("UPS did not become ready in 30 seconds; check USB permissions and driver")
		case <-tick.C:
			if !m.driver.alive() || !m.server.alive() {
				m.stopLocked()
				return errors.New("NUT process exited; see logs")
			}
			if _, err = readUPS(ctx, c); err == nil {
				return nil
			}
		}
	}
}
func (m *Manager) Apply(ctx context.Context, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old := m.cfg
	m.stopLocked()
	if err := m.startLocked(ctx, c); err != nil {
		m.lastError = err.Error()
		m.restore(old)
		return err
	}
	if err := saveConfig(filepath.Join(m.data, "settings.json"), c); err != nil {
		m.stopLocked()
		m.restore(old)
		return err
	}
	m.cfg = c
	m.failures = 0
	m.restarts = 0
	m.lastError = ""
	return nil
}
func (m *Manager) restore(c Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	if err := m.startLocked(ctx, c); err != nil {
		m.lastError += "; rollback failed: " + err.Error()
	}
}
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.startLocked(ctx, m.cfg); err != nil {
		m.lastError = err.Error()
		fmt.Fprintln(m.log, err)
	}
}
func (m *Manager) Stop() { m.mu.Lock(); defer m.mu.Unlock(); m.stopLocked() }
func (m *Manager) Health() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{"enabled": m.cfg.Enabled, "driver_running": m.driver.alive(), "server_running": m.server.alive(), "error": m.lastError, "restart_attempts": m.restarts, "protection": "DSM shutdown integration requires hardware verification"}
}
func (m *Manager) Watch(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			if !m.cfg.Enabled {
				m.mu.Unlock()
				continue
			}
			_, err := readUPS(ctx, m.cfg)
			if err == nil && m.driver.alive() && m.server.alive() {
				m.failures = 0
				m.restarts = 0
				m.lastError = ""
				m.mu.Unlock()
				continue
			}
			m.failures++
			if err != nil {
				m.lastError = err.Error()
			}
			if m.failures >= 3 && m.restarts < 3 {
				m.restarts++
				m.stopLocked()
				fmt.Fprintf(m.log, "watchdog: recovery attempt %d\n", m.restarts)
				if e := m.startLocked(ctx, m.cfg); e != nil {
					m.lastError = e.Error()
				}
				// Recovery is confirmed by a later successful poll, not by process creation.
			}
			m.mu.Unlock()
		}
	}
}

type USBDevice struct {
	VendorID  string `json:"vendor_id"`
	ProductID string `json:"product_id"`
	Serial    string `json:"serial"`
	Name      string `json:"name"`
	Path      string `json:"path"`
}

func USBDevices() []USBDevice {
	paths, _ := filepath.Glob("/sys/bus/usb/devices/*")
	out := []USBDevice{}
	for _, p := range paths {
		read := func(n string) string { b, _ := os.ReadFile(filepath.Join(p, n)); return strings.TrimSpace(string(b)) }
		v, id := read("idVendor"), read("idProduct")
		if v == "" || id == "" || v == "1d6b" {
			continue
		}
		out = append(out, USBDevice{v, id, read("serial"), strings.TrimSpace(read("manufacturer") + " " + read("product")), "/dev/bus/usb/" + padUSB(read("busnum")) + "/" + padUSB(read("devnum"))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func padUSB(s string) string {
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

// Detection never rewrites the live configuration and is only allowed while disabled.
func (m *Manager) Detect(ctx context.Context, c Config) (Config, error) {
	if c.VendorID == "" {
		return c, errors.New("select a USB device first")
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.Enabled {
		return c, errors.New("disable monitoring before detection")
	}
	dir, err := os.MkdirTemp(m.data, "probe-")
	if err != nil {
		return c, err
	}
	defer os.RemoveAll(dir)
	candidates := []Config{}
	for _, d := range drivers {
		if d == "nutdrv_qx" {
			x := c
			x.Driver = d
			x.Subdriver = ""
			candidates = append(candidates, x)
			for _, s := range subdrivers {
				x := c
				x.Driver = d
				x.Subdriver = s
				candidates = append(candidates, x)
			}
		} else {
			x := c
			x.Driver = d
			x.Subdriver = ""
			candidates = append(candidates, x)
		}
	}
	for _, x := range candidates {
		if ctx.Err() != nil {
			return c, ctx.Err()
		}
		if err = writeNUT(dir, dir, x); err != nil {
			return c, err
		}
		cmd, e := m.command(dir, dir, "usr/lib/nut/"+x.Driver, "-a", "ups", "-DD")
		if e != nil {
			continue
		}
		output := &boundedBuffer{limit: 128 * 1024}
		cmd.Stdout = output
		cmd.Stderr = output
		if e = cmd.Start(); e != nil {
			continue
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		timer := time.NewTimer(15 * time.Second)
		select {
		case <-done:
		case <-timer.C:
			_ = cmd.Process.Kill()
			<-done
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-done
		}
		timer.Stop()
		fmt.Fprintf(m.log, "probe %s/%s completed\n", x.Driver, x.Subdriver)
		if strings.Contains(output.String(), "Driver initialization completed") {
			x.Enabled = false
			return x, nil
		}
	}
	return c, errors.New("no working driver found; verify USB permissions and that DSM is not using the device")
}

type boundedBuffer struct {
	mu    sync.Mutex
	b     []byte
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	b.b = append(b.b, p...)
	if len(b.b) > b.limit {
		b.b = b.b[len(b.b)-b.limit:]
	}
	return n, nil
}
func (b *boundedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.b) }
