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
// Herdr 0.9.3 API commands print one JSON response; there is no --json flag.
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
	out := make([]string, 0, len(args)+2)
	if c.Machine != "" {
		out = append(out, "--machine", c.Machine)
	}
	out = append(out, args...)
	return out
}

func (c CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.bin(), c.argv(args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if apiErr := decodeResult(stdout.Bytes(), nil); apiErr != nil {
			return nil, fmt.Errorf("herdr %s: %w", args[0], apiErr)
		}
		return nil, fmt.Errorf("herdr %s: %w: %s", args[0], err, bytes.TrimSpace(stderr.Bytes()))
	}
	return stdout.Bytes(), nil
}

// decodeResult unwraps {"id":...,"result":...} into v, or returns the error body.
func decodeResult(b []byte, v any) error {
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *ErrorBody      `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(b), &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	if v == nil {
		return nil
	}
	if len(resp.Result) == 0 {
		return fmt.Errorf("herdr: response has no result")
	}
	return json.Unmarshal(resp.Result, v)
}

// ListAgents runs `agent list`.
func (c CLI) ListAgents(ctx context.Context) ([]AgentInfo, error) {
	b, err := c.run(ctx, "agent", "list")
	if err != nil {
		return nil, err
	}
	var res struct {
		Agents []AgentInfo `json:"agents"`
	}
	if err := decodeResult(b, &res); err != nil {
		return nil, err
	}
	return res.Agents, nil
}

// ReadPane runs `pane read`.
func (c CLI) ReadPane(ctx context.Context, id string, lines int) (string, error) {
	b, err := c.run(ctx, "pane", "read", id, "--lines", fmt.Sprint(lines))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ProcessInfo runs `pane process-info --pane`.
func (c CLI) ProcessInfo(ctx context.Context, id string) (ProcessInfo, error) {
	b, err := c.run(ctx, "pane", "process-info", "--pane", id)
	if err != nil {
		return ProcessInfo{}, err
	}
	var res struct {
		ProcessInfo ProcessInfo `json:"process_info"`
	}
	if err := decodeResult(b, &res); err != nil {
		return ProcessInfo{}, err
	}
	return res.ProcessInfo, nil
}

// SendKeys runs `pane send-keys`. keys is one logical key such as esc or ctrl+c.
func (c CLI) SendKeys(ctx context.Context, id, keys string) error {
	_, err := c.run(ctx, "pane", "send-keys", id, keys)
	return err
}

// ClosePane runs `pane close`.
func (c CLI) ClosePane(ctx context.Context, id string) error {
	_, err := c.run(ctx, "pane", "close", id)
	return err
}

// Notify runs `notification show`.
func (c CLI) Notify(ctx context.Context, title, body string) error {
	_, err := c.run(ctx, "notification", "show", title, "--body", body)
	return err
}
