package broker

import (
	"context"
	"time"
)

// LinkLatency is a server-clock measurement on the existing Agent websocket.
// It excludes the phone's HTTP RTT and does not estimate model generation speed.
type LinkLatency struct {
	Status       string    `json:"status"`
	Milliseconds *int64    `json:"rtt_ms,omitempty"`
	MeasuredAt   time.Time `json:"measured_at"`
}

func (b *Broker) AgentLinkLatency(ctx context.Context, operatorID int64, agentID string) (LinkLatency, error) {
	b.mu.RLock()
	dev := b.devices[deviceKey(operatorID, agentID)]
	b.mu.RUnlock()
	if dev == nil || dev.conn == nil {
		return LinkLatency{Status: "offline", MeasuredAt: time.Now().UTC()}, nil
	}
	sample, err := dev.measureLink(ctx, dev.conn.Ping)
	b.mu.RLock()
	current := b.devices[deviceKey(operatorID, agentID)]
	b.mu.RUnlock()
	if current != dev {
		return LinkLatency{Status: "unknown", MeasuredAt: time.Now().UTC()}, nil
	}
	return sample, err
}

func (dev *Device) measureLink(ctx context.Context, ping func(context.Context) error) (LinkLatency, error) {
	dev.linkMu.Lock()
	if !dev.linkSample.MeasuredAt.IsZero() && time.Since(dev.linkSample.MeasuredAt) < 10*time.Second {
		sample := dev.linkSample
		dev.linkMu.Unlock()
		return sample, nil
	}
	pending := dev.linkPending
	if pending == nil {
		pending = make(chan struct{})
		dev.linkPending = pending
		go func() {
			// Share one bounded probe across devices on the same account, even when
			// one HTTP caller disconnects before the websocket replies.
			probeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			start := time.Now()
			err := ping(probeCtx)
			sample := LinkLatency{Status: "timeout", MeasuredAt: time.Now().UTC()}
			if err == nil {
				ms := time.Since(start).Milliseconds()
				sample.Status, sample.Milliseconds = "connected", &ms
			}
			dev.linkMu.Lock()
			dev.linkSample = sample
			dev.linkPending = nil
			close(pending)
			dev.linkMu.Unlock()
		}()
	}
	dev.linkMu.Unlock()
	select {
	case <-ctx.Done():
		return LinkLatency{}, ctx.Err()
	case <-pending:
		dev.linkMu.Lock()
		sample := dev.linkSample
		dev.linkMu.Unlock()
		return sample, nil
	}
}
