package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jusso-dev/nighthawk/internal/audit"
	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/store"
)

func (s *Server) anchorLoop(ctx context.Context) {
	if s.opt.Config.Anchor.KeyPath == "" {
		return
	}
	iv := s.opt.Config.Anchor.Interval.D()
	if iv <= 0 {
		iv = time.Hour
	}
	s.anchorOnce()
	t := time.NewTicker(iv)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.anchorOnce()
		}
	}
}

func (s *Server) anchorOnce() {
	keyPath := s.opt.Config.Anchor.KeyPath
	if keyPath == "" || !config.OutsideDir(s.opt.StateDir, keyPath) {
		return
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return
	}
	priv, err := audit.ParsePrivateKey(raw)
	if err != nil {
		return
	}
	rec, err := s.au.Head(s.st.DB())
	if err != nil {
		return
	}
	sig := audit.SignAnchor(priv, rec.Seq, rec.Hash)
	target := "local"
	if s.opt.Config.Anchor.File != "" {
		target = "file"
	} else if s.opt.Config.Anchor.WebhookURL != "" {
		target = "webhook"
	}
	if err := s.st.PutAnchor(context.Background(), store.Anchor{
		Seq: rec.Seq, Hash: rec.Hash, Target: target, TS: time.Now().UTC(), Sig: sig,
	}); err != nil {
		return
	}
	if f := s.opt.Config.Anchor.File; f != "" && config.OutsideDir(s.opt.StateDir, f) {
		_ = os.WriteFile(f, []byte(fmt.Sprintf("%d %s %s\n", rec.Seq, rec.Hash, sig)), 0o600)
	}
	if u := s.opt.Config.Anchor.WebhookURL; u != "" {
		s.postAnchor(u, rec.Seq, rec.Hash, sig)
	}
}

func (s *Server) postAnchor(url string, seq uint64, hash, sig string) {
	body, err := json.Marshal(map[string]any{"seq": seq, "hash": hash, "sig": sig})
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
