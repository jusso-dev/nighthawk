package mcpproxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/jusso-dev/nighthawk/internal/audit"
)

// Tool is one MCP tool descriptor used for pinning.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// Tamper is a new or changed tool relative to approved pins.
type Tamper struct {
	Server string
	Tool   string
	Old    string
	New    string
}

// HashTool returns the sha256 hex of the canonical JSON for name, description, schema.
func HashTool(t Tool) string {
	var schema any
	if len(t.Schema) > 0 {
		if err := json.Unmarshal(t.Schema, &schema); err != nil {
			schema = string(t.Schema)
		}
	}
	b, err := audit.Canonical(map[string]any{
		"name":        t.Name,
		"description": t.Description,
		"schema":      schema,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Diff compares tools against approved pins.
// pins key is tool name, value is approved hash.
// New or changed tools emit one Tamper and always set pending[name]=new hash.
// Unattended mode omits those tools from filtered. Unchanged tools stay filtered and are not pending.
func Diff(server string, tools []Tool, pins map[string]string, unattended bool) (filtered []Tool, tampers []Tamper, pending map[string]string) {
	pending = map[string]string{}
	seen := map[string]bool{}
	for _, t := range tools {
		seen[t.Name] = true
		h := HashTool(t)
		old, ok := pins[t.Name]
		if !ok {
			filtered = append(filtered, t)
			pending[t.Name] = h
			continue
		}
		if old == h {
			filtered = append(filtered, t)
			continue
		}
		tampers = append(tampers, Tamper{Server: server, Tool: t.Name, Old: old, New: h})
		pending[t.Name] = h
		if !unattended {
			filtered = append(filtered, t)
		}
	}
	for name, old := range pins {
		if !seen[name] {
			tampers = append(tampers, Tamper{Server: server, Tool: name, Old: old})
		}
	}
	return filtered, tampers, pending
}

// PinState is the proxy's view of approved tool hashes.
// OnChange reports first-seen hashes, changed hashes, and new tampers once each.
type PinState struct {
	Unattended bool
	mu         sync.Mutex
	Pins       map[string]string
	alerted    map[string]bool
	OnChange   func(fresh, changed map[string]string, tampers []Tamper)
}

type idSet struct {
	mu sync.Mutex
	m  map[string]struct{}
}

func newIDSet() *idSet { return &idSet{m: map[string]struct{}{}} }

func (s *idSet) add(id string) {
	if s == nil || id == "" || id == "null" {
		return
	}
	s.mu.Lock()
	s.m[id] = struct{}{}
	s.mu.Unlock()
}

func (s *idSet) take(id string) bool {
	if s == nil || id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[id]
	delete(s.m, id)
	return ok
}

func noteToolsList(line []byte, ids *idSet) {
	if ids == nil {
		return
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	if line[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal(line, &items) != nil {
			return
		}
		for _, item := range items {
			noteToolsList(item, ids)
		}
		return
	}
	var p struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(line, &p) != nil || p.Method != "tools/list" {
		return
	}
	ids.add(string(bytes.TrimSpace(p.ID)))
}

func rewriteToolsList(line []byte, server string, ids *idSet, pins *PinState) []byte {
	if pins == nil || ids == nil {
		return line
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return line
	}
	if line[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal(line, &items) != nil {
			return line
		}
		changed := false
		for i, item := range items {
			next := rewriteOneList(item, server, ids, pins)
			if !bytes.Equal(bytes.TrimSpace(next), bytes.TrimSpace(item)) {
				items[i] = next
				changed = true
			}
		}
		if !changed {
			return line
		}
		b, err := json.Marshal(items)
		if err != nil {
			return line
		}
		return b
	}
	return rewriteOneList(line, server, ids, pins)
}

func rewriteSSE(raw []byte, server string, ids *idSet, pins *PinState) []byte {
	lines := bytes.Split(raw, []byte("\n"))
	for i, line := range lines {
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		lines[i] = append([]byte("data: "), rewriteToolsList(payload, server, ids, pins)...)
	}
	return bytes.Join(lines, []byte("\n"))
}

func rewriteOneList(line []byte, server string, ids *idSet, pins *PinState) []byte {
	var env map[string]json.RawMessage
	if json.Unmarshal(line, &env) != nil {
		return line
	}
	id := string(bytes.TrimSpace(env["id"]))
	if !ids.take(id) || len(env["result"]) == 0 {
		return line
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(env["result"], &result) != nil || len(result["tools"]) == 0 {
		return line
	}
	var rawTools []json.RawMessage
	if json.Unmarshal(result["tools"], &rawTools) != nil {
		return line
	}
	tools := make([]Tool, 0, len(rawTools))
	for _, raw := range rawTools {
		var t struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Schema      json.RawMessage `json:"inputSchema"`
		}
		if json.Unmarshal(raw, &t) != nil || t.Name == "" {
			continue
		}
		tools = append(tools, Tool{Name: t.Name, Description: t.Description, Schema: t.Schema})
	}
	pins.mu.Lock()
	if pins.Pins == nil {
		pins.Pins = map[string]string{}
	}
	filtered, tampers, pending := Diff(server, tools, pins.Pins, pins.Unattended)
	fresh := map[string]string{}
	changedHashes := map[string]string{}
	for name, h := range pending {
		if _, ok := pins.Pins[name]; !ok {
			fresh[name] = h
			pins.Pins[name] = h
			continue
		}
		changedHashes[name] = h
	}
	if pins.alerted == nil {
		pins.alerted = map[string]bool{}
	}
	var report []Tamper
	for _, tp := range tampers {
		key := tp.Tool + "\x00" + tp.New
		if pins.alerted[key] {
			continue
		}
		pins.alerted[key] = true
		report = append(report, tp)
	}
	cb := pins.OnChange
	pins.mu.Unlock()
	if cb != nil && (len(fresh)+len(changedHashes)+len(report) > 0) {
		cb(fresh, changedHashes, report)
	}
	keep := map[string]bool{}
	for _, t := range filtered {
		keep[t.Name] = true
	}
	kept := make([]json.RawMessage, 0, len(rawTools))
	for _, raw := range rawTools {
		var t struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &t) != nil || !keep[t.Name] {
			continue
		}
		kept = append(kept, raw)
	}
	toolsJSON, err := json.Marshal(kept)
	if err != nil {
		return line
	}
	result["tools"] = toolsJSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return line
	}
	env["result"] = resultJSON
	out, err := json.Marshal(env)
	if err != nil {
		return line
	}
	return out
}
