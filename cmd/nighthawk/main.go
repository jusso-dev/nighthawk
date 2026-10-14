// Command nighthawk is the Nighthawk daemon, CLI and hook binary.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jusso-dev/nighthawk/internal/version"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: nighthawk <command>")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(out, version.Version)
		return 0
	case "daemon":
		return cmdDaemon(args[1:], out, errOut)
	case "hook":
		return cmdHook(args[1:], in, out, errOut)
	case "mode":
		return cmdMode(args[1:], out, errOut)
	case "config":
		return cmdConfig(args[1:], out, errOut)
	case "audit":
		return cmdAudit(args[1:], out, errOut)
	case "herdr-event":
		return cmdHerdrEvent(args[1:], errOut)
	case "spend":
		return cmdSpend(args[1:], out, errOut)
	case "skills":
		return cmdSkills(out, errOut)
	case "doctor":
		return cmdDoctor(out, errOut)
	case "env":
		return cmdEnv(args[1:], out, errOut)
	case "status":
		return cmdStatus(args[1:], out, errOut)
	case "approve", "deny":
		return cmdResolve(args[0], args[1:], out, errOut)
	case "policy":
		return cmdPolicy(args[1:], in, out, errOut)
	case "egress-proxy":
		return cmdEgress(args[1:], errOut)
	case "mcp-proxy":
		return cmdMCPProxy(args[1:], errOut)
	case "mcp":
		return cmdMCP(args[1:], out, errOut)
	case "tui":
		return cmdTUI(errOut)
	case "web":
		return cmdWeb(args[1:], out, errOut)
	case "setup":
		return cmdSetup(args[1:], out, errOut)
	default:
		fmt.Fprintf(errOut, "nighthawk: %q is not implemented yet\n", args[0])
		return 2
	}
}

func callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
