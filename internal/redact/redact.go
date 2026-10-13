package redact

import (
	"regexp"
	"sync"

	"github.com/jusso-dev/nighthawk/internal/event"
)

// Mask replaces a detected secret.
const Mask = "[REDACTED]"

// Rule is one replacement.
type Rule struct {
	Name string
	Re   string
	Repl string
}

var compiled []compiledRule

type compiledRule struct {
	re   *regexp.Regexp
	repl string
}

func init() {
	compiled = compile(builtin)
}

func compile(rules []Rule) []compiledRule {
	out := make([]compiledRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, compiledRule{re: regexp.MustCompile(r.Re), repl: r.Repl})
	}
	return out
}

// Redactor applies built-in rules plus optional extra patterns.
type Redactor struct {
	extra []*regexp.Regexp
}

// Default is the process redactor. SetExtra updates it.
var (
	Default Redactor
	extraMu sync.RWMutex
)

// SetExtra replaces configured extra patterns.
func SetExtra(patterns []string) error {
	rs := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return err
		}
		rs = append(rs, re)
	}
	extraMu.Lock()
	Default.extra = rs
	extraMu.Unlock()
	return nil
}

// String returns s with every known secret pattern masked.
func String(s string) string { return Default.String(s) }

// String redacts s.
func (r *Redactor) String(s string) string {
	extraMu.RLock()
	extra := r.extra
	extraMu.RUnlock()
	if len(extra) == 0 && !needsScan(s) {
		return s
	}
	for _, rule := range compiled {
		s = rule.re.ReplaceAllString(s, rule.repl)
	}
	for _, re := range extra {
		s = re.ReplaceAllString(s, Mask)
	}
	return maskEntropy(s)
}

// Event returns a copy of e with Attrs redacted.
func Event(e event.Event) event.Event {
	if len(e.Attrs) == 0 {
		return e
	}
	cp := e
	cp.Attrs = make(map[string]string, len(e.Attrs))
	for k, v := range e.Attrs {
		cp.Attrs[k] = String(v)
	}
	return cp
}
