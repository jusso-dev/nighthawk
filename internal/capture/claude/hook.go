package claude

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
)

// DenyReason is a fixed deny string. It never includes tool input.
func DenyReason(ruleID string) string {
	if ruleID == "" {
		ruleID = "nh.failclosed"
	}
	return "blocked by Nighthawk: " + ruleID
}

// DenyStdout is the Claude Code PreToolUse deny JSON, plus a newline.
func DenyStdout(ruleID string) []byte {
	b, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": DenyReason(ruleID),
		},
	})
	return append(b, '\n')
}

// HookInput is the Claude Code hook JSON. Checked against the hooks reference on 2026-10-13.
type HookInput struct {
	HookEventName  string         `json:"hook_event_name"`
	SessionID      string         `json:"session_id"`
	TranscriptPath string         `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
	ToolResponse   map[string]any `json:"tool_response"`
}

// ToEvents maps one hook payload to normalised events.
func ToEvents(in HookInput, pane, task string) []event.Event {
	now := time.Now().UTC()
	pre := in.HookEventName == "PreToolUse" || in.HookEventName == "UserPromptSubmit"
	base := event.Event{
		Time: now, Source: event.SourceAgentHook, Agent: "claude",
		SessionID: in.SessionID, PaneID: pane, TaskID: task, Pre: pre,
		Attrs: map[string]string{"hook": in.HookEventName, "cwd": in.Cwd},
	}
	if in.TranscriptPath != "" {
		base.Attrs["transcript_path"] = in.TranscriptPath
	}
	switch in.HookEventName {
	case "SessionStart", "Stop", "SessionEnd":
		base.Kind = event.KindPane
		base.Pre = false
		return []event.Event{base}
	}
	name := in.ToolName
	switch {
	case name == "Bash":
		base.Kind = event.KindCommand
		base.Attrs["tool"] = name
		base.Attrs["command"] = str(in.ToolInput, "command")
	case name == "Write" || name == "Edit" || name == "MultiEdit":
		base.Kind = event.KindFileWrite
		base.Attrs["tool"] = name
		base.Attrs["path"] = first(in.ToolInput, "file_path", "path")
	case name == "Read":
		path := first(in.ToolInput, "file_path", "path")
		if strings.HasSuffix(path, "SKILL.md") || strings.Contains(path, "/skills/") {
			base.Kind = event.KindSkill
			base.Attrs["path"] = path
			base.Attrs["name"] = skillName(path)
		} else {
			base.Kind = event.KindToolCall
			base.Attrs["tool"] = name
			base.Attrs["path"] = path
		}
	case name == "Skill":
		base.Kind = event.KindSkill
		base.Attrs["name"] = first(in.ToolInput, "skill", "name")
		base.Attrs["path"] = first(in.ToolInput, "path", "file_path")
	case name == "WebFetch" || name == "WebSearch":
		base.Kind = event.KindNetwork
		base.Attrs["tool"] = name
		raw := first(in.ToolInput, "url", "query")
		base.Attrs["url"] = raw
		if u, err := url.Parse(raw); err == nil {
			base.Attrs["host"] = u.Hostname()
		}
	case strings.HasPrefix(name, "mcp__"):
		base.Kind = event.KindMCPCall
		srv, tool := splitMCP(name)
		base.Attrs["tool"] = name
		base.Attrs["mcp_server"] = srv
		base.Attrs["mcp_tool"] = tool
		if b, err := json.Marshal(in.ToolInput); err == nil {
			base.Attrs["args"] = string(b)
		}
	default:
		if name == "" {
			base.Kind = event.KindPane
		} else {
			base.Kind = event.KindToolCall
			base.Attrs["tool"] = name
		}
	}
	return []event.Event{base}
}

func str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}

func first(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(m, k); s != "" {
			return s
		}
	}
	return ""
}

func splitMCP(name string) (string, string) {
	rest := strings.TrimPrefix(name, "mcp__")
	srv, tool, ok := strings.Cut(rest, "__")
	if !ok {
		return rest, ""
	}
	return srv, tool
}

func skillName(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "skills" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// Parse unmarshals hook JSON.
func Parse(b []byte) (HookInput, error) {
	var in HookInput
	err := json.Unmarshal(b, &in)
	return in, err
}
