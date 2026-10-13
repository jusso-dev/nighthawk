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

func needsScan(s string) bool {
	if strings.Contains(s, "AKIA") ||
		strings.Contains(s, "ghp_") || strings.Contains(s, "gho_") || strings.Contains(s, "ghu_") ||
		strings.Contains(s, "ghs_") || strings.Contains(s, "ghr_") ||
		strings.Contains(s, "sk-") || strings.Contains(s, "xai-") || strings.Contains(s, "xox") ||
		strings.Contains(s, "AIza") || strings.Contains(s, "eyJ") || strings.Contains(s, "PRIVATE KEY") ||
		strings.Contains(s, "password=") || strings.Contains(s, "Password=") ||
		strings.Contains(s, "Bearer ") || strings.Contains(s, "bearer ") ||
		strings.Contains(s, "://") || strings.Contains(s, "_live_") || strings.Contains(s, "_test_") {
		return true
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

func maskEntropy(s string) string {
	b := []byte(s)
	i := 0
	for i < len(b) {
		if b[i] != '=' && b[i] != ':' {
			i++
			continue
		}
		j := i + 1
		for j < len(b) && (b[j] == ' ' || b[j] == '\t') {
			j++
		}
		k := j
		for k < len(b) && b[k] != ' ' && b[k] != '\n' && b[k] != '\t' && b[k] != '"' && b[k] != '\'' {
			k++
		}
		tok := b[j:k]
		if len(tok) >= 32 && shannon(string(tok)) > 4.0 && !alreadyMasked(tok) {
			repl := []byte(Mask)
			b = append(b[:j], append(repl, b[k:]...)...)
			i = j + len(repl)
			continue
		}
		i = k
	}
	return string(b)
}

func alreadyMasked(tok []byte) bool {
	return string(tok) == Mask
}
