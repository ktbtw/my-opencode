package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"relay-server/internal/model"
)

// Manager issues stateless, signed access tokens. The signing key must be
// stable across process restarts and replicas.
type Manager struct {
	key []byte
	ttl time.Duration
}

type tokenClaims struct {
	Version        int    `json:"v"`
	OperatorID     int64  `json:"operator_id"`
	OperatorUID    string `json:"operator_uid,omitempty"`
	Username       string `json:"username"`
	Name           string `json:"name,omitempty"`
	Email          string `json:"email,omitempty"`
	OperatorKey    string `json:"operator_key,omitempty"`
	MembershipTier string `json:"membership_tier,omitempty"`
	ExpiresAt      int64  `json:"exp"`
}

func NewManager(ttl time.Duration) *Manager {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		buf = []byte("development-only-auth-secret")
	}
	return NewManagerWithSecret(ttl, buf)
}

func NewManagerWithSecret(ttl time.Duration, secret []byte) *Manager {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	key := append([]byte(nil), secret...)
	if len(key) == 0 {
		key = []byte("development-only-auth-secret")
	}
	return &Manager{key: key, ttl: ttl}
}

func (m *Manager) Issue(operator model.Operator) (string, error) {
	claims := tokenClaims{
		Version: 1, OperatorID: operator.ID, OperatorUID: operator.OperatorUID,
		Username: operator.Username, Name: operator.Name, Email: operator.Email,
		OperatorKey: operator.OperatorKey, MembershipTier: operator.MembershipTier,
		ExpiresAt: time.Now().Add(m.ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, m.key)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return "v1." + encoded + "." + signature, nil
}

func (m *Manager) Verify(rawToken string) (model.Operator, bool) {
	parts := strings.Split(strings.TrimSpace(rawToken), ".")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] == "" || parts[2] == "" {
		return model.Operator{}, false
	}
	mac := hmac.New(sha256.New, m.key)
	_, _ = mac.Write([]byte(parts[1]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(got, want) {
		return model.Operator{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return model.Operator{}, false
	}
	var claims tokenClaims
	if json.Unmarshal(payload, &claims) != nil || claims.Version != 1 || claims.OperatorID == 0 || claims.Username == "" {
		return model.Operator{}, false
	}
	if time.Now().Unix() >= claims.ExpiresAt {
		return model.Operator{}, false
	}
	// MembershipTier 保持签发时的原值：等级归一化与权威值由数据库和策略层决定。
	return model.Operator{ID: claims.OperatorID, OperatorUID: claims.OperatorUID, Username: claims.Username, Name: claims.Name, Email: claims.Email, OperatorKey: claims.OperatorKey, MembershipTier: claims.MembershipTier}, true
}

// ExtractBearer accepts the conventional scheme case-insensitively.
func ExtractBearer(header string) string {
	value := strings.TrimSpace(header)
	if len(value) < 8 || !strings.EqualFold(value[:6], "Bearer") || value[6] != ' ' {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
