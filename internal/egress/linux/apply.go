package linux

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Apply loads the table with nft. It refuses to run unless the process is root on Linux.
func Apply(ctx context.Context, cgroup string) error {
	if runtime.GOOS != "linux" {
		return errors.New("nft apply requires linux")
	}
	if os.Geteuid() != 0 {
		return errors.New("nft apply requires root")
	}
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(Table(cgroup))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("nft: " + strings.TrimSpace(string(out)))
	}
	return nil
}

// Disable deletes table inet nighthawk.
func Disable(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return errors.New("nft apply requires linux")
	}
	if os.Geteuid() != 0 {
		return errors.New("nft apply requires root")
	}
	cmd := exec.CommandContext(ctx, "nft", "delete", "table", "inet", "nighthawk")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("nft: " + strings.TrimSpace(string(out)))
	}
	return nil
}
