package app

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func localLANIPv4s() []net.IP {
	interfaces, _ := net.Interfaces()
	var ips []net.IP
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || strings.HasPrefix(iface.Name, "docker") || strings.HasPrefix(iface.Name, "br-") || strings.HasPrefix(iface.Name, "veth") {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, addr := range addresses {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.To4() != nil && ip.IsPrivate() {
				ips = append(ips, ip.To4())
			}
		}
	}
	return ips
}

// Never choose a client-supplied address or all interfaces. Prefer the actual
// DSM nginx server address, but only when it belongs to an active local NIC.
func chooseLANIPv4(preferred string, addresses []net.IP) string {
	wanted := net.ParseIP(preferred)
	unique := map[string]bool{}
	for _, ip := range addresses {
		if ip.To4() == nil || !ip.IsPrivate() || ip.IsLoopback() {
			continue
		}
		if wanted != nil && ip.Equal(wanted) {
			return ip.String()
		}
		unique[ip.String()] = true
	}
	if len(unique) == 1 {
		for ip := range unique {
			return ip
		}
	}
	return ""
}

func (s *Server) settings(r *http.Request, dsmProxy bool) map[string]any {
	preferred := ""
	if dsmProxy {
		preferred = r.Header.Get("X-UPS-Server-Addr")
	} else if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		preferred, _, _ = net.SplitHostPort(addr.String())
	}
	suggested := chooseLANIPv4(preferred, s.localIPs())
	_, err := os.Stat(filepath.Join(s.Manager.data, "settings.json"))
	saved := !os.IsNotExist(err)
	c := s.Manager.Config()
	autodetected := !saved && suggested != ""
	if autodetected {
		c.Listen = suggested
	}
	return map[string]any{"config": c, "drivers": drivers, "subdrivers": subdrivers, "suggested_listen": suggested, "listen_autodetected": autodetected, "settings_saved": saved}
}
