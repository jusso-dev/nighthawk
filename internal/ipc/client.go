package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
)

// Call sends one request and reads one response. It returns when ctx ends.
func Call(ctx context.Context, path string, req Request) (Response, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	b, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	b = append(b, '\n')
	if _, err := conn.Write(b); err != nil {
		return Response{}, err
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return Response{}, err
		}
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, fmt.Errorf("ipc: empty response")
	}
	var resp Response
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
