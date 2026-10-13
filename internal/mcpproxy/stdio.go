package mcpproxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
)

// RunStdio proxies newline-delimited JSON-RPC between os.Stdin/os.Stdout and a child process.
// argv is the child argv; the first element is the executable.
// Denied tools/call requests are answered locally and never reach the child.
// RunStdio proxies with no pin filter.
func RunStdio(ctx context.Context, name string, argv []string, d Decider, logf func(LogLine)) error {
	return runStdio(ctx, name, argv, d, logf, nil)
}

// RunStdioPinned is RunStdio plus tools/list pin filtering.
func RunStdioPinned(ctx context.Context, name string, argv []string, d Decider, logf func(LogLine), pins *PinState) error {
	return runStdio(ctx, name, argv, d, logf, pins)
}

func runStdio(ctx context.Context, name string, argv []string, d Decider, logf func(LogLine), pins *PinState) error {
	if len(argv) == 0 {
		return errors.New("mcpproxy: empty argv")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	configureChild(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	var outMu sync.Mutex
	writeClient := func(b []byte) error {
		outMu.Lock()
		defer outMu.Unlock()
		if len(b) == 0 {
			return nil
		}
		if _, err := os.Stdout.Write(b); err != nil {
			return err
		}
		if b[len(b)-1] != '\n' {
			_, err = os.Stdout.Write([]byte{'\n'})
			return err
		}
		return nil
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				killChildGroup(cmd.Process.Pid)
			}
		case <-done:
		}
	}()

	ids := newIDSet()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := rewriteToolsList(append([]byte(nil), sc.Bytes()...), name, ids, pins)
			_ = writeClient(append(line, '\n'))
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			noteToolsList(line, ids)
			fwd, local := gateMessage(ctx, line, name, d, logf)
			for _, resp := range local {
				if err := writeClient(resp); err != nil {
					errCh <- err
					return
				}
			}
			if fwd == nil {
				continue
			}
			if _, err := stdin.Write(append(fwd, '\n')); err != nil {
				errCh <- err
				return
			}
		}
		if err := sc.Err(); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	readErr := <-errCh
	_ = stdin.Close()
	wg.Wait()
	waitErr := cmd.Wait()
	close(done)

	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if waitErr != nil && ctx.Err() == nil {
		return waitErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}
