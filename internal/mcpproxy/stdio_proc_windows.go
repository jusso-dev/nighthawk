//go:build windows

package mcpproxy

import "os/exec"

func configureChild(cmd *exec.Cmd) {}

func killChildGroup(pid int) {}
