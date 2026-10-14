package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/enforce"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/ipc"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

func stateDir(fs *flag.FlagSet) *string {
	return fs.String("state-dir", "", "state directory")
}

func resolveState(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return config.StateDir()
}

func loadCfg() (config.Config, error) {
	return config.Load(config.ConfigPath())
}

func daemonCall(ctx context.Context, state string, req ipc.Request) (ipc.Response, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return ipc.Call(cctx, config.SocketPath(state), req)
}

// callFailed reports a failed daemon call. It keeps "unreachable" for
// transport errors and shows the daemon's own error otherwise.
func callFailed(errOut io.Writer, resp ipc.Response, err error) bool {
	switch {
	case err != nil:
		fmt.Fprintln(errOut, "nighthawk: daemon unreachable")
	case !resp.OK:
		msg := resp.Error
		if msg == "" {
			msg = "request failed"
		}
		fmt.Fprintln(errOut, "nighthawk: "+msg)
	default:
		return false
	}
	return true
}

func decideEvent(ctx context.Context, state string, ev event.Event) policy.Decision {
	dec, _ := decideWithApproval(ctx, state, ev)
	return dec
}

// decideWithApproval also returns the approval id the daemon opened for an
// ask_human decision, or "" when there is none.
func decideWithApproval(ctx context.Context, state string, ev event.Event) (policy.Decision, string) {
	resp, err := daemonCall(ctx, state, ipc.Request{Op: "decide", Event: &ev})
	if err != nil || !resp.OK || resp.Decision == nil {
		return enforce.Fallback(config.ReadMode(state), err), ""
	}
	var data struct {
		ApprovalID string `json:"approval_id"`
	}
	if len(resp.Data) > 0 {
		_ = json.Unmarshal(resp.Data, &data)
	}
	return *resp.Decision, data.ApprovalID
}

// waitApproved long-polls the daemon until a human decides approval id or
// wait ends. It reports true only for an approval. A wait that runs out
// marks the approval expired so a late answer cannot apply.
func waitApproved(state, id string, wait time.Duration) bool {
	sock := config.SocketPath(state)
	ctx, cancel := context.WithTimeout(context.Background(), wait+2*time.Second)
	defer cancel()
	resp, err := ipc.Call(ctx, sock, ipc.Request{Op: "approval.wait", ID: id, Wait: wait})
	var a struct {
		Status string `json:"Status"`
	}
	if err == nil && resp.OK && json.Unmarshal(resp.Data, &a) == nil && a.Status != "pending" {
		return a.Status == "approved"
	}
	ectx, ecancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer ecancel()
	_, _ = ipc.Call(ectx, sock, ipc.Request{Op: "approval.expire", ID: id})
	return false
}

func printJSON(out io.Writer, v any) int {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return 1
	}
	return 0
}

func usage(errOut io.Writer, text string) int {
	fmt.Fprintln(errOut, text)
	return 2
}

func getenv(k string) string { return os.Getenv(k) }
