package mcpproxy

import (
	"bytes"
	"context"
	"encoding/json"
)

type rpcProbe struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func isNotification(id json.RawMessage) bool {
	return id == nil
}

func loggedMethod(m string) bool {
	switch m {
	case "initialize", "tools/list", "tools/call":
		return true
	default:
		return false
	}
}

// gateMessage filters one JSON-RPC value (object or array).
// forward is written to the child/upstream when non-nil.
// local are responses written to the client without forwarding.
func gateMessage(ctx context.Context, raw []byte, server string, d Decider, logf func(LogLine)) (forward []byte, local [][]byte) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '[' {
		return gateBatch(ctx, raw, server, d, logf)
	}
	return gateOne(ctx, raw, server, d, logf)
}

func gateOne(ctx context.Context, raw []byte, server string, d Decider, logf func(LogLine)) (forward []byte, local [][]byte) {
	var p rpcProbe
	if err := json.Unmarshal(raw, &p); err != nil || p.Method == "" {
		return raw, nil
	}
	if isNotification(p.ID) {
		emitLog(logf, p, server, "", nil, 0)
		return raw, nil
	}
	if p.Method != "tools/call" {
		emitLog(logf, p, server, "", nil, 0)
		return raw, nil
	}

	var cp callParams
	_ = json.Unmarshal(p.Params, &cp)
	args := string(cp.Arguments)
	dec, _ := decideCall(ctx, d, server, cp.Name, args)
	if Denied(dec) {
		rule := dec.RuleID
		if rule == "" {
			rule = "nh.failclosed"
		}
		emitLog(logf, p, server, cp.Name, &LogLine{Args: clipLog(args), Err: "blocked by Nighthawk: " + rule}, 0)
		return nil, [][]byte{DenyError(p.ID, rule)}
	}
	emitLog(logf, p, server, cp.Name, &LogLine{Args: clipLog(args)}, 0)
	return raw, nil
}

func gateBatch(ctx context.Context, raw []byte, server string, d Decider, logf func(LogLine)) (forward []byte, local [][]byte) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw, nil
	}
	kept := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		fwd, loc := gateOne(ctx, item, server, d, logf)
		if len(loc) > 0 {
			local = append(local, loc...)
			continue
		}
		if fwd != nil {
			kept = append(kept, json.RawMessage(append([]byte(nil), fwd...)))
		}
	}
	if len(kept) == 0 {
		if len(local) == 0 {
			return raw, nil
		}
		// Whole batch denied: one error, do not forward.
		return nil, local[:1]
	}
	b, err := json.Marshal(kept)
	if err != nil {
		return raw, local
	}
	return b, local
}

func emitLog(logf func(LogLine), p rpcProbe, server, tool string, extra *LogLine, resultSize int) {
	if logf == nil || !loggedMethod(p.Method) {
		return
	}
	ll := LogLine{
		Method:     p.Method,
		Server:     server,
		Tool:       tool,
		ID:         string(bytes.TrimSpace(p.ID)),
		ResultSize: resultSize,
	}
	if extra != nil {
		ll.Args = extra.Args
		ll.Err = extra.Err
		if extra.Tool != "" {
			ll.Tool = extra.Tool
		}
		if extra.ResultSize != 0 {
			ll.ResultSize = extra.ResultSize
		}
	}
	if p.Method == "tools/call" && ll.Tool == "" {
		var cp callParams
		_ = json.Unmarshal(p.Params, &cp)
		ll.Tool = cp.Name
		if ll.Args == "" && len(cp.Arguments) > 0 {
			ll.Args = clipLog(string(cp.Arguments))
		}
	}
	logf(ll)
}
