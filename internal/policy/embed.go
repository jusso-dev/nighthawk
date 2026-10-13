package policy

import _ "embed"

//go:embed default.yaml
var defaultYAML []byte

// DefaultEngine is the embedded unattended ruleset.
func DefaultEngine() (*Engine, error) {
	return LoadYAML("policies/default.yaml", defaultYAML)
}
