// Package integrity tracks SHA-256 digests of watched paths.
package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

const fileName = "integrity.json"

// Write records sha256 digests for paths into stateDir/integrity.json (mode 0600).
func Write(stateDir string, paths []string) error {
	m := make(map[string]string, len(paths))
	for _, p := range paths {
		sum, err := hashFile(p)
		if err != nil {
			return err
		}
		m[p] = sum
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, fileName), b, 0o600)
}

// Check returns paths whose current hash differs from the recorded digest.
func Check(stateDir string) (changed []string, err error) {
	b, err := os.ReadFile(filepath.Join(stateDir, fileName))
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for p, want := range m {
		got, err := hashFile(p)
		if err != nil || got != want {
			changed = append(changed, p)
		}
	}
	return changed, nil
}

func hashFile(path string) (string, error) {
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
