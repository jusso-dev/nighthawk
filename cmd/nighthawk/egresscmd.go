package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/egress"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

// standaloneEgressPort differs from the daemon's default egress port so a
// standalone proxy does not collide with a running daemon.
const standaloneEgressPort = 8766

// cmdEgress runs a proxy outside the daemon. It has no pane tokens, so it
// accepts requests without one and never treats the proxy user as a pane id.
func cmdEgress(args []string, errOut io.Writer) int {
	fs := flag.NewFlagSet("egress-proxy", flag.ContinueOnError)
	fs.SetOutput(errOut)
	port := fs.Int("port", standaloneEgressPort, "listen port")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadCfg()
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: config")
		return 1
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintf(errOut, "nighthawk: listen: %v\n", err)
		return 1
	}
	fmt.Fprintf(errOut, "nighthawk: egress proxy on %s\n", ln.Addr())
	state := config.StateDir()
	var warn sync.Once
	var logMu sync.Mutex
	p := &egress.Proxy{
		Allow: egress.Allow{Hosts: cfg.Egress.Allowlist, AllowPrivate: cfg.Egress.AllowPrivate},
		Decide: func(ctx context.Context, pane, host string, port int) (policy.Decision, error) {
			ev := event.Event{
				Kind: event.KindNetwork, Source: event.SourceEgress, PaneID: pane, Pre: true,
				Attrs: map[string]string{"host": host, "port": strconv.Itoa(port)},
			}
			d := decideEvent(ctx, state, ev)
			if d.RuleID == "nh.failopen" {
				warn.Do(func() { fmt.Fprintln(errOut, "nighthawk: daemon unreachable") })
			}
			return d, nil
		},
		OnEvent: func(ev event.Event, d policy.Decision) {
			logMu.Lock()
			defer logMu.Unlock()
			fmt.Fprintf(errOut, "nighthawk: egress %s %s:%s in=%s out=%s ms=%s rule=%s\n",
				d.Action, ev.Attrs["host"], ev.Attrs["port"], ev.Attrs["bytes_in"],
				ev.Attrs["bytes_out"], ev.Attrs["duration_ms"], d.RuleID)
		},
	}
	if err := p.Serve(context.Background(), ln); err != nil {
		fmt.Fprintln(errOut, "nighthawk: egress")
		return 1
	}
	return 0
}
