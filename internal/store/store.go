package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
	_ "modernc.org/sqlite"
)

// Store is the SQLite database. One writer.
type Store struct {
	db   *sql.DB
	path string
}

// Open creates or opens path, applies migrations, and sets the file mode to 0600.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := chmodDB(path); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func chmodDB(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(p); err == nil {
			if err := os.Chmod(p, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Path() string { return s.path }

// SchemaVersion is the latest applied migration.
func (s *Store) SchemaVersion() (int, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v)
	if err != nil {
		return 0, err
	}
	var n int
	for _, c := range v {
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// Filter selects events.
type Filter struct {
	Agent string
	Kind  event.Kind
	From  time.Time
	To    time.Time
}

// Session is one pane row.
type Session struct {
	PaneID         string
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

// Usage is one token row.
type Usage struct {
	TS         time.Time
	PaneID     string
	Agent      string
	SessionID  string
	TaskID     string
	Model      string
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	MessageKey string
}

func (s *Store) InsertEvent(ctx context.Context, e event.Event) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		return insertEvent(tx, e)
	})
}

func (s *Store) InsertDecision(ctx context.Context, eventID string, d policy.Decision) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		return insertDecision(tx, eventID, d)
	})
}

func insertEvent(tx *sql.Tx, e event.Event) error {
	attrs, err := json.Marshal(e.Attrs)
	if err != nil {
		return err
	}
	pre := 0
	if e.Pre {
		pre = 1
	}
	_, err = tx.Exec(`INSERT INTO events(id, ts, kind, source, machine, pane_id, agent, session_id, task_id, pre, attrs)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Time.UnixMilli(), string(e.Kind), string(e.Source), e.Machine, e.PaneID, e.Agent, e.SessionID, e.TaskID, pre, string(attrs))
	return err
}

func insertDecision(tx *sql.Tx, eventID string, d policy.Decision) error {
	_, err := tx.Exec(`INSERT INTO decisions(id, event_id, ts, action, rule_id, reason) VALUES(?,?,?,?,?,?)`,
		event.NewID(), eventID, time.Now().UnixMilli(), string(d.Action), d.RuleID, d.Reason)
	return err
}

// Record stores an event, its decision, and returns the tx for the caller to append audit.
func (s *Store) Record(ctx context.Context, e event.Event, d policy.Decision, appendAudit func(tx *sql.Tx) error) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := insertEvent(tx, e); err != nil {
			return err
		}
		if err := insertDecision(tx, e.ID, d); err != nil {
			return err
		}
		if appendAudit != nil {
			return appendAudit(tx)
		}
		return nil
	})
}

func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) UpsertSession(ctx context.Context, sess Session) error {
	var ended any
	if !sess.Ended.IsZero() {
		ended = sess.Ended.UnixMilli()
	}
	var started any
	if !sess.Started.IsZero() {
		started = sess.Started.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(pane_id, workspace, agent, agent_session_id, cwd, status, started, ended, machine, task_id)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(pane_id) DO UPDATE SET
			workspace=excluded.workspace,
			agent=excluded.agent,
			agent_session_id=excluded.agent_session_id,
			cwd=excluded.cwd,
			status=excluded.status,
			started=COALESCE(sessions.started, excluded.started),
			ended=COALESCE(excluded.ended, sessions.ended),
			machine=excluded.machine,
			task_id=COALESCE(excluded.task_id, sessions.task_id)`,
		sess.PaneID, sess.Workspace, sess.Agent, sess.AgentSessionID, sess.Cwd, sess.Status, started, ended, sess.Machine, sess.TaskID)
	return err
}

func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT pane_id, workspace, agent, agent_session_id, cwd, status, started, ended, machine, task_id FROM sessions ORDER BY started`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		var started, ended sql.NullInt64
		if err := rows.Scan(&sess.PaneID, &sess.Workspace, &sess.Agent, &sess.AgentSessionID, &sess.Cwd, &sess.Status, &started, &ended, &sess.Machine, &sess.TaskID); err != nil {
			return nil, err
		}
		if started.Valid {
			sess.Started = time.UnixMilli(started.Int64)
		}
		if ended.Valid {
			sess.Ended = time.UnixMilli(ended.Int64)
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) InsertUsage(ctx context.Context, u Usage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO usage(ts, pane_id, agent, session_id, task_id, model, input_tokens, output_tokens, cache_read, cache_write, message_key)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		u.TS.UnixMilli(), u.PaneID, u.Agent, u.SessionID, u.TaskID, u.Model, u.Input, u.Output, u.CacheRead, u.CacheWrite, nullStr(u.MessageKey))
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) ListEvents(ctx context.Context, f Filter) ([]event.Event, error) {
	q := `SELECT id, ts, kind, source, machine, pane_id, agent, session_id, task_id, pre, attrs FROM events WHERE 1=1`
	var args []any
	if f.Agent != "" {
		q += ` AND agent = ?`
		args = append(args, f.Agent)
	}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, string(f.Kind))
	}
	if !f.From.IsZero() {
		q += ` AND ts >= ?`
		args = append(args, f.From.UnixMilli())
	}
	if !f.To.IsZero() {
		q += ` AND ts < ?`
		args = append(args, f.To.UnixMilli())
	}
	q += ` ORDER BY ts`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []event.Event
	for rows.Next() {
		var e event.Event
		var ts int64
		var kind, source, attrs string
		var pre int
		if err := rows.Scan(&e.ID, &ts, &kind, &source, &e.Machine, &e.PaneID, &e.Agent, &e.SessionID, &e.TaskID, &pre, &attrs); err != nil {
			return nil, err
		}
		e.Time = time.UnixMilli(ts)
		e.Kind = event.Kind(kind)
		e.Source = event.Source(source)
		e.Pre = pre == 1
		if attrs != "" && attrs != "null" {
			_ = json.Unmarshal([]byte(attrs), &e.Attrs)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UsageTotals is grouped token usage for one key, model and UTC day.
// At is the latest row time in the group, so callers can price by date.
type UsageTotals struct {
	Key        string
	Model      string
	At         time.Time
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// dayExpr is the UTC date of a usage row.
const dayExpr = `strftime('%Y-%m-%d', ts / 1000, 'unixepoch')`

// UsageGrouped sums usage since a time by agent, session, task, model or day.
// Rows are also split by UTC day so each part can be priced at its own date.
func (s *Store) UsageGrouped(ctx context.Context, by string, since time.Time) ([]UsageTotals, error) {
	col := "model"
	switch by {
	case "agent":
		col = "agent"
	case "session":
		col = "session_id"
	case "task":
		col = "task_id"
	case "day":
		col = dayExpr
	case "model":
		col = "model"
	default:
		col = "model"
	}
	q := `SELECT COALESCE(` + col + `, ''), COALESCE(model, ''), MAX(ts), SUM(input_tokens), SUM(output_tokens), SUM(cache_read), SUM(cache_write)
		FROM usage WHERE ts >= ? GROUP BY 1, 2, ` + dayExpr + ` ORDER BY 1, 2, 3`
	rows, err := s.db.QueryContext(ctx, q, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageTotals
	for rows.Next() {
		var u UsageTotals
		var at int64
		if err := rows.Scan(&u.Key, &u.Model, &at, &u.Input, &u.Output, &u.CacheRead, &u.CacheWrite); err != nil {
			return nil, err
		}
		u.At = time.UnixMilli(at)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) GetOffset(ctx context.Context, path string) (int64, error) {
	var off int64
	err := s.db.QueryRowContext(ctx, `SELECT byte_off FROM usage_offsets WHERE path = ?`, path).Scan(&off)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return off, err
}

func (s *Store) SetOffset(ctx context.Context, path string, off int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO usage_offsets(path, byte_off) VALUES(?,?)
		ON CONFLICT(path) DO UPDATE SET byte_off = excluded.byte_off`, path, off)
	return err
}

// Skill is one tracked skill.
type Skill struct {
	Name      string
	Path      string
	Hash      string
	FirstSeen time.Time
	LastSeen  time.Time
	Uses      int
}

func (s *Store) UpsertSkill(ctx context.Context, name, path, hash string, changed bool) (Skill, bool, error) {
	now := time.Now().UnixMilli()
	var prev string
	err := s.db.QueryRowContext(ctx, `SELECT hash FROM skills WHERE name = ? AND path = ?`, name, path).Scan(&prev)
	if err == sql.ErrNoRows {
		_, err = s.db.ExecContext(ctx, `INSERT INTO skills(name, path, hash, first_seen, last_seen, uses) VALUES(?,?,?,?,?,1)`, name, path, hash, now, now)
		return Skill{Name: name, Path: path, Hash: hash, Uses: 1}, false, err
	}
	if err != nil {
		return Skill{}, false, err
	}
	hashChanged := prev != hash
	_, err = s.db.ExecContext(ctx, `UPDATE skills SET hash = ?, last_seen = ?, uses = uses + 1 WHERE name = ? AND path = ?`, hash, now, name, path)
	if err != nil {
		return Skill{}, false, err
	}
	var uses int
	_ = s.db.QueryRowContext(ctx, `SELECT uses FROM skills WHERE name = ? AND path = ?`, name, path).Scan(&uses)
	return Skill{Name: name, Path: path, Hash: hash, Uses: uses}, hashChanged, nil
}

func (s *Store) ListSkills(ctx context.Context) ([]Skill, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, path, hash, first_seen, last_seen, uses FROM skills ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Skill
	for rows.Next() {
		var sk Skill
		var first, last int64
		if err := rows.Scan(&sk.Name, &sk.Path, &sk.Hash, &first, &last, &sk.Uses); err != nil {
			return nil, err
		}
		sk.FirstSeen = time.UnixMilli(first)
		sk.LastSeen = time.UnixMilli(last)
		out = append(out, sk)
	}
	return out, rows.Err()
}

// Pin is an MCP tool pin.
type Pin struct {
	Server      string
	Tool        string
	Hash        string
	PendingHash string
	FirstSeen   time.Time
}

func (s *Store) GetPin(ctx context.Context, server, tool string) (Pin, bool, error) {
	var p Pin
	var ts int64
	var pending sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT server, tool, hash, pending_hash, first_seen FROM mcp_pins WHERE server = ? AND tool = ?`, server, tool).
		Scan(&p.Server, &p.Tool, &p.Hash, &pending, &ts)
	if err == sql.ErrNoRows {
		return Pin{}, false, nil
	}
	if err != nil {
		return Pin{}, false, err
	}
	p.FirstSeen = time.UnixMilli(ts)
	if pending.Valid {
		p.PendingHash = pending.String
	}
	return p, true, nil
}

func (s *Store) PutPin(ctx context.Context, p Pin) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mcp_pins(server, tool, hash, pending_hash, first_seen) VALUES(?,?,?,?,?)
		ON CONFLICT(server, tool) DO UPDATE SET hash = excluded.hash, pending_hash = excluded.pending_hash`,
		p.Server, p.Tool, p.Hash, nullStr(p.PendingHash), time.Now().UnixMilli())
	return err
}

func (s *Store) ListPins(ctx context.Context) ([]Pin, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT server, tool, hash, pending_hash, first_seen FROM mcp_pins ORDER BY server, tool`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pin
	for rows.Next() {
		var p Pin
		var ts int64
		var pending sql.NullString
		if err := rows.Scan(&p.Server, &p.Tool, &p.Hash, &pending, &ts); err != nil {
			return nil, err
		}
		p.FirstSeen = time.UnixMilli(ts)
		if pending.Valid {
			p.PendingHash = pending.String
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Approval is a pending human decision.
type Approval struct {
	ID        string
	EventID   string
	RuleID    string
	Status    string
	DecidedBy string
	DecidedAt time.Time
	ExpiresAt time.Time
	EventJSON string
}

func (s *Store) InsertApproval(ctx context.Context, a Approval) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO approvals(id, event_id, rule_id, status, decided_by, decided_at, expires_at, event_json)
		VALUES(?,?,?,?,?,?,?,?)`, a.ID, a.EventID, a.RuleID, a.Status, a.DecidedBy, nullTime(a.DecidedAt), a.ExpiresAt.UnixMilli(), a.EventJSON)
	return err
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

func (s *Store) GetApproval(ctx context.Context, id string) (Approval, error) {
	var a Approval
	var decided sql.NullInt64
	var exp int64
	err := s.db.QueryRowContext(ctx, `SELECT id, event_id, rule_id, status, decided_by, decided_at, expires_at, event_json FROM approvals WHERE id = ?`, id).
		Scan(&a.ID, &a.EventID, &a.RuleID, &a.Status, &a.DecidedBy, &decided, &exp, &a.EventJSON)
	if err != nil {
		return Approval{}, err
	}
	a.ExpiresAt = time.UnixMilli(exp)
	if decided.Valid {
		a.DecidedAt = time.UnixMilli(decided.Int64)
	}
	return a, nil
}

// ErrNoPendingApproval means the id is unknown, already decided or expired.
var ErrNoPendingApproval = errors.New("no pending approval")

// ResolveApproval decides a pending, unexpired approval once.
func (s *Store) ResolveApproval(ctx context.Context, id, status, who string) error {
	now := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE approvals SET status = ?, decided_by = ?, decided_at = ? WHERE id = ? AND status = 'pending' AND expires_at > ?`,
		status, who, now, id, now)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return ErrNoPendingApproval
	}
	return nil
}

// ExpireApproval marks a still pending approval expired. A decided one is left alone.
func (s *Store) ExpireApproval(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE approvals SET status = 'expired', decided_at = ? WHERE id = ? AND status = 'pending'`,
		time.Now().UnixMilli(), id)
	return err
}

func (s *Store) ListApprovals(ctx context.Context, status string) ([]Approval, error) {
	q := `SELECT id, event_id, rule_id, status, decided_by, decided_at, expires_at, event_json FROM approvals`
	var args []any
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY expires_at`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		var a Approval
		var decided sql.NullInt64
		var exp int64
		if err := rows.Scan(&a.ID, &a.EventID, &a.RuleID, &a.Status, &a.DecidedBy, &decided, &exp, &a.EventJSON); err != nil {
			return nil, err
		}
		a.ExpiresAt = time.UnixMilli(exp)
		if decided.Valid {
			a.DecidedAt = time.UnixMilli(decided.Int64)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// OutboxRow is one SIEM delivery attempt.
type OutboxRow struct {
	ID       int64
	Payload  string
	Attempts int
	NextAt   time.Time
}

func (s *Store) EnqueueOutbox(ctx context.Context, payload string, next time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO siem_outbox(payload, attempts, next_at, delivered) VALUES(?,0,?,0)`, payload, next.UnixMilli())
	return err
}

func (s *Store) DueOutbox(ctx context.Context, now time.Time) ([]OutboxRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, payload, attempts, next_at FROM siem_outbox WHERE delivered = 0 AND next_at <= ? ORDER BY id`, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		var r OutboxRow
		var next int64
		if err := rows.Scan(&r.ID, &r.Payload, &r.Attempts, &next); err != nil {
			return nil, err
		}
		r.NextAt = time.UnixMilli(next)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) MarkDelivered(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE siem_outbox SET delivered = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) FailOutbox(ctx context.Context, id int64, next time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE siem_outbox SET attempts = attempts + 1, next_at = ? WHERE id = ?`, next.UnixMilli(), id)
	return err
}

// Anchor is a signed audit head.
type Anchor struct {
	Seq    uint64
	Hash   string
	Target string
	TS     time.Time
	Sig    string
}

func (s *Store) PutAnchor(ctx context.Context, a Anchor) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_anchors(seq, hash, target, ts, sig) VALUES(?,?,?,?,?)`,
		a.Seq, a.Hash, a.Target, a.TS.UnixMilli(), a.Sig)
	return err
}

func (s *Store) ListAnchors(ctx context.Context) ([]Anchor, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq, hash, target, ts, sig FROM audit_anchors ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Anchor
	for rows.Next() {
		var a Anchor
		var ts int64
		if err := rows.Scan(&a.Seq, &a.Hash, &a.Target, &ts, &a.Sig); err != nil {
			return nil, err
		}
		a.TS = time.UnixMilli(ts)
		out = append(out, a)
	}
	return out, rows.Err()
}
