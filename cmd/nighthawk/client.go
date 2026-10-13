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

func decideEvent(ctx context.Context, state string, ev event.Event) policy.Decision {
	resp, err := daemonCall(ctx, state, ipc.Request{Op: "decide", Event: &ev})
	if err != nil || !resp.OK || resp.Decision == nil {
		return enforce.Fallback(config.ReadMode(state), err)
	}
	return *resp.Decision
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
