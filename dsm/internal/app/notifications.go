package app

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// DSM's documented desktop-notification interface. This stays under the package
// account; no sudo, system UPS config edits or implicit email subscriptions.
func notifyDSM(ctx context.Context, event string) error {
	if event != "lost" && event != "restored" && event != "test" {
		return fmt.Errorf("unknown notification event")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/syno/bin/synodsmnotify", "-c", "org.usbupsserver.App", "@administrators", "usb-ups-server:notifications:title", "usb-ups-server:notifications:"+event)
	output := &boundedBuffer{limit: 4096}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("DSM desktop notification: %w: %s", err, output.String())
	}
	return nil
}
