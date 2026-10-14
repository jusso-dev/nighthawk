package judge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jusso-dev/nighthawk/internal/policy"
)

func TestEnabledNoDial(t *testing.T) {
	c := &Client{}
	if c.Enabled() {
		t.Fatal("empty endpoint should disable")
	}
	v, err := c.Judge(context.Background(), "x")
	if err != nil || v.Verdict != "allow" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestJudgeInjectionMalicious(t *testing.T) {
	sample, err := os.ReadFile(filepath.Join("testdata", "injections", "sample.txt"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(b, &req); err != nil {
			t.Error(err)
		}
		found := false
		for _, m := range req.Messages {
			if m.Role == "user" && m.Content == string(sample) {
				found = true
			}
		}
		if !found {
			t.Error("sample not sent as user content")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{
					"content": `{"verdict":"malicious","reason":"injection attempt"}`,
				}},
			},
		})
	}))
	defer srv.Close()
	c := &Client{Endpoint: srv.URL, Model: "test", HTTP: srv.Client()}
	v, err := c.Judge(context.Background(), string(sample))
	if err != nil {
		t.Fatal(err)
	}
	if v.Verdict != "malicious" {
		t.Fatalf("%+v", v)
	}
}

func TestTighten(t *testing.T) {
	allow := policy.Decision{Action: policy.Allow, RuleID: "nh.default"}
	d := Tighten(allow, Verdict{Verdict: "suspicious", Reason: "hmm"})
	if d.Action != policy.AskHuman || d.RuleID != "nh.judge" {
		t.Fatalf("%+v", d)
	}
	d = Tighten(allow, Verdict{Verdict: "malicious", Reason: "bad"})
	if d.Action != policy.Block || d.RuleID != "nh.judge" {
		t.Fatalf("%+v", d)
	}
	block := policy.Decision{Action: policy.Block, RuleID: "nh.x"}
	if got := Tighten(block, Verdict{Verdict: "allow"}); got.Action != policy.Block {
		t.Fatalf("%+v", got)
	}
}
