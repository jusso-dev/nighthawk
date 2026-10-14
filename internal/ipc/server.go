package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// Serve listens on a 0600 unix socket. A stale socket is removed only when
// dialing it fails. Serve returns nil after ctx is cancelled.
func Serve(ctx context.Context, path string, h Handler) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := prepareSocket(path); err != nil {
		return err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return err
	}
	go func() {
		<-ctx.Done()
		ln.Close()
		os.Remove(path)
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				os.Remove(path)
				return nil
			}
			continue
		}
		go serveConn(ctx, conn, h)
	}
}

func prepareSocket(path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	c, err := net.Dial("unix", path)
	if err != nil {
		return os.Remove(path)
	}
	c.Close()
	return fmt.Errorf("socket %s is already in use", path)
}

func serveConn(ctx context.Context, conn net.Conn, h Handler) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	if !sc.Scan() {
		return
	}
	var req Request
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
		writeResp(conn, Response{OK: false, Error: err.Error()})
		return
	}
	cctx := ctx
	if req.Wait > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, req.Wait)
		defer cancel()
	}
	if dl, ok := cctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	writeResp(conn, h.Handle(cctx, req))
}

func writeResp(conn net.Conn, resp Response) {
	b, err := json.Marshal(resp)
	if err != nil {
		b = []byte(`{"ok":false,"error":"encode"}`)
	}
	b = append(b, '\n')
	_, _ = conn.Write(b)
}

// ErrClosed is returned when the listener is shutting down.
var ErrClosed = errors.New("ipc closed")
