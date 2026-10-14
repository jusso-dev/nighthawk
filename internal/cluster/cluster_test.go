package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestSSHArgs(t *testing.T) {
	want := []string{"ssh", "-o", "BatchMode=yes", "worker1", "nighthawk", "spend", "--by", "task", "--json"}
	if got := SSHArgs("worker1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestQueryWith(t *testing.T) {
	results := QueryWith(context.Background(), []string{"a", "b"}, func(ctx context.Context, host string) ([]byte, error) {
		if host == "b" {
			return nil, errors.New("down")
		}
		return []byte(`{"task":1}`), nil
	})
	if len(results) != 2 {
		t.Fatalf("%d", len(results))
	}
	if results[0].Status != "ok" || string(results[0].Spend) != `{"task":1}` {
		t.Fatalf("%+v", results[0])
	}
	if results[1].Status != "unknown" || results[1].Spend != nil {
		t.Fatalf("%+v", results[1])
	}
	raw, err := json.Marshal(results[1])
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(raw), `"spend":null`) {
		t.Fatalf("marshal=%s", raw)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
