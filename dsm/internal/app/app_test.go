package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, http.Handler, string) {
	t.Helper()
	m, e := NewManager(t.TempDir(), t.TempDir(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewServer(m, false, &boundedBuffer{limit: 1024})
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(m.data, "admin-token"))
	if e != nil {
		t.Fatal(e)
	}
	return s, s.Handler(), string(b)
}
func request(h http.Handler, method, path, body, token string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAuthAndCSRF(t *testing.T) {
	_, h, token := testServer(t)
	for _, p := range []string{"status", "settings", "usb", "logs", "diagnostics", "metrics"} {
		if w := request(h, "GET", "/api/"+p, "", "", nil); w.Code != 401 {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
	body := fmt.Sprintf(`{"token":%q}`, token)
	if w := request(h, "POST", "/api/login", body, "", nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
	headers := map[string]string{"X-UPS-Request": "1", "Origin": "https://evil.example"}
	if w := request(h, "POST", "/api/login", body, "", headers); w.Code != 403 {
		t.Fatal(w.Code)
	}
	delete(headers, "Origin")
	login := request(h, "POST", "/api/login", body, "", headers)
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	w := request(h, "GET", "/api/settings", "", "", map[string]string{"Cookie": cookies[0].String()})
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = request(h, "GET", "/api/diagnostics", "", token, nil)
	if strings.Contains(w.Body.String(), token) {
		t.Fatal("diagnostics leaked token")
	}
}

func TestSettingsValidationAndPersistence(t *testing.T) {
	s, h, token := testServer(t)
	headers := map[string]string{"X-UPS-Request": "1"}
	c := DefaultConfig()
	c.Port = 4400
	b, _ := json.Marshal(c)
	w := request(h, "POST", "/api/settings", string(b), token, headers)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	saved, e := readConfig(filepath.Join(s.Manager.data, "settings.json"))
	if e != nil || saved.Port != 4400 {
		t.Fatal(saved, e)
	}
	c.Driver = "../../bin/sh"
	b, _ = json.Marshal(c)
	w = request(h, "POST", "/api/settings", string(b), token, headers)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	if s.Manager.Config().Port != 4400 {
		t.Fatal("invalid configuration was applied")
	}
	for _, b := range []string{`{"enabled":false,"unknown":1}`, `{} {}`} {
		if w := request(h, "POST", "/api/settings", b, token, headers); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	st, _ := os.Stat(filepath.Join(s.Manager.data, "settings.json"))
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
}

func TestRejectConfigInjection(t *testing.T) {
	tests := []func(*Config){func(c *Config) { c.Serial = "foo\nuser = root" }, func(c *Config) { c.VendorID = ".*"; c.ProductID = "1234" }, func(c *Config) { c.Listen = "127.0.0.1\nLISTEN 0.0.0.0" }, func(c *Config) { c.Subdriver = "armac" }, func(c *Config) { c.Port = 80 }, func(c *Config) { c.PollInterval = 0 }}
	for i, mutate := range tests {
		c := DefaultConfig()
		mutate(&c)
		if c.Validate() == nil {
			t.Fatalf("accepted injection/invalid config %d", i)
		}
	}
}

func TestNUTProtocol(t *testing.T) {
	for _, tc := range []struct {
		response string
		ok       bool
	}{
		{"BEGIN LIST VAR ups\nVAR ups ups.status \"OL\"\nVAR ups ups.model \"<script> & \\\"quoted\\\"\"\nEND LIST VAR ups\n", true},
		{"ERR DATA-STALE\n", false},
		{"BEGIN LIST VAR ups\nVAR ups ups.status \"OL\"\n", false},
		{"BEGIN LIST VAR ups\nEND LIST VAR ups\n", false},
		{"BEGIN LIST VAR ups\nVAR ups ups.status \"WAIT\"\nEND LIST VAR ups\n", false},
	} {
		ln, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		go func() {
			conn, e := ln.Accept()
			if e == nil {
				defer conn.Close()
				_, _ = bufio.NewReader(conn).ReadString('\n')
				_, _ = io.WriteString(conn, tc.response)
			}
		}()
		c := DefaultConfig()
		c.Port = ln.Addr().(*net.TCPAddr).Port
		v, e := readUPS(context.Background(), c)
		ln.Close()
		if tc.ok {
			if e != nil || v["ups.status"] != "OL" {
				t.Fatal(v, e)
			}
		} else if e == nil {
			t.Fatal("invalid/stale data accepted")
		}
	}
}

// Executed as a child process by the manager tests; no USB or root needed.
func TestNUTChild(t *testing.T) {
	role := os.Getenv("UPS_TEST_ROLE")
	if role == "" {
		return
	}
	if role == "driver" {
		for {
			time.Sleep(time.Hour)
		}
	}
	b, e := os.ReadFile(filepath.Join(os.Getenv("NUT_CONFPATH"), "upsd.conf"))
	if e != nil {
		os.Exit(2)
	}
	fields := strings.Fields(string(b))
	ln, e := net.Listen("tcp4", net.JoinHostPort(fields[1], fields[2]))
	if e != nil {
		os.Exit(2)
	}
	for {
		c, e := ln.Accept()
		if e != nil {
			os.Exit(2)
		}
		go func(c net.Conn) {
			defer c.Close()
			_, _ = bufio.NewReader(c).ReadString('\n')
			_, _ = io.WriteString(c, "BEGIN LIST VAR ups\nVAR ups ups.status \"OL\"\nEND LIST VAR ups\n")
		}(c)
	}
}

func fakeRuntime(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"nut/lib", "nut/usr/sbin", "nut/usr/lib/nut"} {
		if e := os.MkdirAll(filepath.Join(root, d), 0700); e != nil {
			t.Fatal(e)
		}
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	write := func(p, s string) {
		if e := os.WriteFile(filepath.Join(root, p), []byte(s), 0700); e != nil {
			t.Fatal(e)
		}
	}
	write("nut/lib/ld-musl-test.so.1", "#!/bin/sh\nshift 2\nexec \"$@\"\n")
	for p, role := range map[string]string{"nut/usr/lib/nut/usbhid-ups": "driver", "nut/usr/sbin/upsd": "server"} {
		write(p, fmt.Sprintf("#!/bin/sh\nexport UPS_TEST_ROLE=%s\nexec '%s' -test.run='^TestNUTChild$' -- \"$@\"\n", role, strings.ReplaceAll(exe, "'", "'\\''")))
	}
	return root
}
func freePort(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestApplyRollsBackAndStopsOnlyChildren(t *testing.T) {
	m, e := NewManager(fakeRuntime(t), t.TempDir(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Stop()
	c := DefaultConfig()
	c.Enabled = true
	c.Port = freePort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e = m.Apply(ctx, c); e != nil {
		t.Fatal(e)
	}
	bad := c
	bad.Driver = "richcomm_usb" // allowed but absent from fake runtime
	if e = m.Apply(ctx, bad); e == nil {
		t.Fatal("failed driver accepted")
	}
	if m.Config() != c {
		t.Fatal("configuration did not roll back")
	}
	if _, e = readUPS(ctx, c); e != nil {
		t.Fatal("previous service was not restored", e)
	}
	// An unrelated listener must survive failure and package shutdown.
	unrelated, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer unrelated.Close()
	bad = c
	bad.Port = unrelated.Addr().(*net.TCPAddr).Port
	if e = m.Apply(ctx, bad); e == nil {
		t.Fatal("occupied port accepted")
	}
	m.Stop()
	conn, e := net.Dial("tcp", unrelated.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	conn.Close()
	if m.driver.alive() || m.server.alive() {
		t.Fatal("children left running")
	}
}

func TestCertificateAndLoginLimit(t *testing.T) {
	dir := t.TempDir()
	if e := ensureCertificate(dir); e != nil {
		t.Fatal(e)
	}
	key, _ := os.ReadFile(filepath.Join(dir, "server.key"))
	if e := ensureCertificate(dir); e != nil {
		t.Fatal(e)
	}
	again, _ := os.ReadFile(filepath.Join(dir, "server.key"))
	if string(key) != string(again) {
		t.Fatal("certificate changed on restart")
	}
	st, _ := os.Stat(filepath.Join(dir, "server.key"))
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	_, h, _ := testServer(t)
	for i := 0; i < 10; i++ {
		w := request(h, "POST", "/api/login", `{"token":"bad"}`, "", map[string]string{"X-UPS-Request": "1"})
		if w.Code != 401 {
			t.Fatal(i, w.Code)
		}
	}
	if w := request(h, "POST", "/api/login", `{}`, "", map[string]string{"X-UPS-Request": "1"}); w.Code != 429 {
		t.Fatal(w.Code)
	}
}

func TestConfigFileEscaping(t *testing.T) {
	dir := t.TempDir()
	c := DefaultConfig()
	c.Serial = "ABC 123"
	if e := writeNUT(dir, "/tmp/private", c); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "ups.conf"))
	if !strings.Contains(string(b), `serial = "ABC 123"`) {
		t.Fatal(string(b))
	}
	b, _ = os.ReadFile(filepath.Join(dir, "upsd.conf"))
	if !strings.Contains(string(b), "LISTEN 127.0.0.1 "+strconv.Itoa(c.Port)) {
		t.Fatal(string(b))
	}
}

// Enable in a plain glibc Linux container to prove that the bundled musl/NUT
// runtime works without Alpine libraries or root. Uses NUT's dummy UPS driver.
func TestBundledRuntime(t *testing.T) {
	root := os.Getenv("UPS_RUNTIME_ROOT")
	if root == "" {
		t.Skip("set UPS_RUNTIME_ROOT to an extracted SPK target")
	}
	data := t.TempDir()
	m, e := NewManager(root, data, os.Stdout)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Stop()
	c := DefaultConfig()
	c.Port = freePort(t)
	conf := filepath.Join(data, "conf")
	run := filepath.Join(data, "run")
	if e = writeNUT(conf, run, c); e != nil {
		t.Fatal(e)
	}
	seq := filepath.Join(conf, "test.dev")
	if e = atomicWrite(seq, []byte("ups.status: OL\nbattery.charge: 87\nups.model: Simulated UPS\n")); e != nil {
		t.Fatal(e)
	}
	if e = atomicWrite(filepath.Join(conf, "ups.conf"), []byte("[ups]\n driver = dummy-ups\n port = "+seq+"\n")); e != nil {
		t.Fatal(e)
	}
	m.driver, e = m.spawn("usr/lib/nut/dummy-ups", "-a", "ups", "-F")
	if e != nil {
		t.Fatal(e)
	}
	m.server, e = m.spawn("usr/sbin/upsd", "-F")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var vars map[string]string
	for ctx.Err() == nil {
		vars, e = readUPS(ctx, c)
		if e == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if e != nil {
		t.Fatal(e)
	}
	if vars["battery.charge"] != "87" || vars["ups.status"] != "OL" {
		t.Fatal(vars)
	}
	m.cfg = c
	m.cfg.Enabled = true
	s, e := NewServer(m, false, &boundedBuffer{limit: 1024})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(data, "admin-token"))
	w := request(s.Handler(), "GET", "/api/status", "", string(b), nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatal(w.Body.String())
	}
	m.driver.stop()
	for i := 0; i < 25; i++ {
		_, e = readUPS(ctx, c)
		if e != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if e == nil {
		t.Fatal("stopped driver still reported healthy")
	}
}
