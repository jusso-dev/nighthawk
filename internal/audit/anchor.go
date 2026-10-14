package audit

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// SignAnchor signs seq and hash with an ed25519 private key.
func SignAnchor(priv ed25519.PrivateKey, seq uint64, hash string) string {
	msg := anchorMsg(seq, hash)
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg))
}

// CheckAnchor reports whether sig matches seq and hash.
func CheckAnchor(pub ed25519.PublicKey, seq uint64, hash, sig string) bool {
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, anchorMsg(seq, hash), raw)
}

func anchorMsg(seq uint64, hash string) []byte {
	return []byte(strconv.FormatUint(seq, 10) + "\n" + hash)
}

// ParsePrivateKey accepts a raw or base64 ed25519 seed (32) or private key (64).
func ParsePrivateKey(raw []byte) (ed25519.PrivateKey, error) {
	text := strings.TrimSpace(string(raw))
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		decoded = raw
	}
	switch len(decoded) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(decoded), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(decoded), nil
	default:
		return nil, fmt.Errorf("key")
	}
}

// AnchorMismatch is returned when a stored anchor does not match the chain.
type AnchorMismatch struct {
	Seq uint64
}

func (a AnchorMismatch) Error() string {
	return fmt.Sprintf("anchor mismatch at seq %d", a.Seq)
}
