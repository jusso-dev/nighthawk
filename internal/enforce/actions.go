package enforce

import (
	"context"
	"syscall"
	"time"

	"github.com/jusso-dev/nighthawk/internal/herdr"
)

// HerdrAPI is the pane control surface.
type HerdrAPI interface {
	SendKeys(ctx context.Context, id, keys string) error
	ClosePane(ctx context.Context, id string) error
	ProcessInfo(ctx context.Context, id string) (herdr.ProcessInfo, error)
	Notify(ctx context.Context, title, body string) error
}

// Actions pauses and kills panes.
type Actions struct {
	H         HerdrAPI
	Signal    func(pid int, sig syscall.Signal) error
	Audit     func(payload []byte)
	Descended func(pid, ancestor int) bool
	// Wait is the SIGTERM to SIGKILL gap. Zero means 3s. Negative skips SIGKILL.
	Wait time.Duration
}

// Pause sends an interrupt and posts a notification.
func (a *Actions) Pause(ctx context.Context, paneID, agent string) error {
	var err error
	for _, k := range KeysFor(agent) {
		if e := a.H.SendKeys(ctx, paneID, k); e != nil {
			err = e
		}
	}
	_ = a.H.Notify(ctx, "Nighthawk", "paused pane "+paneID)
	a.audit("pause", paneID, err)
	return err
}

// Kill signals only descendants of the pane shell, then closes the pane.
func (a *Actions) Kill(ctx context.Context, paneID string) error {
	info, err := a.H.ProcessInfo(ctx, paneID)
	if err != nil {
		_ = a.H.Notify(ctx, "Nighthawk", "kill failed for pane "+paneID)
		a.audit("kill", paneID, err)
		return err
	}
	desc := a.Descended
	if desc == nil {
		desc = Descended
	}
	sig := a.Signal
	if sig == nil {
		sig = syscall.Kill
	}
	var sigErr error
	var killed []int
	for _, proc := range info.Processes {
		pid := proc.PID
		if !desc(pid, info.ShellPID) {
			continue
		}
		if e := sig(pid, syscall.SIGTERM); e != nil {
			sigErr = e
		}
		killed = append(killed, pid)
	}
	if w := a.termWait(); w > 0 && len(killed) > 0 {
		t := time.NewTimer(w)
		select {
		case <-ctx.Done():
			t.Stop()
		case <-t.C:
			for _, pid := range killed {
				// Re-check: the pid may have exited and been reused.
				if !desc(pid, info.ShellPID) {
					continue
				}
				if e := sig(pid, syscall.SIGKILL); e != nil && sigErr == nil {
					sigErr = e
				}
			}
		}
	}
	if sigErr != nil {
		_ = a.H.Notify(ctx, "Nighthawk", "kill failed for pane "+paneID)
	}
	if e := a.H.ClosePane(ctx, paneID); e != nil && sigErr == nil {
		sigErr = e
	}
	a.audit("kill", paneID, sigErr)
	return sigErr
}

func (a *Actions) termWait() time.Duration {
	if a.Wait < 0 {
		return 0
	}
	if a.Wait == 0 {
		return 3 * time.Second
	}
	return a.Wait
}

func (a *Actions) audit(action, pane string, err error) {
	if a.Audit == nil {
		return
	}
	msg := action + " " + pane
	if err != nil {
		msg += " error"
	}
	a.Audit([]byte(msg))
}
