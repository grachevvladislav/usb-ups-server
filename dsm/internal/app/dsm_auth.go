package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"time"
	"unicode"
)

var errDSMAdmin = errors.New("доступ разрешён только администраторам DSM")

type dsmAuthenticator struct {
	mu         sync.Mutex
	cgiPath    string
	isAdmin    func(string) bool
	digest     [32]byte
	username   string
	validUntil time.Time
}

func newDSMAuthenticator() *dsmAuthenticator {
	return &dsmAuthenticator{
		cgiPath: "/usr/syno/synoman/webman/modules/authenticate.cgi",
		isAdmin: isDSMAdministrator,
	}
}

func isDSMAdministrator(name string) bool {
	u, err := user.Lookup(name)
	if err != nil {
		return false
	}
	group, err := user.LookupGroup("administrators")
	if err != nil {
		return false
	}
	groups, err := u.GroupIds()
	if err != nil {
		return false
	}
	for _, gid := range groups {
		if gid == group.Gid {
			return true
		}
	}
	return false
}

type authOutput struct{ buffer bytes.Buffer }

func (b *authOutput) String() string { return b.buffer.String() }

func (b *authOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 256 {
		return 0, fmt.Errorf("oversized DSM authentication output")
	}
	return b.buffer.Write(p)
}

// DSM's official CGI validates the session; neither a username supplied by the
// browser nor possession of our old standalone token authorizes the DSM API.
func (a *dsmAuthenticator) authenticate(r *http.Request) (string, error) {
	cookie := r.Header.Get("Cookie")
	token := r.Header.Get("X-SYNO-TOKEN")
	clientIP := r.Header.Get("X-Real-IP")
	serverIP := r.Header.Get("X-UPS-Server-Addr")
	if token == "" || len(token) > 1024 || strings.ContainsAny(token, "\r\n\x00") || cookie == "" || len(cookie) > 8192 || strings.ContainsAny(cookie, "\r\n\x00") || net.ParseIP(clientIP) == nil || net.ParseIP(serverIP) == nil {
		return "", errors.New("сессия DSM не подтверждена")
	}
	key := sha256.Sum256([]byte(cookie + "\x00" + clientIP + "\x00" + r.UserAgent() + "\x00" + r.Header.Get("X-SYNO-TOKEN")))
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.username != "" && key == a.digest && time.Now().Before(a.validUntil) {
		return a.username, nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.cgiPath)
	_, port, _ := net.SplitHostPort(r.Host)
	if port == "" {
		port = "443"
	}
	cmd.Env = append(os.Environ(),
		"HTTP_COOKIE="+cookie,
		"REMOTE_ADDR="+clientIP,
		"SERVER_ADDR="+serverIP,
		"SERVER_PORT="+port,
		"HTTP_HOST="+r.Host,
		"HTTP_USER_AGENT="+r.UserAgent(),
		"HTTP_X_SYNO_TOKEN="+r.Header.Get("X-SYNO-TOKEN"),
		"REQUEST_METHOD=GET", "REQUEST_URI=/webman/", "QUERY_STRING=SynoToken="+url.QueryEscape(token), "HTTPS=on")
	output := &authOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("проверка сессии DSM недоступна: %w", err)
	}
	name := strings.TrimSpace(output.String())
	if name == "" || len(name) > 128 || strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", errors.New("сессия DSM не подтверждена")
	}
	if !a.isAdmin(name) {
		return "", errDSMAdmin
	}
	a.digest, a.username, a.validUntil = key, name, time.Now().Add(5*time.Second)
	return name, nil
}
