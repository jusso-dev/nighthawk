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

func cmdEgress(args []string, errOut io.Writer) int {
	fs := flag.NewFlagSet("egress-proxy", flag.ContinueOnError)
	fs.SetOutput(errOut)
	port := fs.Int("port", 0, "listen port")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadCfg()
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: config")
		return 1
	}
	if *port == 0 {
		*port = cfg.Egress.Port
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: listen")
		return 1
	}
	state := config.StateDir()
	var warn sync.Once
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
	}
	if err := p.Serve(context.Background(), ln); err != nil {
		fmt.Fprintln(errOut, "nighthawk: egress")
		return 1
	}
	return 0
}
