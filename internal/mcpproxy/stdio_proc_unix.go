//go:build unix

package mcpproxy

import (
	"os/exec"
	"syscall"
)

func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killChildGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
