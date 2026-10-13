package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jusso-dev/nighthawk/internal/cluster"
	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/ipc"
	"github.com/jusso-dev/nighthawk/internal/mcpproxy"
	"github.com/jusso-dev/nighthawk/internal/policy"
	"github.com/jusso-dev/nighthawk/internal/setup"
	"github.com/jusso-dev/nighthawk/internal/store"
	"github.com/jusso-dev/nighthawk/internal/tui"
	"github.com/jusso-dev/nighthawk/internal/web"
)

func cmdMCPProxy(args []string, errOut io.Writer) int {
	fs := flag.NewFlagSet("mcp-proxy", flag.ContinueOnError)
	fs.SetOutput(errOut)
	server := fs.String("server", "", "mcp server name")
	upstream := fs.String("upstream", "", "streamable HTTP upstream")
	port := fs.Int("port", 0, "local HTTP listen port")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *server == "" {
		return usage(errOut, "usage: nighthawk mcp-proxy --server NAME -- <cmd> [args] | --upstream URL")
	}
	state := config.StateDir()
	decider := func(ctx context.Context, ev event.Event) (policy.Decision, error) {
		if ev.Source == "" {
			ev.Source = event.SourceMCPProxy
		}
		if ev.Agent == "" {
			ev.Agent = "mcp"
		}
		ev.Attrs = copyAttrs(ev.Attrs)
		if ev.Attrs["mcp_server"] == "" {
			ev.Attrs["mcp_server"] = *server
		}
		return decideEvent(ctx, state, ev), nil
	}
	logf := func(l mcpproxy.LogLine) {
		fmt.Fprintf(errOut, "mcp method=%s server=%s tool=%s args=%s err=%s bytes=%d\n", l.Method, l.Server, l.Tool, l.Args, l.Err, l.ResultSize)
	}
	pins := loadPins(*server)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if *upstream != "" {
		ln, err := net.Listen("tcp", configAddr(*port))
		if err != nil {
			fmt.Fprintln(errOut, "nighthawk: listen")
			return 1
		}
		fmt.Fprintf(errOut, "nighthawk: mcp http %s\n", ln.Addr())
		if err := mcpproxy.ServePinned(ctx, ln, *upstream, *server, decider, logf, pins); err != nil && ctx.Err() == nil {
			fmt.Fprintln(errOut, "nighthawk: mcp-proxy")
			return 1
		}
		return 0
	}
	argv := fs.Args()
	if len(argv) == 0 {
		return usage(errOut, "usage: nighthawk mcp-proxy --server NAME -- <cmd> [args]")
	}
	if err := mcpproxy.RunStdioPinned(ctx, *server, argv, decider, logf, pins); err != nil && ctx.Err() == nil {
		fmt.Fprintln(errOut, "nighthawk: mcp-proxy")
		return 1
	}
	return 0
}

func loadPins(server string) *mcpproxy.PinState {
	st := &mcpproxy.PinState{
		Pins:       map[string]string{},
		Unattended: config.ReadMode(config.StateDir()) == policy.Unattended,
	}
	ctx, cancel := callCtx()
	defer cancel()
	resp, err := daemonCall(ctx, config.StateDir(), ipc.Request{Op: "pins.list"})
	if err == nil && resp.OK {
		var rows []store.Pin
		_ = json.Unmarshal(resp.Data, &rows)
		for _, p := range rows {
			if p.Server == server && p.Hash != "" {
				st.Pins[p.Tool] = p.Hash
			}
		}
	}
	st.OnChange = func(fresh, changed map[string]string, tampers []mcpproxy.Tamper) {
		extra, err := json.Marshal(map[string]any{
			"server": server, "fresh": fresh, "changed": changed, "tampers": tampers,
		})
		if err != nil {
			return
		}
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = daemonCall(cctx, config.StateDir(), ipc.Request{Op: "pins.sync", Extra: extra})
	}
	return st
}

func cmdMCP(args []string, out, errOut io.Writer) int {
	if len(args) < 1 || args[0] != "pins" {
		return usage(errOut, "usage: nighthawk mcp pins [--approve server/tool]")
	}
	fs := flag.NewFlagSet("pins", flag.ContinueOnError)
	fs.SetOutput(errOut)
	approve := fs.String("approve", "", "server/tool to approve")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *approve == "" {
		return printOp(out, errOut, ipc.Request{Op: "pins.list"})
	}
	server, tool, ok := strings.Cut(*approve, "/")
	if !ok || server == "" || tool == "" {
		return usage(errOut, "usage: nighthawk mcp pins --approve server/tool")
	}
	extra, _ := json.Marshal(map[string]string{"server": server, "tool": tool})
	ctx, cancel := callCtx()
	defer cancel()
	resp, err := daemonCall(ctx, config.StateDir(), ipc.Request{Op: "pins.approve", Extra: extra, Who: "cli"})
	if callFailed(errOut, resp, err) {
		return 1
	}
	fmt.Fprintln(out, "approved")
	return 0
}

func cmdTUI(errOut io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := tui.Run(ctx, config.SocketPath(config.StateDir())); err != nil && ctx.Err() == nil {
		fmt.Fprintln(errOut, "nighthawk: tui")
		return 1
	}
	return 0
}

func cmdWeb(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(errOut)
	addr := fs.String("addr", "", "listen address")
	know := fs.Bool("i-know", false, "allow a non-loopback address")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadCfg()
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: config")
		return 1
	}
	if *addr == "" {
		*addr = cfg.Web.Addr
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		fmt.Fprintln(errOut, "nighthawk: token")
		return 1
	}
	token := hex.EncodeToString(buf[:])
	fmt.Fprintf(out, "http://%s/?token=%s\n", *addr, token)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := web.Serve(ctx, *addr, config.SocketPath(config.StateDir()), token, *know); err != nil && ctx.Err() == nil {
		fmt.Fprintln(errOut, "nighthawk: web")
		return 1
	}
	return 0
}

func cmdSetup(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dry := fs.Bool("dry-run", false, "print a diff and do not write")
	uninstall := fs.Bool("uninstall", false, "remove nighthawk hooks")
	claude := fs.String("claude", "", "Claude settings.json path")
	codex := fs.String("codex", "", "Codex config.toml path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: home")
		return 1
	}
	if *claude == "" && *codex == "" {
		*claude = filepath.Join(home, ".claude", "settings.json")
		*codex = filepath.Join(home, ".codex", "config.toml")
	}
	bin, err := os.Executable()
	if err != nil || bin == "" {
		fmt.Fprintln(errOut, "nighthawk: executable")
		return 1
	}
	if *claude != "" {
		var diff string
		var serr error
		if *uninstall {
			diff, serr = setup.ClaudeUninstall(*claude, *dry)
		} else {
			diff, serr = setup.ClaudeInstall(*claude, bin, *dry)
		}
		if serr != nil {
			fmt.Fprintln(errOut, "nighthawk: claude setup")
			return 1
		}
		_, _ = io.WriteString(out, diff)
	}
	if *codex != "" {
		var diff string
		var serr error
		if *uninstall {
			diff, serr = setup.CodexUninstall(*codex, *dry)
		} else {
			diff, serr = setup.CodexInstall(*codex, bin, *dry)
		}
		if serr != nil {
			fmt.Fprintln(errOut, "nighthawk: codex setup")
			return 1
		}
		_, _ = io.WriteString(out, diff)
	}
	return 0
}

func cmdStatusCluster(out, errOut io.Writer, hosts string) int {
	var list []string
	for _, h := range strings.Split(hosts, ",") {
		if h = strings.TrimSpace(h); h != "" {
			list = append(list, h)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	local, _ := daemonCall(ctx, config.StateDir(), ipc.Request{Op: "status"})
	remote := cluster.QueryWith(ctx, list, cluster.SSH)
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"local": local, "hosts": remote}); err != nil {
		fmt.Fprintln(errOut, "nighthawk: status")
		return 1
	}
	return 0
}

func copyAttrs(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func configAddr(port int) string {
	if port == 0 {
		return "127.0.0.1:0"
	}
	return "127.0.0.1:" + strconv.Itoa(port)
}

func notifyDesktop(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		script := "display notification " + strconv.Quote(body) + " with title " + strconv.Quote(title)
		_ = exec.Command("osascript", "-e", script).Run()
	case "linux":
		_ = exec.Command("notify-send", title, body).Run()
	}
}
