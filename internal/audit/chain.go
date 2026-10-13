// Package audit implements the tamper-evident hash chain behind the audit log.
// Storage in SQLite is added by the M0 storage issue; this package only owns
// the chaining and verification rules so they can be tested in isolation.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// Genesis is the previous-hash value of the first record.
const Genesis = "0000000000000000000000000000000000000000000000000000000000000000"

// Record is one entry in the chain.
type Record struct {
	Seq      uint64
	Payload  []byte
	PrevHash string
	Hash     string
}

// ErrBroken is returned when verification finds a mismatch.
var ErrBroken = errors.New("audit chain broken")

// HashOf computes the hash of a record from its sequence, previous hash and payload.
func HashOf(seq uint64, prev string, payload []byte) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d\n%s\n", seq, prev)
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// Chain appends records in memory.
type Chain struct {
	records []Record
}

// Append adds a payload and returns the new record.
func (c *Chain) Append(payload []byte) Record {
	prev := Genesis
	if n := len(c.records); n > 0 {
		prev = c.records[n-1].Hash
	}
	seq := uint64(len(c.records) + 1)
	r := Record{Seq: seq, Payload: append([]byte(nil), payload...), PrevHash: prev}
	r.Hash = HashOf(seq, prev, r.Payload)
	c.records = append(c.records, r)
	return r
}

// Records returns a copy of the chain.
func (c *Chain) Records() []Record { return append([]Record(nil), c.records...) }

// Verify checks every link and returns the first broken sequence number.
func Verify(rs []Record) (uint64, error) {
	prev := Genesis
	for i, r := range rs {
		if r.Seq != uint64(i+1) || r.PrevHash != prev || HashOf(r.Seq, r.PrevHash, r.Payload) != r.Hash {
			return r.Seq, ErrBroken
		}
		prev = r.Hash
	}
	return 0, nil
}
