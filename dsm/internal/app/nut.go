package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Read NUT directly: the web service needs neither an external upsc process nor TLS libraries.
func readUPS(ctx context.Context, c Config) (map[string]string, error) {
	host := c.Listen
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(c.Port)))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = fmt.Fprint(conn, "LIST VAR ups\n"); err != nil {
		return nil, err
	}
	s := bufio.NewScanner(conn)
	s.Buffer(make([]byte, 4096), 65536)
	vars := map[string]string{}
	begun := false
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "ERR ") {
			return nil, errors.New(line)
		}
		if line == "BEGIN LIST VAR ups" {
			begun = true
			continue
		}
		if line == "END LIST VAR ups" && begun {
			if vars["ups.status"] == "" {
				return nil, errors.New("missing UPS status")
			}
			if contains(strings.Fields(vars["ups.status"]), "WAIT") {
				return nil, errors.New("UPS is initializing")
			}
			return vars, nil
		}
		if strings.HasPrefix(line, "VAR ups ") && begun {
			p := strings.SplitN(strings.TrimPrefix(line, "VAR ups "), " ", 2)
			if len(p) == 2 {
				v, e := strconv.Unquote(p[1])
				if e != nil {
					return nil, errors.New("invalid NUT response")
				}
				vars[p[0]] = v
			}
		}
		if len(vars) > 1024 {
			return nil, errors.New("too many NUT variables")
		}
	}
	if s.Err() != nil {
		return nil, s.Err()
	}
	return nil, errors.New("incomplete NUT response")
}
