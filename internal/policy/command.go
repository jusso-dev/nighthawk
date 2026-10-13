package policy

import "strings"

// Segments returns the full command plus pieces split on ; && || and |.
// bash -c / sh -c wrappers are unwrapped first, and the wrapped form is kept
// so rules can still see `sh -c "$(curl ...)"`. Quotes are respected.
func Segments(cmd string) []string {
	cmd = collapse(cmd)
	var out []string
	if inner, ok := unwrapShell(cmd); ok {
		out = append(out, cmd)
		cmd = collapse(inner)
	}
	parts := splitOps(cmd)
	out = append(out, cmd)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && p != cmd {
			out = append(out, p)
		}
	}
	return out
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func unwrapShell(cmd string) (string, bool) {
	for _, sh := range []string{"bash -c ", "sh -c ", "zsh -c "} {
		if strings.HasPrefix(cmd, sh) {
			rest := strings.TrimSpace(cmd[len(sh):])
			return trimQuotes(rest), true
		}
	}
	return "", false
}

func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func splitOps(s string) []string {
	var parts []string
	var b strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			b.WriteByte(c)
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			b.WriteByte(c)
			continue
		}
		if c == ';' || c == '|' || (c == '&' && i+1 < len(s) && s[i+1] == '&') {
			parts = append(parts, b.String())
			b.Reset()
			if c == '&' {
				i++
			}
			continue
		}
		b.WriteByte(c)
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts
}
