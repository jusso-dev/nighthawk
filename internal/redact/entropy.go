package redact

import (
	"math"
	"strings"
)

func shannon(s string) float64 {
	if s == "" {
		return 0
	}
	var freq [256]int
	for i := 0; i < len(s); i++ {
		freq[s[i]]++
	}
	n := float64(len(s))
	var h float64
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// needsScan is the fast path. lower is strings.ToLower(s), so hints match
// any case (PASSWORD=, BEARER).
func needsScan(s, lower string) bool {
	for _, r := range compiled {
		if r.hinted(lower) {
			return true
		}
	}
	return entropyHint(s)
}

func entropyHint(s string) bool {
	run := 0
	armed := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '=' || c == ':' {
			armed = true
			run = 0
			continue
		}
		if !armed {
			continue
		}
		if c == ' ' || c == '\n' || c == '\t' || c == '"' || c == '\'' {
			armed = false
			run = 0
			continue
		}
		run++
		if run >= 32 {
			return true
		}
	}
	return false
}

// maskEntropy replaces high-entropy values after '=' or ':'. One pass, one
// output buffer, so dense input stays linear.
func maskEntropy(s string) string {
	var out strings.Builder
	last := 0
	i := 0
	for i < len(s) {
		if s[i] != '=' && s[i] != ':' {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		k := j
		for k < len(s) && s[k] != ' ' && s[k] != '\n' && s[k] != '\t' && s[k] != '"' && s[k] != '\'' {
			k++
		}
		tok := s[j:k]
		if len(tok) >= 32 && tok != Mask && shannon(tok) > 4.0 {
			if last == 0 {
				out.Grow(len(s))
			}
			out.WriteString(s[last:j])
			out.WriteString(Mask)
			last = k
		}
		i = k
	}
	if last == 0 {
		return s
	}
	out.WriteString(s[last:])
	return out.String()
}
