package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"usb-ups-server/dsm/internal/app"
)

func main() {
	exe, _ := os.Executable()
	root := flag.String("root", filepath.Dir(filepath.Dir(exe)), "package target directory")
	data := flag.String("data", "/var/packages/usb-ups-server/var", "persistent private directory")
	listen := flag.String("listen", ":8787", "HTTPS address")
	plain := flag.Bool("http", false, "development HTTP; loopback only")
	proxy := flag.String("dsm-proxy", "127.0.0.1:8788", "loopback HTTP listener for DSM nginx; empty disables")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if e := app.Run(ctx, *root, *data, *listen, *plain, *proxy); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
