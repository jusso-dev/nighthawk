package audit

import "encoding/json"

// Canonical marshals v to JSON with map keys sorted, so hashes stay stable.
func Canonical(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var parsed any
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}
	return json.Marshal(parsed)
}
