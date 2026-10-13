package policy

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/jusso-dev/nighthawk/internal/event"
	"gopkg.in/yaml.v3"
)

// Rule is one ordered policy rule.
type Rule struct {
	ID       string `yaml:"id"`
	Severity string `yaml:"severity"`
	Why      string `yaml:"why"`
	Match    Match  `yaml:"match"`
	Action   Action `yaml:"action"`
}

// SpendOver matches when spend in a scope exceeds USD.
type SpendOver struct {
	Scope string  `yaml:"scope"`
	USD   float64 `yaml:"usd"`
}

// LoopOver matches repeated calls.
type LoopOver struct {
	Count  int    `yaml:"count"`
	Within string `yaml:"within"`
}

// Match is the fixed matcher set from ADR 0002.
type Match struct {
	Kind         string     `yaml:"kind"`
	Agent        string     `yaml:"agent"`
	Tool         string     `yaml:"tool"`
	MCPServer    string     `yaml:"mcp_server"`
	MCPTool      string     `yaml:"mcp_tool"`
	CommandRegex string     `yaml:"command_regex"`
	PathGlob     []string   `yaml:"path_glob"`
	Host         string     `yaml:"host"`
	HostNotIn    string     `yaml:"host_not_in"`
	Mode         string     `yaml:"mode"`
	SpendOver    *SpendOver `yaml:"spend_over"`
	Loop         *LoopOver  `yaml:"loop"`
}

// File is a policy document.
type File struct {
	Version int    `yaml:"version"`
	Mode    string `yaml:"mode"`
	Default Action `yaml:"default"`
	Rules   []Rule `yaml:"rules"`
}

// Facts are lookups the engine cannot do from the event alone.
type Facts struct {
	HostAllowed func(host string) bool
	SpendUSD    func(scope, id string) float64
	LoopCount   func(e event.Event, within time.Duration) int
}

type compiled struct {
	Rule
	re      *regexp.Regexp
	loopWin time.Duration
	globs   []string
}

// Engine evaluates rules in order. First match wins.
type Engine struct {
	rules  []compiled
	def    Action
	source string
	Facts  Facts
}

// LoadYAML compiles a policy file. Invalid regex names the file and rule id.
func LoadYAML(source string, b []byte) (*Engine, error) {
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if f.Default == "" {
		f.Default = Block
	}
	eng := &Engine{def: f.Default, source: source}
	for _, r := range f.Rules {
		c := compiled{Rule: r, globs: r.Match.PathGlob}
		if r.Match.CommandRegex != "" {
			re, err := regexp.Compile(r.Match.CommandRegex)
			if err != nil {
				return nil, fmt.Errorf("%s: rule %s: %w", source, r.ID, err)
			}
			c.re = re
		}
		if r.Match.Loop != nil && r.Match.Loop.Within != "" {
			d, err := time.ParseDuration(r.Match.Loop.Within)
			if err != nil {
				return nil, fmt.Errorf("%s: rule %s: %w", source, r.ID, err)
			}
			c.loopWin = d
		}
		if r.Action == "" {
			return nil, fmt.Errorf("%s: rule %s: missing action", source, r.ID)
		}
		eng.rules = append(eng.rules, c)
	}
	return eng, nil
}

// Rules returns the loaded rules.
func (e *Engine) Rules() []Rule {
	if e == nil {
		return nil
	}
	out := make([]Rule, len(e.rules))
	for i, r := range e.rules {
		out[i] = r.Rule
	}
	return out
}

// Explain returns one rule.
func (e *Engine) Explain(id string) (Rule, bool) {
	for _, r := range e.rules {
		if r.ID == id {
			return r.Rule, true
		}
	}
	return Rule{}, false
}

// Evaluate implements Evaluator.
func (e *Engine) Evaluate(ev event.Event, m Mode) (Decision, error) {
	if e == nil {
		return Decision{Action: Allow, RuleID: "nh.default", Reason: "no policy"}, nil
	}
	for _, r := range e.rules {
		if r.matches(ev, m, e.Facts) {
			action := r.Action
			if m == Attended && r.Severity != "critical" && r.Severity != "info" && (action == Block || action == KillSession || action == PausePane) {
				action = AskHuman
			}
			if m == Attended && r.Severity == "info" && action == Allow {
				return Decision{Action: Allow, RuleID: r.ID, Reason: r.ID}, nil
			}
			return Decision{Action: action, RuleID: r.ID, Reason: r.ID}, nil
		}
	}
	if m == Unattended {
		return Decision{Action: e.def, RuleID: "nh.default", Reason: "no matching rule"}, nil
	}
	return Decision{Action: Allow, RuleID: "nh.default", Reason: "attended default"}, nil
}

func (r compiled) matches(ev event.Event, m Mode, facts Facts) bool {
	mt := r.Match
	if mt.Kind != "" && string(ev.Kind) != mt.Kind {
		return false
	}
	if mt.Agent != "" && ev.Agent != mt.Agent {
		return false
	}
	if mt.Mode != "" && string(m) != mt.Mode {
		return false
	}
	if mt.Tool != "" && attr(ev, "tool") != mt.Tool {
		return false
	}
	if mt.MCPServer != "" && attr(ev, "mcp_server") != mt.MCPServer {
		return false
	}
	if mt.MCPTool != "" && attr(ev, "mcp_tool") != mt.MCPTool {
		return false
	}
	if mt.Host != "" && attr(ev, "host") != mt.Host {
		return false
	}
	if mt.HostNotIn == "allowlist" {
		host := attr(ev, "host")
		if facts.HostAllowed == nil || facts.HostAllowed(host) {
			return false
		}
	}
	if len(mt.PathGlob) > 0 {
		path := attr(ev, "path")
		if !globAny(mt.PathGlob, path) {
			return false
		}
	}
	if r.re != nil {
		if !regexAny(r.re, ev) {
			return false
		}
	}
	if mt.SpendOver != nil {
		if facts.SpendUSD == nil {
			return false
		}
		id := scopeID(mt.SpendOver.Scope, ev)
		if facts.SpendUSD(mt.SpendOver.Scope, id) < mt.SpendOver.USD {
			return false
		}
	}
	if mt.Loop != nil {
		if facts.LoopCount == nil {
			return false
		}
		if facts.LoopCount(ev, r.loopWin) < mt.Loop.Count {
			return false
		}
	}
	return true
}

func attr(ev event.Event, k string) string {
	if ev.Attrs == nil {
		return ""
	}
	return ev.Attrs[k]
}

func globAny(globs []string, path string) bool {
	if path == "" {
		return false
	}
	cands := []string{path}
	if strings.HasPrefix(path, "/") {
		cands = append(cands, strings.TrimPrefix(path, "/"))
	}
	for _, g := range globs {
		for _, c := range cands {
			ok, err := doublestar.PathMatch(g, c)
			if err == nil && ok {
				return true
			}
			ok, err = doublestar.Match(g, c)
			if err == nil && ok {
				return true
			}
		}
	}
	return false
}

func regexAny(re *regexp.Regexp, ev event.Event) bool {
	fields := []string{attr(ev, "command"), attr(ev, "payload"), attr(ev, "text")}
	for _, f := range fields {
		if f == "" {
			continue
		}
		for _, seg := range Segments(f) {
			if re.MatchString(seg) {
				return true
			}
		}
	}
	return false
}

func scopeID(scope string, ev event.Event) string {
	switch scope {
	case "agent":
		return ev.Agent
	case "task":
		return ev.TaskID
	case "day":
		return ev.Time.UTC().Format("2006-01-02")
	default:
		return ev.SessionID
	}
}
