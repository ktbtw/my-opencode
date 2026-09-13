package overlay

import (
	"sync"
	"sync/atomic"
	"time"
)

type Event struct {
	ID         uint64    `json:"id"`
	Type       string    `json:"type"`
	OperatorID int64     `json:"-"`
	Data       any       `json:"data"`
	SentAt     time.Time `json:"sent_at"`
}

type Hub struct {
	mu          sync.RWMutex
	subscribers map[int64]map[uint64]chan Event
	sequence    atomic.Uint64
	subscriber  atomic.Uint64
}

func NewHub() *Hub {
	return &Hub{subscribers: map[int64]map[uint64]chan Event{}}
}

func (h *Hub) Publish(operatorID int64, eventType string, data any) {
	if h == nil || operatorID <= 0 || eventType == "" {
		return
	}
	h.mu.Lock()
	event := Event{
		ID: h.sequence.Add(1), Type: eventType, OperatorID: operatorID,
		Data: data, SentAt: time.Now().UTC(),
	}
	for id, ch := range h.subscribers[operatorID] {
		select {
		case ch <- event:
		default:
			delete(h.subscribers[operatorID], id)
			close(ch)
		}
	}
	h.mu.Unlock()
}

func (h *Hub) Subscribe(operatorID int64) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	id := h.subscriber.Add(1)
	h.mu.Lock()
	if h.subscribers[operatorID] == nil {
		h.subscribers[operatorID] = map[uint64]chan Event{}
	}
	h.subscribers[operatorID][id] = ch
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		if subs := h.subscribers[operatorID]; subs != nil {
			delete(subs, id)
			if len(subs) == 0 {
				delete(h.subscribers, operatorID)
			}
		}
		h.mu.Unlock()
	}
	return ch, cancel
}
