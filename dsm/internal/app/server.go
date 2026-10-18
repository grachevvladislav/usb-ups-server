package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var assets embed.FS

type Server struct {
	Manager    *Manager
	tokenHash  [32]byte
	session    string
	secure     bool
	logs       *boundedBuffer
	attemptsMu sync.Mutex
	attempts   map[string]attempt
	operation  chan struct{}
	dsmAuth    *dsmAuthenticator
}
type attempt struct {
	count int
	since time.Time
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func NewServer(m *Manager, secure bool, logs *boundedBuffer) (*Server, error) {
	path := filepath.Join(m.data, "admin-token")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		b = []byte(randomToken())
		err = atomicWrite(path, b)
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) != 64 {
		return nil, fmt.Errorf("invalid admin-token file")
	}
	return &Server{Manager: m, tokenHash: sha256.Sum256([]byte(strings.TrimSpace(string(b)))), session: randomToken(), secure: secure, logs: logs, attempts: map[string]attempt{}, operation: make(chan struct{}, 1), dsmAuth: newDSMAuthenticator()}, nil
}
func (s *Server) validToken(token string) bool {
	hash := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(hash[:], s.tokenHash[:]) == 1
}
func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, status int, e error) {
	jsonReply(w, status, map[string]any{"ok": false, "error": e.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}
	return nil
}
func (s *Server) allowLogin(r *http.Request) bool {
	s.attemptsMu.Lock()
	defer s.attemptsMu.Unlock()
	now := time.Now()
	for k, a := range s.attempts {
		if now.Sub(a.since) > time.Minute {
			delete(s.attempts, k)
		}
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	a := s.attempts[ip]
	if a.count == 0 {
		a.since = now
	}
	a.count++
	if len(s.attempts) >= 1024 && a.count == 1 {
		return false
	}
	s.attempts[ip] = a
	return a.count <= 10
}
func (s *Server) Handler() http.Handler {
	return s.handler(false)
}

const DSMProxyPath = "/webman/3rdparty/usb-ups-server/app/"

type dsmUserKey struct{}

// DSMHandler is served exclusively on a loopback listener behind DSM nginx.
// The proxy preserves the external Host and overwrites X-Forwarded-Proto.
func (s *Server) DSMHandler() http.Handler {
	return s.handler(true)
}

func (s *Server) handler(dsmProxy bool) http.Handler {
	cookiePath := "/api/"
	if dsmProxy {
		cookiePath = DSMProxyPath + "api/"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/metrics", s.metrics)
	page := func(w http.ResponseWriter, r *http.Request) {
		if dsmProxy {
			http.NotFound(w, r)
			return
		}
		b, _ := assets.ReadFile("index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	}
	mux.HandleFunc("GET /{$}", page)
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		name, _ := r.Context().Value(dsmUserKey{}).(string)
		jsonReply(w, 200, map[string]any{"username": name, "dsm": dsmProxy})
	})
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		if !s.allowLogin(r) {
			apiError(w, 429, fmt.Errorf("too many login attempts; wait one minute"))
			return
		}
		var req struct {
			Token string `json:"token"`
		}
		if e := decode(w, r, &req); e != nil {
			apiError(w, 400, e)
			return
		}
		if !s.validToken(req.Token) {
			apiError(w, 401, fmt.Errorf("invalid token"))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "ups_session", Value: s.session, Path: cookiePath, HttpOnly: true, Secure: s.secure || dsmProxy, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
		jsonReply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "ups_session", Path: cookiePath, MaxAge: -1, HttpOnly: true, Secure: s.secure || dsmProxy, SameSite: http.SameSiteStrictMode})
		jsonReply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		jsonReply(w, 200, map[string]any{"config": s.Manager.Config(), "drivers": drivers, "subdrivers": subdrivers})
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		c := s.Manager.Config()
		result := map[string]any{"health": s.Manager.Health(), "ok": false}
		if c.Enabled {
			v, e := readUPS(r.Context(), c)
			if e != nil {
				result["error"] = e.Error()
			} else {
				result["ok"] = true
				result["variables"] = v
				result["observed_at"] = time.Now().UTC()
			}
		}
		jsonReply(w, 200, result)
	})
	mux.HandleFunc("GET /api/usb", func(w http.ResponseWriter, r *http.Request) { jsonReply(w, 200, USBDevices()) })
	mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, r *http.Request) {
		jsonReply(w, 200, map[string]string{"text": s.logs.String()})
	})
	mux.HandleFunc("GET /api/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="ups-diagnostics.json"`)
		jsonReply(w, 200, map[string]any{"version": Version, "config": s.Manager.Config(), "health": s.Manager.Health(), "usb": USBDevices(), "logs": s.logs.String()})
	})
	for _, op := range []string{"settings", "detect"} {
		op := op
		mux.HandleFunc("POST /api/"+op, func(w http.ResponseWriter, r *http.Request) {
			select {
			case s.operation <- struct{}{}:
				defer func() { <-s.operation }()
			default:
				apiError(w, 409, fmt.Errorf("another operation is running"))
				return
			}
			var c Config
			if e := decode(w, r, &c); e != nil {
				apiError(w, 400, e)
				return
			}
			if e := c.Validate(); e != nil {
				apiError(w, 400, e)
				return
			}
			timeout := 75 * time.Second
			if op == "detect" {
				timeout = 5 * time.Minute
			}
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			if op == "detect" {
				found, e := s.Manager.Detect(ctx, c)
				if e != nil {
					apiError(w, 422, e)
					return
				}
				jsonReply(w, 200, found)
				return
			}
			if e := s.Manager.Apply(ctx, c); e != nil {
				apiError(w, 422, e)
				return
			}
			jsonReply(w, 200, map[string]bool{"ok": true})
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if dsmProxy {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			ip := net.ParseIP(host)
			if err != nil || ip == nil || !ip.IsLoopback() || r.Header.Get("X-Forwarded-Proto") != "https" {
				apiError(w, http.StatusForbidden, fmt.Errorf("open this application through DSM HTTPS"))
				return
			}
			if r.URL.Path == "/api/login" || r.URL.Path == "/api/logout" {
				http.NotFound(w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				name, err := s.dsmAuth.authenticate(r)
				if err != nil {
					code := http.StatusUnauthorized
					if errors.Is(err, errDSMAdmin) {
						code = http.StatusForbidden
					}
					apiError(w, code, err)
					return
				}
				r = r.WithContext(context.WithValue(r.Context(), dsmUserKey{}, name))
			}
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		// All browser mutations require a same-origin custom header. No CORS is granted.
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("X-UPS-Request") != "1" {
				apiError(w, 403, fmt.Errorf("missing request header"))
				return
			}
			origin := r.Header.Get("Origin")
			scheme := "http"
			if s.secure || dsmProxy {
				scheme = "https"
			}
			if origin != "" && origin != scheme+"://"+r.Host {
				apiError(w, 403, fmt.Errorf("cross-origin request"))
				return
			}
		}
		if !dsmProxy && strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/login" {
			cookie, e := r.Cookie("ups_session")
			allowed := e == nil && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(s.session)) == 1
			if !allowed && !s.validToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) {
				apiError(w, 401, fmt.Errorf("authentication required"))
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func Run(ctx context.Context, root, data, listen string, plain bool, proxyListen string) error {
	logs := &boundedBuffer{limit: 256 * 1024}
	m, err := NewManager(root, data, io.MultiWriter(&rotatingLog{path: filepath.Join(data, "nut.log")}, logs))
	if err != nil {
		return err
	}
	s, err := NewServer(m, !plain, logs)
	if err != nil {
		return err
	}
	if plain {
		h, _, e := net.SplitHostPort(listen)
		if e != nil || net.ParseIP(h) == nil || !net.ParseIP(h).IsLoopback() {
			return fmt.Errorf("plain HTTP is allowed on loopback only")
		}
	}
	server := &http.Server{Addr: listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 6 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	if !plain {
		if err = ensureCertificate(data); err != nil {
			listener.Close()
			return err
		}
	}
	var proxyServer *http.Server
	var proxyListener net.Listener
	if proxyListen != "" {
		host, _, e := net.SplitHostPort(proxyListen)
		ip := net.ParseIP(host)
		if e != nil || ip == nil || !ip.IsLoopback() {
			listener.Close()
			return fmt.Errorf("DSM proxy listener must use a loopback IP")
		}
		proxyListener, err = net.Listen("tcp", proxyListen)
		if err != nil {
			listener.Close()
			return fmt.Errorf("DSM proxy listener: %w", err)
		}
		proxyServer = &http.Server{Handler: s.DSMHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 6 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Start asynchronously so login and diagnostics stay available during startup.
	go func() { m.Start(runCtx); m.Watch(runCtx) }()
	done := make(chan error, 2)
	if proxyServer != nil {
		go func() { done <- proxyServer.Serve(proxyListener) }()
	}
	go func() {
		if plain {
			done <- server.Serve(listener)
		} else {
			done <- server.ServeTLS(listener, filepath.Join(data, "server.crt"), filepath.Join(data, "server.key"))
		}
	}()
	select {
	case <-ctx.Done():
	case err = <-done:
	}
	cancel()
	shutdown, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = server.Shutdown(shutdown)
	if proxyServer != nil {
		_ = proxyServer.Shutdown(shutdown)
	}
	m.Stop()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
