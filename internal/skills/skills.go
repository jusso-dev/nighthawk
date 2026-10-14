// Package skills identifies and hashes skill files.
package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FileHash returns the SHA-256 hex digest of path.
func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// NameFromPath returns the path segment after /skills/, or basename without extension.
func NameFromPath(path string) string {
	const marker = "/skills/"
	if i := strings.Index(filepath.ToSlash(path), marker); i >= 0 {
		rest := filepath.ToSlash(path)[i+len(marker):]
		if rest == "" {
			return ""
		}
		seg := strings.SplitN(rest, "/", 2)[0]
		return strings.TrimSuffix(seg, filepath.Ext(seg))
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
