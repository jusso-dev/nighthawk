// Package transcript parses agent transcript JSONL for token usage.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
)

// ParseClaude extracts token usage from a Claude transcript JSONL line.
func ParseClaude(line []byte) (model string, in, out, cacheRead, cacheWrite int64, messageKey string, ok bool) {
	var raw struct {
		UUID    string `json:"uuid"`
		Message struct {
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
		return "", 0, 0, 0, 0, "", false
	}
	if raw.Message.Model == "" && raw.Message.Usage.InputTokens == 0 && raw.Message.Usage.OutputTokens == 0 &&
		raw.Message.Usage.CacheReadInputTokens == 0 && raw.Message.Usage.CacheCreationInputTokens == 0 {
		return "", 0, 0, 0, 0, "", false
	}
	key := raw.UUID
	if key == "" {
		key = raw.Message.ID
	}
	return raw.Message.Model,
		raw.Message.Usage.InputTokens,
		raw.Message.Usage.OutputTokens,
		raw.Message.Usage.CacheReadInputTokens,
		raw.Message.Usage.CacheCreationInputTokens,
		key,
		true
}

// ParseCodex extracts token usage from a Codex transcript JSONL line.
func ParseCodex(line []byte) (model string, in, out, cacheRead, cacheWrite int64, messageKey string, ok bool) {
	var raw struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
			Info struct {
				Model          string `json:"model"`
				LastTokenUsage struct {
					InputTokens              int64 `json:"input_tokens"`
					OutputTokens             int64 `json:"output_tokens"`
					CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
					CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				} `json:"last_token_usage"`
			} `json:"info"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return "", 0, 0, 0, 0, "", false
	}
	if raw.Type != "event_msg" || raw.Payload.Type != "token_count" {
		return "", 0, 0, 0, 0, "", false
	}
	u := raw.Payload.Info.LastTokenUsage
	return raw.Payload.Info.Model, u.InputTokens, u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens, "", true
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
