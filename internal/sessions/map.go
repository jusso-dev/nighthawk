package sessions

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/jusso-dev/nighthawk/internal/herdr"
)

// Pane is one live or closed pane.
type Pane struct {
	ID             string
	Workspace      string
	Agent          string
	AgentSessionID string
	Cwd            string
	Status         string
	Started        time.Time
	Ended          time.Time
	Machine        string
	TaskID         string
}

// Map is the daemon's pane map.
type Map struct {
	mu    sync.RWMutex
	panes map[string]*Pane
}

// NewMap returns an empty map.
func NewMap() *Map { return &Map{panes: map[string]*Pane{}} }

// Apply folds one Herdr envelope into the map.
func (m *Map) Apply(env herdr.Envelope, now time.Time) {
	var data struct {
		PaneID         string `json:"pane_id"`
		WorkspaceID    string `json:"workspace_id"`
		Agent          string `json:"agent"`
		AgentSessionID string `json:"agent_session_id"`
		Cwd            string `json:"cwd"`
		Status         string `json:"status"`
		AgentStatus    string `json:"agent_status"`
	}
	_ = json.Unmarshal(env.Data, &data)
	if data.PaneID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.ensure(data.PaneID, now)
	if data.WorkspaceID != "" {
		p.Workspace = data.WorkspaceID
	}
	if data.Agent != "" {
		p.Agent = data.Agent
	}
	if data.AgentSessionID != "" {
		p.AgentSessionID = data.AgentSessionID
	}
	if data.Cwd != "" {
		p.Cwd = data.Cwd
	}
	if data.AgentStatus != "" {
		p.Status = data.AgentStatus
	}
	if data.Status != "" {
		p.Status = data.Status
	}
	switch env.Event {
	case "pane.closed", "pane.exited", "pane_closed", "pane_exited":
		p.Ended = now
		if p.Status == "" {
			p.Status = "closed"
		}
	}
}

// ApplyPlugin records a manifest event. kind is created, exited, or closed.
func (m *Map) ApplyPlugin(kind, paneID string, now time.Time) {
	if paneID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.ensure(paneID, now)
	if kind == "exited" || kind == "closed" {
		p.Ended = now
		p.Status = kind
	}
}

// Reconcile rebuilds live panes from agent list. Missing live panes are closed.
func (m *Map) Reconcile(agents []herdr.AgentInfo, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	for _, a := range agents {
		if a.PaneID == "" {
			continue
		}
		seen[a.PaneID] = true
		p := m.ensure(a.PaneID, now)
		p.Agent = a.Agent
		p.Status = a.Status
		p.Cwd = a.Cwd
		p.Workspace = a.WorkspaceID
		p.AgentSessionID = a.AgentSessionID
		p.Ended = time.Time{}
	}
	for id, p := range m.panes {
		if !seen[id] && p.Ended.IsZero() && p.Status != "" {
			p.Ended = now
			p.Status = "closed"
		}
	}
}

func (m *Map) ensure(id string, now time.Time) *Pane {
	p, ok := m.panes[id]
	if !ok {
		p = &Pane{ID: id, Started: now, Agent: "unknown"}
		m.panes[id] = p
	}
	return p
}

// Get returns a pane. Unknown ids are reported as agent=unknown and not dropped.
func (m *Map) Get(id string) Pane {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.panes[id]
	if !ok {
		return Pane{ID: id, Agent: "unknown"}
	}
	return *p
}

// List returns a snapshot.
func (m *Map) List() []Pane {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Pane, 0, len(m.panes))
	for _, p := range m.panes {
		out = append(out, *p)
	}
	return out
}
