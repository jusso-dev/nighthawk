// Package transcript parses agent transcript JSONL for token usage.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"time"
)

// Usage is token use read from one transcript line.
type Usage struct {
	Model      string
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	// Key dedupes repeated lines for the same API response.
	Key string
	// TS is the line timestamp, zero when the line has none.
	TS time.Time
}

// ParseClaude extracts token usage from a Claude transcript JSONL line.
// Claude Code writes one line per content block, each carrying the same
// usage, so the key is the API message id rather than the line uuid.
func ParseClaude(line []byte) (Usage, bool) {
	var raw struct {
		UUID      string `json:"uuid"`
		Timestamp string `json:"timestamp"`
		Message   struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage struct {
				InputTokens              int64 `json:"input_tokens"`
				OutputTokens             int64 `json:"output_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return Usage{}, false
	}
	u := raw.Message.Usage
	if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadInputTokens == 0 && u.CacheCreationInputTokens == 0 {
		return Usage{}, false
	}
	key := raw.Message.ID
	if key == "" {
		key = raw.UUID
	}
	return Usage{
		Model: raw.Message.Model, Input: u.InputTokens, Output: u.OutputTokens,
		CacheRead: u.CacheReadInputTokens, CacheWrite: u.CacheCreationInputTokens,
		Key: key, TS: parseTS(raw.Timestamp),
	}, true
}

// codexTokens is a Codex token_count usage block. input_tokens includes
// cached_input_tokens, and output_tokens includes reasoning tokens.
type codexTokens struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
}

// ParseCodex extracts token usage from a Codex rollout token_count line.
// Codex does not put the model there: callers fill it from CodexModel.
// Key is the running session total, so a repeated token_count dedupes.
func ParseCodex(line []byte) (Usage, bool) {
	var raw struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Payload   struct {
			Type string `json:"type"`
			Info *struct {
				Model           string      `json:"model"`
				TotalTokenUsage codexTokens `json:"total_token_usage"`
				LastTokenUsage  codexTokens `json:"last_token_usage"`
			} `json:"info"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return Usage{}, false
	}
	if raw.Type != "event_msg" || raw.Payload.Type != "token_count" || raw.Payload.Info == nil {
		return Usage{}, false
	}
	u := raw.Payload.Info.LastTokenUsage
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return Usage{}, false
	}
	in := u.InputTokens - u.CachedInputTokens
	if in < 0 {
		in = 0
	}
	var key string
	if tot := raw.Payload.Info.TotalTokenUsage.TotalTokens; tot > 0 {
		key = "total:" + strconv.FormatInt(tot, 10)
	}
	return Usage{
		Model: raw.Payload.Info.Model, Input: in, Output: u.OutputTokens,
		CacheRead: u.CachedInputTokens, CacheWrite: u.CacheWriteInputTokens,
		Key: key, TS: parseTS(raw.Timestamp),
	}, true
}

// CodexModel returns the model named by a Codex turn_context or session_meta line.
func CodexModel(line []byte) (string, bool) {
	var raw struct {
		Type    string `json:"type"`
		Payload struct {
			Model string `json:"model"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return "", false
	}
	if (raw.Type != "turn_context" && raw.Type != "session_meta") || raw.Payload.Model == "" {
		return "", false
	}
	return raw.Payload.Model, true
}

func parseTS(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// ReadNew reads complete lines from path starting at off.
// A trailing partial line without a newline is not consumed.
func ReadNew(path string, off int64) (lines [][]byte, next int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, off, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, off, err
	}
	r := bufio.NewReader(f)
	pos := off
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if err == nil || (err == io.EOF && bytes.HasSuffix(line, []byte{'\n'})) {
				// complete line including newline
				trimmed := bytes.TrimRight(line, "\r\n")
				cp := make([]byte, len(trimmed))
				copy(cp, trimmed)
				lines = append(lines, cp)
				pos += int64(len(line))
			} else if err == io.EOF {
				// partial line: do not consume
				break
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return lines, pos, err
		}
	}
	return lines, pos, nil
}
