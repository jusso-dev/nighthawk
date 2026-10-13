package audit

import (
	"database/sql"
	"fmt"
	"time"
)

// SQLStore appends hash-chain rows to the audit table.
type SQLStore struct {
	db *sql.DB
}

// NewSQLStore wraps a database that already has the audit table.
func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }

// Append inserts payload on tx, linking it to the current head.
func (s *SQLStore) Append(tx *sql.Tx, payload []byte) (Record, error) {
	prev := Genesis
	var seq uint64
	var last uint64
	var lastHash string
	err := tx.QueryRow(`SELECT seq, hash FROM audit ORDER BY seq DESC LIMIT 1`).Scan(&last, &lastHash)
	switch {
	case err == sql.ErrNoRows:
		seq = 1
	case err != nil:
		return Record{}, err
	default:
		seq = last + 1
		prev = lastHash
	}
	body := append([]byte(nil), payload...)
	r := Record{Seq: seq, Payload: body, PrevHash: prev}
	r.Hash = HashOf(seq, prev, body)
	_, err = tx.Exec(`INSERT INTO audit(seq, ts, payload, prev_hash, hash) VALUES(?,?,?,?,?)`,
		r.Seq, time.Now().UnixMilli(), r.Payload, r.PrevHash, r.Hash)
	return r, err
}

// Head returns the latest record.
func (s *SQLStore) Head(q rowQuery) (Record, error) {
	var r Record
	err := q.QueryRow(`SELECT seq, payload, prev_hash, hash FROM audit ORDER BY seq DESC LIMIT 1`).Scan(&r.Seq, &r.Payload, &r.PrevHash, &r.Hash)
	return r, err
}

// HashAt returns the stored hash for seq.
func (s *SQLStore) HashAt(q rowQuery, seq uint64) (string, error) {
	var h string
	err := q.QueryRow(`SELECT hash FROM audit WHERE seq = ?`, seq).Scan(&h)
	return h, err
}

type rowQuery interface {
	QueryRow(query string, args ...any) *sql.Row
}

// VerifyDB walks the table. from is the first seq to check (1 means the head of the chain).
func (s *SQLStore) VerifyDB(from uint64) (uint64, error) {
	if from == 0 {
		from = 1
	}
	prev := Genesis
	if from > 1 {
		if err := s.db.QueryRow(`SELECT hash FROM audit WHERE seq = ?`, from-1).Scan(&prev); err != nil {
			return from, fmt.Errorf("%w at seq %d", ErrBroken, from)
		}
	}
	rows, err := s.db.Query(`SELECT seq, payload, prev_hash, hash FROM audit WHERE seq >= ? ORDER BY seq`, from)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	return verifyFrom(prev, func(yield func(Record) bool) {
		for rows.Next() {
			var r Record
			if err := rows.Scan(&r.Seq, &r.Payload, &r.PrevHash, &r.Hash); err != nil {
				return
			}
			if !yield(r) {
				return
			}
		}
	})
}

// VerifyStream checks a sequence that starts at genesis.
// rows calls yield for each record and stops when yield returns false.
func VerifyStream(rows func(yield func(Record) bool)) (uint64, error) {
	return verifyFrom(Genesis, rows)
}

func verifyFrom(prev string, rows func(yield func(Record) bool)) (uint64, error) {
	var broken uint64
	var verr error
	rows(func(r Record) bool {
		if r.PrevHash != prev || HashOf(r.Seq, r.PrevHash, r.Payload) != r.Hash {
			broken = r.Seq
			verr = fmt.Errorf("%w at seq %d", ErrBroken, r.Seq)
			return false
		}
		prev = r.Hash
		return true
	})
	return broken, verr
}
