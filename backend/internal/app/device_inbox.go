package app

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Keep reading control frames while ordered business handling waits on storage.
// Backpressure bounds both message count and queued bytes; an individual frame
// is separately bounded by SetReadLimit. Never drop accepted task events.
func consumeDeviceMessages(ctx context.Context, conn *websocket.Conn, handle func([]byte) error, observe func(time.Duration, time.Duration, int)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type message struct {
		data     []byte
		received time.Time
	}
	inbox := make(chan message, 128)
	done := make(chan error, 1)
	wake := make(chan struct{}, 1)
	var queued atomic.Int64
	go func() {
		defer close(inbox)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				done <- err
				return
			}
			received := time.Now()
			for queued.Load() > 0 && queued.Load()+int64(len(data)) > 8<<20 {
				select {
				case <-wake:
				case <-ctx.Done():
					done <- ctx.Err()
					return
				}
			}
			queued.Add(int64(len(data)))
			select {
			case inbox <- message{data, received}:
			case <-ctx.Done():
				queued.Add(-int64(len(data)))
				done <- ctx.Err()
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-inbox:
			if !ok {
				return <-done
			}
			queued.Add(-int64(len(msg.data)))
			select {
			case wake <- struct{}{}:
			default:
			}
			start := time.Now()
			err := handle(msg.data)
			if observe != nil {
				observe(start.Sub(msg.received), time.Since(start), len(inbox))
			}
			if err != nil {
				return err
			}
		}
	}
}

func logDeviceHandling(agentID string, wait, processing time.Duration, pending int) {
	if wait < 250*time.Millisecond && processing < 250*time.Millisecond {
		return
	}
	log.Printf("[ws.device] slow handling agent_id=%s queue_ms=%d handler_ms=%d pending=%d", agentID, wait.Milliseconds(), processing.Milliseconds(), pending)
}
