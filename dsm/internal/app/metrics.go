package app

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	c := s.Manager.Config()
	if !c.Enabled {
		fmt.Fprintln(w, "ups_up 0")
		return
	}
	vars, err := readUPS(r.Context(), c)
	if err != nil {
		fmt.Fprintln(w, "ups_up 0")
		return
	}
	fmt.Fprintln(w, "ups_up 1")
	for _, k := range []string{"input.voltage", "output.voltage", "ups.load", "battery.charge", "battery.voltage", "battery.runtime", "ups.temperature", "input.frequency"} {
		if n, e := strconv.ParseFloat(vars[k], 64); e == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
			name := "ups_" + strings.ReplaceAll(strings.TrimPrefix(k, "ups."), ".", "_")
			fmt.Fprintf(w, "%s{ups=\"ups\"} %g\n", name, n)
		}
	}
	flags := strings.Fields(vars["ups.status"])
	for _, f := range []string{"OL", "OB", "LB", "RB"} {
		n := 0
		if contains(flags, f) {
			n = 1
		}
		fmt.Fprintf(w, "ups_status{ups=\"ups\",flag=%q} %d\n", f, n)
	}
}
