package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// CLI calls Herdr with a fixed argv. It never invokes a shell.
type CLI struct {
	Bin     string
	Machine string
}

func (c CLI) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	if b := os.Getenv("HERDR_BIN_PATH"); b != "" {
		return b
	}
	return "herdr"
}

func (c CLI) argv(args ...string) []string {
	out := make([]string, 0, len(args)+4)
	if c.Machine != "" {
		out = append(out, "--machine", c.Machine)
	}
	out = append(out, "--json")
	out = append(out, args...)
	return out
}

func (c CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.bin(), c.argv(args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("herdr %s: %w: %s", args[0], err, stderr.Bytes())
	}
	return stdout.Bytes(), nil
}

// ListAgents runs `agent list`.
func (c CLI) ListAgents(ctx context.Context) ([]AgentInfo, error) {
	b, err := c.run(ctx, "agent", "list")
	if err != nil {
		return nil, err
	}
	var out []AgentInfo
	if err := json.Unmarshal(bytes.TrimSpace(b), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadPane runs `pane read`.
func (c CLI) ReadPane(ctx context.Context, id string, lines int) (string, error) {
	b, err := c.run(ctx, "pane", "read", id, "--lines", fmt.Sprint(lines))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ProcessInfo runs `pane process-info`.
func (c CLI) ProcessInfo(ctx context.Context, id string) (ProcessInfo, error) {
	b, err := c.run(ctx, "pane", "process-info", id)
	if err != nil {
		return ProcessInfo{}, err
	}
	var info ProcessInfo
	if err := json.Unmarshal(bytes.TrimSpace(b), &info); err != nil {
		return ProcessInfo{}, err
	}
	return info, nil
}

// SendKeys runs `pane send-keys`.
func (c CLI) SendKeys(ctx context.Context, id, keys string) error {
	_, err := c.run(ctx, "pane", "send-keys", id, "--keys", keys)
	return err
}

// ClosePane runs `pane close`.
func (c CLI) ClosePane(ctx context.Context, id string) error {
	_, err := c.run(ctx, "pane", "close", id)
	return err
}

// Notify runs `notification`.
func (c CLI) Notify(ctx context.Context, title, body string) error {
	_, err := c.run(ctx, "notification", title, body)
	return err
}
