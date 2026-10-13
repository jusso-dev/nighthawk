package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jusso-dev/nighthawk/internal/capture"
	"github.com/jusso-dev/nighthawk/internal/capture/claude"
	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/cost"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

func cmdHook(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) < 1 {
		return usage(errOut, "usage: nighthawk hook <claude|codex|grok>")
	}
	ad, ok := capture.ByName(args[0])
	if !ok {
		fmt.Fprintf(errOut, "nighthawk: unknown agent %q\n", args[0])
		return 2
	}
	body, err := io.ReadAll(io.LimitReader(in, 1<<20+1))
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: read hook input")
		return 1
	}
	if len(body) > 1<<20 {
		fmt.Fprintln(errOut, "nighthawk: hook input too large")
		return 1
	}
	pane := os.Getenv("HERDR_PANE_ID")
	task := cost.TaskID(args[0], os.Getenv("NIGHTHAWK_TASK_ID"))
	evs, err := ad.ToEvents(body, pane, task)
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: hook json")
		return 1
	}
	state := config.StateDir()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var deny bool
	var rule string
	var warned bool
	for _, ev := range evs {
		if ev.Pre {
			dec := decideEvent(ctx, state, ev)
			if dec.RuleID == "nh.failopen" && !warned {
				fmt.Fprintln(errOut, "nighthawk: daemon unreachable")
				warned = true
			}
			if dec.Action != policy.Allow {
				deny = true
				rule = dec.RuleID
			}
			continue
		}
		_, _ = daemonCall(ctx, state, ipcRequestSubmit(ev))
	}
	if deny {
		_, _ = out.Write(claude.DenyStdout(rule))
	}
	return 0
}
