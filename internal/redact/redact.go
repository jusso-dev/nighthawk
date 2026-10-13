// Package redact removes secrets from text before it is stored or exported.
package redact

import "regexp"

// Mask replaces a detected secret.
const Mask = "[REDACTED]"

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{20,}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[A-Za-z0-9._\-]+`),
}

// String returns s with every known secret pattern masked.
func String(s string) string {
	for i, p := range patterns {
		if i == len(patterns)-1 {
			s = p.ReplaceAllString(s, "${1}"+Mask)
			continue
		}
		s = p.ReplaceAllString(s, Mask)
	}
	return s
}
