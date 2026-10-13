package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jusso-dev/nighthawk/internal/audit"
	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/cost"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/ipc"
	"github.com/jusso-dev/nighthawk/internal/policy"
	"github.com/jusso-dev/nighthawk/internal/store"
)

func (s *Server) Handle(ctx context.Context, req ipc.Request) ipc.Response {
	switch req.Op {
	case "ping":
		return ipc.Response{OK: true}
	case "mode.get":
		return ipc.Response{OK: true, Mode: s.currentMode()}
	case "mode.set":
		if err := s.setMode(req.Mode); err != nil {
			return ipc.Response{Error: err.Error()}
		}
		return ipc.Response{OK: true, Mode: s.currentMode()}
	case "decide", "submit":
		if req.Event == nil {
			return ipc.Response{Error: "missing event"}
		}
		s.watchTranscript(*req.Event)
		dec, approval := s.decide(ctx, *req.Event)
		resp := ipc.Response{OK: true, Decision: &dec, Mode: s.currentMode()}
		if approval != "" {
			resp.Data = mustJSON(map[string]string{"approval_id": approval})
		}
		return resp
	case "sessions.list":
		return dataOK(s.sessions.List())
	case "events.list":
		from, err := parseSince(req.Since, time.Now())
		if err != nil {
			return ipc.Response{Error: err.Error()}
		}
		evs, err := s.st.ListEvents(ctx, store.Filter{
			Agent: req.Agent, Kind: event.Kind(req.Kind), From: from,
		})
		if err != nil {
			return ipc.Response{Error: "store"}
		}
		return dataOK(evs)
	case "spend":
		return s.spend(ctx, req)
	case "approval.wait":
		return s.waitApproval(ctx, req)
	case "approval.expire":
		if err := s.st.ExpireApproval(ctx, req.ID); err != nil {
			return ipc.Response{Error: "approval"}
		}
		return ipc.Response{OK: true}
	case "approval.resolve":
		status := req.Kind
		if status != "approved" && status != "denied" {
			status = "denied"
		}
		if err := s.st.ResolveApproval(ctx, req.ID, status, req.Who); err != nil {
			if errors.Is(err, store.ErrNoPendingApproval) {
				return ipc.Response{Error: "no pending approval " + req.ID}
			}
			return ipc.Response{Error: "approval"}
		}
		payload, _ := audit.Canonical(map[string]string{"approval_id": req.ID, "status": status, "decided_by": req.Who})
		s.auditRaw(payload)
		return ipc.Response{OK: true}
	case "approvals.list":
		rows, err := s.st.ListApprovals(ctx, req.Kind)
		if err != nil {
			return ipc.Response{Error: "store"}
		}
		return dataOK(rows)
	case "audit.head":
		rec, err := s.au.Head(s.st.DB())
		if err != nil {
			return dataOK(map[string]any{"seq": 0})
		}
		return dataOK(map[string]any{"seq": rec.Seq, "hash": rec.Hash})
	case "audit.verify":
		var from uint64
		if req.Since != "" {
			n, err := strconv.ParseUint(req.Since, 10, 64)
			if err != nil {
				return ipc.Response{Error: "from"}
			}
			from = n
		}
		if _, err := s.au.VerifyDB(from); err != nil {
			return ipc.Response{Error: err.Error()}
		}
		return ipc.Response{OK: true}
	case "status":
		return s.status()
	case "env":
		return s.env(req.Pane)
	case "doctor":
		return s.doctor(ctx)
	case "pins.list":
		rows, err := s.st.ListPins(ctx)
		if err != nil {
			return ipc.Response{Error: "store"}
		}
		return dataOK(rows)
	case "pins.approve":
		return s.approvePin(ctx, req)
	case "pins.sync":
		return s.syncPins(ctx, req)
	case "skills.list":
		rows, err := s.st.ListSkills(ctx)
		if err != nil {
			return ipc.Response{Error: "store"}
		}
		return dataOK(rows)
	case "herdr.event":
		now := time.Now()
		s.sessions.ApplyPlugin(req.Kind, req.Pane, now)
		s.savePane(s.sessions.Get(req.Pane))
		s.wakeSubs()
		return ipc.Response{OK: true}
	default:
		return ipc.Response{Error: "unknown op"}
	}
}

func (s *Server) setMode(m policy.Mode) error {
	if m != policy.Attended && m != policy.Unattended {
		return errMode
	}
	if err := config.WriteMode(s.opt.StateDir, m); err != nil {
		return err
	}
	s.mu.Lock()
	s.mode = m
	s.mu.Unlock()
	return nil
}

func (s *Server) spend(ctx context.Context, req ipc.Request) ipc.Response {
	since, err := parseSince(req.Since, time.Now())
	if err != nil {
		return ipc.Response{Error: err.Error()}
	}
	if req.Since == "" {
		since = time.Now().Add(-24 * time.Hour)
	}
	by := req.By
	if by == "" {
		by = "model"
	}
	rows, err := s.st.UsageGrouped(ctx, by, since)
	if err != nil {
		return ipc.Response{Error: "store"}
	}
	// Rows come split by day. Price each part at its own date, then merge.
	lines := make([]cost.Line, 0, len(rows))
	at := map[[2]string]int{}
	for _, r := range rows {
		l := s.prices.Line(cost.Usage{
			Key: r.Key, Model: r.Model, Input: r.Input, Output: r.Output,
			CacheRead: r.CacheRead, CacheWrite: r.CacheWrite,
		}, r.At, s.opt.Config.CurrencyRateAUD)
		k := [2]string{r.Key, r.Model}
		i, ok := at[k]
		if !ok {
			at[k] = len(lines)
			lines = append(lines, l)
			continue
		}
		m := &lines[i]
		m.Input += l.Input
		m.Output += l.Output
		m.CacheRead += l.CacheRead
		m.CacheWrite += l.CacheWrite
		m.USD += l.USD
		m.AUD += l.AUD
		if m.Source == "" {
			m.Source = l.Source
		}
		m.Unpriced = m.Unpriced && l.Unpriced
	}
	return dataOK(lines)
}

// maxApprovalWait bounds one approval.wait call.
const maxApprovalWait = 10 * time.Minute

// waitApproval polls until the approval is decided, expires, or the wait
// ends. An approval past its expiry is marked expired. A wait that ends
// first returns it still pending; the caller decides whether to expire it.
// It returns a little before ctx ends so the reply still reaches the client.
func (s *Server) waitApproval(ctx context.Context, req ipc.Request) ipc.Response {
	wait := req.Wait
	if wait <= 0 {
		wait = 30 * time.Second
	}
	if wait > maxApprovalWait {
		wait = maxApprovalWait
	}
	margin := wait / 10
	if margin > 500*time.Millisecond {
		margin = 500 * time.Millisecond
	}
	deadline := time.Now().Add(wait - margin)
	if dl, ok := ctx.Deadline(); ok && dl.Add(-margin).Before(deadline) {
		deadline = dl.Add(-margin)
	}
	for {
		a, err := s.st.GetApproval(ctx, req.ID)
		if err != nil {
			return ipc.Response{Error: "approval"}
		}
		if a.Status == "pending" && !time.Now().Before(a.ExpiresAt) {
			if err := s.st.ExpireApproval(ctx, a.ID); err != nil {
				return ipc.Response{Error: "approval"}
			}
			continue
		}
		if a.Status != "pending" || !time.Now().Before(deadline) {
			return dataOK(a)
		}
		select {
		case <-ctx.Done():
			return dataOK(a)
		case <-time.After(40 * time.Millisecond):
		}
	}
}

func (s *Server) approvePin(ctx context.Context, req ipc.Request) ipc.Response {
	var body struct {
		Server string `json:"server"`
		Tool   string `json:"tool"`
	}
	if len(req.Extra) > 0 {
		if err := json.Unmarshal(req.Extra, &body); err != nil {
			return ipc.Response{Error: "pin"}
		}
	}
	p, ok, err := s.st.GetPin(ctx, body.Server, body.Tool)
	if err != nil || !ok {
		return ipc.Response{Error: "pin"}
	}
	if p.PendingHash != "" {
		p.Hash = p.PendingHash
		p.PendingHash = ""
	}
	if err := s.st.PutPin(ctx, p); err != nil {
		return ipc.Response{Error: "store"}
	}
	s.auditRaw([]byte("pin approve " + body.Server + "/" + body.Tool))
	return ipc.Response{OK: true}
}

func (s *Server) syncPins(ctx context.Context, req ipc.Request) ipc.Response {
	var body struct {
		Server  string            `json:"server"`
		Fresh   map[string]string `json:"fresh"`
		Changed map[string]string `json:"changed"`
		Tampers []struct {
			Tool string `json:"Tool"`
			Old  string `json:"Old"`
			New  string `json:"New"`
		} `json:"tampers"`
	}
	if err := json.Unmarshal(req.Extra, &body); err != nil {
		return ipc.Response{Error: "pin"}
	}
	for tool, h := range body.Fresh {
		_ = s.st.PutPin(ctx, store.Pin{Server: body.Server, Tool: tool, Hash: h})
	}
	for tool, h := range body.Changed {
		p, ok, err := s.st.GetPin(ctx, body.Server, tool)
		if err != nil {
			return ipc.Response{Error: "store"}
		}
		if !ok {
			p = store.Pin{Server: body.Server, Tool: tool, Hash: h}
		} else {
			p.PendingHash = h
		}
		if err := s.st.PutPin(ctx, p); err != nil {
			return ipc.Response{Error: "store"}
		}
	}
	for _, tp := range body.Tampers {
		s.decide(ctx, event.Event{
			Kind: event.KindTamper, Source: event.SourceMCPProxy,
			Attrs: map[string]string{
				"mcp_server": body.Server, "mcp_tool": tp.Tool,
				"old_hash": tp.Old, "new_hash": tp.New,
			},
		})
	}
	return ipc.Response{OK: true}
}

func (s *Server) status() ipc.Response {
	rec, err := s.au.Head(s.st.DB())
	var seq uint64
	var hash string
	if err == nil {
		seq = rec.Seq
		hash = rec.Hash
	}
	return dataOK(map[string]any{
		"mode": s.currentMode(), "sessions": len(s.sessions.List()),
		"audit_seq": seq, "audit_hash": hash, "version": "daemon",
	})
}

func (s *Server) env(pane string) ipc.Response {
	if pane == "" {
		return ipc.Response{Error: "pane"}
	}
	tok := s.remember(pane)
	if tok == "" {
		return ipc.Response{Error: "token"}
	}
	port := s.opt.Config.Egress.Port
	if port == 0 {
		port = 8765
	}
	proxy := "http://" + tok + "@127.0.0.1:" + strconv.Itoa(port)
	return dataOK(map[string]string{
		"HTTPS_PROXY": proxy,
		"HTTP_PROXY":  proxy,
		"NO_PROXY":    "localhost,127.0.0.1",
		"pane":        pane,
	})
}

func (s *Server) doctor(ctx context.Context) ipc.Response {
	// The store has one connection. Finish ListEvents before opening rows,
	// or the second query waits on the connection rows holds forever.
	nets := map[string]bool{}
	evs, err := s.st.ListEvents(ctx, store.Filter{Kind: event.KindNetwork})
	if err != nil {
		return ipc.Response{Error: "store"}
	}
	for _, e := range evs {
		nets[e.PaneID] = true
	}
	rows, err := s.st.DB().QueryContext(ctx, `SELECT DISTINCT pane_id, agent FROM usage WHERE pane_id != ''`)
	if err != nil {
		return ipc.Response{Error: "store"}
	}
	defer rows.Close()
	var missing []map[string]string
	for rows.Next() {
		var pane, agent string
		if err := rows.Scan(&pane, &agent); err != nil {
			return ipc.Response{Error: "store"}
		}
		if !nets[pane] {
			missing = append(missing, map[string]string{"pane": pane, "agent": agent})
		}
	}
	return dataOK(map[string]any{"usage_without_network": missing})
}

func dataOK(v any) ipc.Response {
	return ipc.Response{OK: true, Data: mustJSON(v)}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

// parseSince reads a window start: "" for all time, "7d" days, a Go
// duration such as "36h", or an RFC3339 time.
func parseSince(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if n, ok := strings.CutSuffix(s, "d"); ok {
		if days, err := strconv.Atoi(n); err == nil && days >= 0 {
			return now.AddDate(0, 0, -days), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, errors.New("since: want 7d, a duration like 36h, or an RFC3339 time")
}

type modeError string

func (e modeError) Error() string { return string(e) }

const errMode modeError = "mode must be attended or unattended"
