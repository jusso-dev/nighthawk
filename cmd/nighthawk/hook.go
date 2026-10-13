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
	"github.com/jusso-dev/nighthawk/internal/redact"
	"github.com/jusso-dev/nighthawk/internal/setup"
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
	state := config.StateDir()
	if cfg, err := loadCfg(); err == nil {
		_ = redact.SetExtra(cfg.Redact.Extra) // Load already compiled these.
	}
	// Claude Code treats exit 1 as non-blocking, so input we cannot check
	// must still deny when nobody is watching.
	reject := func(msg string) int {
		fmt.Fprintln(errOut, "nighthawk: "+msg)
		if config.ReadMode(state) == policy.Unattended {
			_, _ = out.Write(claude.DenyStdout("nh.failclosed"))
			return 0
		}
		return 1
	}
	// Codex runs notify programs with the JSON payload as the last argument.
	var body []byte
	var err error
	if len(args) > 1 {
		body = []byte(args[len(args)-1])
	} else if body, err = io.ReadAll(io.LimitReader(in, 1<<20+1)); err != nil {
		return reject("read hook input")
	}
	if len(body) > 1<<20 {
		return reject("hook input too large")
	}
	pane := os.Getenv("HERDR_PANE_ID")
	task := cost.TaskID(args[0], os.Getenv("NIGHTHAWK_TASK_ID"))
	evs, err := ad.ToEvents(body, pane, task)
	if err != nil {
		return reject("hook json")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var deny bool
	var rule string
	var warned bool
	for _, ev := range evs {
		if ev.Pre {
			dec, approval := decideWithApproval(ctx, state, ev)
			if dec.RuleID == "nh.failopen" && !warned {
				fmt.Fprintln(errOut, "nighthawk: daemon unreachable")
				warned = true
			}
			if dec.Action == policy.AskHuman && approval != "" {
				fmt.Fprintln(errOut, "nighthawk: waiting for approval "+approval)
				if waitApproved(state, approval, approvalWait()) {
					continue
				}
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

// approvalWait is how long a hook holds a tool call for a human. It stays
// under the PreToolUse timeout setup installs so the agent never kills the
// hook first. NIGHTHAWK_APPROVAL_WAIT may only shorten it.
func approvalWait() time.Duration {
	limit := time.Duration(setup.ClaudePreToolTimeout)*time.Second - 10*time.Second
	if d, err := time.ParseDuration(os.Getenv("NIGHTHAWK_APPROVAL_WAIT")); err == nil && d > 0 && d < limit {
		return d
	}
	return limit
}
