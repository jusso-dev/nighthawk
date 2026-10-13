package event

import "testing"

func TestNewID(t *testing.T) {
	a := NewID()
	b := NewID()
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("%q %q", a, b)
	}
	if a == b {
		t.Fatal("not unique")
	}
	for _, c := range a {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')) {
			t.Fatalf("bad char %q", c)
		}
		switch c {
		case 'I', 'L', 'O', 'U':
			t.Fatalf("non-crockford %q", c)
		}
	}
}
