package event

import (
	"crypto/rand"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewID returns a 26-character ULID (Crockford base32, millisecond prefix).
func NewID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		panic(err)
	}
	return encode128(b)
}

func encode128(b [16]byte) string {
	var out [26]byte
	var acc uint64
	bits := 0
	n := 0
	for _, c := range b {
		acc = (acc << 8) | uint64(c)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out[n] = crockford[(acc>>bits)&31]
			n++
		}
	}
	if bits > 0 {
		out[n] = crockford[(acc<<(5-bits))&31]
		n++
	}
	return string(out[:n])
}
