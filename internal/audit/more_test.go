package audit

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/store"
)

func TestCanonicalStable(t *testing.T) {
	a, err := Canonical(map[string]any{"b": 1, "a": map[string]any{"d": 2, "c": 3}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonical(struct {
		B int            `json:"b"`
		A map[string]int `json:"a"`
	}{B: 1, A: map[string]int{"d": 2, "c": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("%s vs %s", a, b)
	}
}

func TestSQLAppendAndTriggers(t *testing.T) {
	s := openDB(t)
	as := NewSQLStore(s.DB())
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	r1, err := as.Append(tx, []byte(`{"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.Append(tx, []byte(`{"n":2}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := as.Append(tx, []byte(`{"n":3}`)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE audit SET payload = ? WHERE seq = 1`, []byte(`{"n":9}`)); err == nil {
		t.Fatal("update should fail")
	}
	if _, err := s.DB().Exec(`DELETE FROM audit WHERE seq = 2`); err == nil {
		t.Fatal("delete should fail")
	}
	if _, err := as.VerifyDB(1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`DROP TRIGGER audit_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`DROP TRIGGER audit_no_delete`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE audit SET payload = ? WHERE seq = ?`, []byte(`{"n":9}`), r1.Seq); err != nil {
		t.Fatal(err)
	}
	seq, err := as.VerifyDB(1)
	if err == nil || seq != 1 {
		t.Fatalf("seq %d err %v", seq, err)
	}

	s2 := openDB(t)
	as2 := NewSQLStore(s2.DB())
	tx, err = s2.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := as2.Append(tx, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	tx.Commit()
	if _, err := s2.DB().Exec(`DROP TRIGGER audit_no_delete`); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.DB().Exec(`DELETE FROM audit WHERE seq = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := as2.VerifyDB(1); err == nil {
		t.Fatal("delete should break the chain")
	}
}

func TestMillionVerify(t *testing.T) {
	n := 1_000_000
	prev := Genesis
	start := time.Now()
	seq, err := VerifyStream(func(yield func(Record) bool) {
		for i := 1; i <= n; i++ {
			payload := []byte("x")
			s := uint64(i)
			h := HashOf(s, prev, payload)
			r := Record{Seq: s, Payload: payload, PrevHash: prev, Hash: h}
			prev = h
			if !yield(r) {
				return
			}
		}
	})
	if err != nil || seq != 0 {
		t.Fatal(seq, err)
	}
	if time.Since(start) > 30*time.Second {
		t.Fatalf("verify took %s", time.Since(start))
	}
}

func TestAnchorDetectsRewrite(t *testing.T) {
	s := openDB(t)
	as := NewSQLStore(s.DB())
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.Append(tx, []byte("a")); err != nil {
		t.Fatal(err)
	}
	r, err := as.Append(tx, []byte("b"))
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig := SignAnchor(priv, r.Seq, r.Hash)
	if !CheckAnchor(pub, r.Seq, r.Hash, sig) {
		t.Fatal("sig")
	}
	if _, err := s.DB().Exec(`DROP TRIGGER audit_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE audit SET payload = ? WHERE seq = 1`, []byte("z")); err != nil {
		t.Fatal(err)
	}
	// Recompute a forged chain so a full rewrite would pass Verify but not the anchor.
	rows, err := s.DB().Query(`SELECT seq, payload FROM audit ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		seq     uint64
		payload []byte
	}
	var rs []row
	for rows.Next() {
		var rr row
		if err := rows.Scan(&rr.seq, &rr.payload); err != nil {
			t.Fatal(err)
		}
		rs = append(rs, rr)
	}
	rows.Close()
	prev := Genesis
	for _, rr := range rs {
		h := HashOf(rr.seq, prev, rr.payload)
		if _, err := s.DB().Exec(`UPDATE audit SET prev_hash = ?, hash = ? WHERE seq = ?`, prev, h, rr.seq); err != nil {
			t.Fatal(err)
		}
		prev = h
	}
	if _, err := as.VerifyDB(1); err != nil {
		t.Fatal("rewritten chain should self-verify", err)
	}
	head, err := as.Head(s.DB())
	if err != nil {
		t.Fatal(err)
	}
	if CheckAnchor(pub, r.Seq, head.Hash, sig) && head.Hash == r.Hash {
		t.Fatal("anchor still matches rewritten chain")
	}
	if head.Hash == r.Hash {
		t.Fatal("hash unchanged")
	}
	stored, err := as.HashAt(s.DB(), r.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if stored == r.Hash || CheckAnchor(pub, r.Seq, stored, sig) {
		t.Fatal("verify --anchors should fail")
	}
}

func openDB(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// silence unused import if sql helpers change
var _ = sql.ErrNoRows
