// Command nighthawk is the Nighthawk daemon, CLI and hook binary.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/jusso-dev/nighthawk/internal/version"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: nighthawk <version|daemon|hook|mcp-proxy|egress-proxy|tui|audit>")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(out, version.Version)
		return 0
	default:
		fmt.Fprintf(errOut, "nighthawk: %q is not implemented yet\n", args[0])
		return 2
	}
}
