package mail

import (
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"math/big"
	"net/smtp"
	"os"
	"strconv"
	"sync"
	"time"
)

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	Brand    string
	CodeTTL  time.Duration
	Cooldown time.Duration
}

func DefaultConfig() Config {
	return Config{
		Host:     envStr("MAIL_HOST", "smtp.qq.com"),
		Port:     envInt("MAIL_PORT", 465),
		Username: envStr("MAIL_USERNAME", "2946846090@qq.com"),
		Password: envStr("MAIL_PASSWORD", "ogboqzfmqjcmdcdd"),
		From:     envStr("MAIL_FROM", "2946846090@qq.com"),
		Brand:    envStr("MAIL_BRAND", "ChatCodex"),
		CodeTTL:  time.Duration(envInt("MAIL_CODE_TTL", 180)) * time.Second,
		Cooldown: time.Duration(envInt("MAIL_CODE_COOLDOWN", 60)) * time.Second,
	}
}

type codeEntry struct {
	code    string
	expires time.Time
	sentAt  time.Time
}

type Mailer struct {
	cfg   Config
	mu    sync.RWMutex
	codes map[string]codeEntry // key = email
}

func New(cfg Config) *Mailer {
	m := &Mailer{cfg: cfg, codes: map[string]codeEntry{}}
	go m.gc()
	return m
}

// SendCode 向目标邮箱发送 6 位数字验证码
func (m *Mailer) SendCode(email string) error {
	m.mu.RLock()
	if e, ok := m.codes[email]; ok && time.Since(e.sentAt) < m.cfg.Cooldown {
		m.mu.RUnlock()
		remain := m.cfg.Cooldown - time.Since(e.sentAt)
		return fmt.Errorf("发送频率过快，请 %d 秒后重试", int(remain.Seconds())+1)
	}
	m.mu.RUnlock()

	code, err := randomDigits(6)
	if err != nil {
		return fmt.Errorf("生成验证码失败: %w", err)
	}

	subject := fmt.Sprintf("[%s] 邮箱验证码", m.cfg.Brand)
	body := fmt.Sprintf(
		"您的验证码是：<b style=\"font-size:24px;color:#1a56db\">%s</b><br><br>"+
			"验证码 %d 分钟内有效，请勿泄露给他人。<br><br>"+
			"—— %s",
		code, int(m.cfg.CodeTTL.Minutes()), m.cfg.Brand,
	)

	if err := m.sendMail(email, subject, body); err != nil {
		return fmt.Errorf("发送邮件失败: %w", err)
	}

	m.mu.Lock()
	m.codes[email] = codeEntry{
		code:    code,
		expires: time.Now().Add(m.cfg.CodeTTL),
		sentAt:  time.Now(),
	}
	m.mu.Unlock()
	return nil
}

func (m *Mailer) SendNotification(email, subject, htmlBody string) error {
	if email == "" {
		return fmt.Errorf("邮箱不能为空")
	}
	if subject == "" {
		subject = fmt.Sprintf("[%s] 系统通知", m.cfg.Brand)
	}
	if htmlBody == "" {
		htmlBody = fmt.Sprintf("您有一条新的 %s 通知。", m.cfg.Brand)
	}
	if err := m.sendMail(email, subject, htmlBody); err != nil {
		return fmt.Errorf("发送邮件失败: %w", err)
	}
	return nil
}

// VerifyCode 验证邮箱验证码（验证后立即失效）
func (m *Mailer) VerifyCode(email, code string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.codes[email]
	if !ok || time.Now().After(e.expires) || e.code != code {
		return false
	}
	delete(m.codes, email)
	return true
}

func (m *Mailer) sendMail(to, subject, htmlBody string) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		m.cfg.From, to, subject, htmlBody,
	)

	tlsCfg := &tls.Config{ServerName: m.cfg.Host}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()

	auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(m.cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (m *Mailer) gc() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for k, v := range m.codes {
			if now.After(v.expires) {
				delete(m.codes, k)
			}
		}
		m.mu.Unlock()
	}
}

func randomDigits(n int) (string, error) {
	s := ""
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		s += strconv.Itoa(int(v.Int64()))
	}
	return s, nil
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
