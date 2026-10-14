// Package cluster queries spend across remote nighthawk hosts.
package cluster

import (
	"context"
	"encoding/json"
	"os/exec"
)

// Result is one host's spend query outcome.
type Result struct {
	Host   string          `json:"host"`
	Status string          `json:"status"`
	Spend  json.RawMessage `json:"spend"`
}

// QueryWith runs fn for each host. On error Status is "unknown" and Spend is nil.
func QueryWith(ctx context.Context, hosts []string, run func(ctx context.Context, host string) ([]byte, error)) []Result {
	out := make([]Result, 0, len(hosts))
	for _, h := range hosts {
		r := Result{Host: h}
		b, err := run(ctx, h)
		if err != nil {
			r.Status = "unknown"
			r.Spend = nil
		} else {
			r.Status = "ok"
			r.Spend = json.RawMessage(b)
		}
		out = append(out, r)
	}
	return out
}

// SSHArgs returns the exact argv used for a remote spend query.
func SSHArgs(host string) []string {
	return []string{"ssh", "-o", "BatchMode=yes", host, "nighthawk", "spend", "--by", "task", "--json"}
}

// SSH runs SSHArgs via exec.CommandContext.
func SSH(ctx context.Context, host string) ([]byte, error) {
	args := SSHArgs(host)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	return cmd.Output()
}
