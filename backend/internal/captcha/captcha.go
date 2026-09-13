package captcha

import (
	"sync"
	"time"

	"github.com/mojocn/base64Captcha"
)

type store struct {
	mu    sync.RWMutex
	items map[string]entry
}

type entry struct {
	value   string
	expires time.Time
}

func newStore(ttl time.Duration) *store {
	s := &store{items: map[string]entry{}}
	go s.gc(ttl)
	return s
}

func (s *store) Set(id, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = entry{value: value, expires: time.Now().Add(5 * time.Minute)}
	return nil
}

func (s *store) Get(id string, clear bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[id]
	if !ok || time.Now().After(e.expires) {
		delete(s.items, id)
		return ""
	}
	if clear {
		delete(s.items, id)
	}
	return e.value
}

func (s *store) Verify(id, answer string, clear bool) bool {
	v := s.Get(id, clear)
	if v == "" {
		return false
	}
	return v == answer
}

func (s *store) gc(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for k, v := range s.items {
			if now.After(v.expires) {
				delete(s.items, k)
			}
		}
		s.mu.Unlock()
	}
}

var captchaStore = newStore(time.Minute)

// Generate 生成图形验证码，返回 captcha_id 和 base64 图片
func Generate() (id string, b64 string, err error) {
	driver := base64Captcha.NewDriverDigit(80, 240, 5, 0.7, 80)
	c := base64Captcha.NewCaptcha(driver, captchaStore)
	id, b64, _, err = c.Generate()
	return
}

// Verify 校验验证码（校验后立即失效）
func Verify(id, answer string) bool {
	return captchaStore.Verify(id, answer, true)
}
