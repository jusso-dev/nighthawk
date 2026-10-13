package enforce

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/herdr"
)

type fakeH struct {
	keys    []string
	closed  bool
	notes   int
	info    herdr.ProcessInfo
	infoErr error
}

func (f *fakeH) SendKeys(_ context.Context, _, keys string) error {
	f.keys = append(f.keys, keys)
	return nil
}
func (f *fakeH) ClosePane(context.Context, string) error { f.closed = true; return nil }
func (f *fakeH) ProcessInfo(context.Context, string) (herdr.ProcessInfo, error) {
	return f.info, f.infoErr
}
func (f *fakeH) Notify(context.Context, string, string) error { f.notes++; return nil }

func TestKillStaysInTree(t *testing.T) {
	f := &fakeH{info: herdr.ProcessInfo{ShellPID: 10, PIDs: []int{11, 99}}}
	var signaled []int
	var audits int
	a := &Actions{
		H:      f,
		Wait:   -1,
		Signal: func(pid int, sig syscall.Signal) error { signaled = append(signaled, pid); return nil },
		Audit:  func([]byte) { audits++ },
		Descended: func(pid, ancestor int) bool {
			return ancestor == 10 && pid == 11
		},
	}
	if err := a.Kill(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if len(signaled) != 1 || signaled[0] != 11 || !f.closed || audits != 1 {
		t.Fatalf("sig %v closed %v audits %d", signaled, f.closed, audits)
	}
}

func TestPauseAudits(t *testing.T) {
	f := &fakeH{}
	audits := 0
	a := &Actions{H: f, Audit: func([]byte) { audits++ }}
	if err := a.Pause(context.Background(), "p", "claude"); err != nil {
		t.Fatal(err)
	}
	if len(f.keys) != 1 || f.keys[0] != "Escape" || f.notes != 1 || audits != 1 {
		t.Fatalf("%+v", f)
	}
}

func TestKillEscalates(t *testing.T) {
	f := &fakeH{info: herdr.ProcessInfo{ShellPID: 10, PIDs: []int{11}}}
	var sigs []syscall.Signal
	a := &Actions{
		H:    f,
		Wait: time.Millisecond,
		Signal: func(pid int, sig syscall.Signal) error {
			if pid == 11 {
				sigs = append(sigs, sig)
			}
			return nil
		},
		Audit:     func([]byte) {},
		Descended: func(pid, ancestor int) bool { return pid == 11 && ancestor == 10 },
	}
	if err := a.Kill(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if len(sigs) != 2 || sigs[0] != syscall.SIGTERM || sigs[1] != syscall.SIGKILL {
		t.Fatalf("%v", sigs)
	}
}

func TestKillFailureNotifies(t *testing.T) {
	f := &fakeH{infoErr: context.Canceled}
	a := &Actions{H: f, Audit: func([]byte) {}}
	if err := a.Kill(context.Background(), "p"); err == nil || f.notes != 1 {
		t.Fatalf("%v notes %d", err, f.notes)
	}
}
