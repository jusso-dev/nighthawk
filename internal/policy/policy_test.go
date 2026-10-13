package policy

import (
	"errors"
	"testing"
)

func TestFailClosed(t *testing.T) {
	if d := OnError(Unattended, errors.New("x")); d.Action != Block {
		t.Fatalf("unattended: %v", d.Action)
	}
	if d := OnError(Attended, errors.New("x")); d.Action != AskHuman {
		t.Fatalf("attended: %v", d.Action)
	}
}
