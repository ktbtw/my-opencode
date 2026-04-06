package auth

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"relay-server/internal/model"
)

type session struct {
	operator model.Operator
	expires  time.Time
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]session
	ttl      time.Duration
}

func NewManager(ttl time.Duration) *Manager {
	return &Manager{
		sessions: map[string]session{},
		ttl:      ttl,
	}
}

func (m *Manager) Issue(operator model.Operator) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[token] = session{
		operator: operator,
		expires:  time.Now().Add(m.ttl),
	}
	return token, nil
}

func (m *Manager) Verify(rawToken string) (model.Operator, bool) {
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return model.Operator{}, false
	}

	m.mu.RLock()
	item, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok {
		return model.Operator{}, false
	}
	if time.Now().After(item.expires) {
		m.mu.Lock()
		delete(m.sessions, token)
		m.mu.Unlock()
		return model.Operator{}, false
	}
	// 自动续期：每次验证成功时刷新过期时间
	m.mu.Lock()
	item.expires = time.Now().Add(m.ttl)
	m.sessions[token] = item
	m.mu.Unlock()
	return item.operator, true
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
