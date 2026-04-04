package broker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"

	"relay-server/internal/model"
)

var ErrOffline = errors.New("device offline")

type Device struct {
	ID       string
	Hostname string
	Version  string
	Projects []model.HelloProject
	SeenAt   time.Time
	conn     *websocket.Conn
	mu       sync.Mutex
}

type Broker struct {
	mu      sync.RWMutex
	devices map[string]*Device
}

func New() *Broker {
	return &Broker{
		devices: map[string]*Device{},
	}
}

func (b *Broker) Add(conn *websocket.Conn, hello model.HelloPayload) *Device {
	b.mu.Lock()
	defer b.mu.Unlock()
	dev := &Device{
		ID:       hello.DeviceID,
		Hostname: hello.Hostname,
		Version:  hello.Version,
		Projects: hello.Projects,
		SeenAt:   time.Now().UTC(),
		conn:     conn,
	}
	b.devices[hello.DeviceID] = dev
	return dev
}

func (b *Broker) Touch(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if dev, ok := b.devices[id]; ok {
		dev.SeenAt = time.Now().UTC()
	}
}

func (b *Broker) Remove(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.devices, id)
}

func (b *Broker) Has(id string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.devices[id]
	return ok
}

func (b *Broker) Dispatch(ctx context.Context, deviceID string, env model.Envelope) error {
	b.mu.RLock()
	dev, ok := b.devices[deviceID]
	b.mu.RUnlock()
	if !ok {
		return ErrOffline
	}

	dev.mu.Lock()
	defer dev.mu.Unlock()
	buf, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return dev.conn.Write(ctx, websocket.MessageText, buf)
}
