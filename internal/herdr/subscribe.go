package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"time"
)

// Subscribe streams events.subscribe. On disconnect it calls resync and redials
// with backoff. History is not durable, so resync must be agent list.
func Subscribe(ctx context.Context, socketPath string, subs []Subscription, resync func() error) (<-chan Envelope, error) {
	out := make(chan Envelope, 32)
	go func() {
		defer close(out)
		delay := 50 * time.Millisecond
		for {
			if ctx.Err() != nil {
				return
			}
			err := streamOnce(ctx, socketPath, subs, out)
			if ctx.Err() != nil {
				return
			}
			if resync != nil {
				_ = resync()
			}
			if err == nil {
				return
			}
			if !errors.Is(err, context.Canceled) {
				log.Printf("nighthawk: herdr subscribe: %v; retry in %s", err, delay)
			}
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			if delay < 2*time.Second {
				delay *= 2
			}
		}
	}()
	return out, nil
}

func streamOnce(ctx context.Context, socketPath string, subs []Subscription, out chan<- Envelope) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Unblock the read loop on cancel; otherwise a quiet stream never returns.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	req := map[string]any{
		"id":     "nh-sub",
		"method": "events.subscribe",
		"params": map[string]any{"subscriptions": subs},
	}
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := conn.Write(b); err != nil {
		return err
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 2<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var probe struct {
			Result json.RawMessage `json:"result"`
			Error  *ErrorBody      `json:"error"`
			Event  string          `json:"event"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		if probe.Error != nil {
			return fmt.Errorf("herdr events.subscribe: %w", probe.Error)
		}
		if probe.Event == "" {
			continue
		}
		var env Envelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}
		env.Data = append([]byte(nil), env.Data...)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- env:
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return context.Canceled
}
