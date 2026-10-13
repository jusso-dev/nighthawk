package audit

import "testing"

func TestChainVerifies(t *testing.T) {
	var c Chain
	for _, p := range []string{"a", "b", "c"} {
		c.Append([]byte(p))
	}
	if _, err := Verify(c.Records()); err != nil {
		t.Fatal(err)
	}
}

func TestTamperIsDetected(t *testing.T) {
	var c Chain
	c.Append([]byte("a"))
	c.Append([]byte("b"))
	rs := c.Records()
	rs[0].Payload = []byte("x")
	if seq, err := Verify(rs); err == nil || seq != 1 {
		t.Fatalf("want break at 1, got %d %v", seq, err)
	}
}
