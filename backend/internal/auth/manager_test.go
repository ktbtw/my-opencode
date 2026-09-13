package auth

import (
	"strings"
	"testing"
	"time"

	"relay-server/internal/model"
)

func TestSignedTokenSurvivesManagerRestart(t *testing.T) {
	secret := []byte("test-secret-that-is-long-enough")
	operator := model.Operator{ID: 42, OperatorUID: "op_42", Username: "alice", Name: "Alice", OperatorKey: "key"}
	first := NewManagerWithSecret(time.Hour, secret)
	token, err := first.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	second := NewManagerWithSecret(time.Hour, secret)
	got, ok := second.Verify(token)
	if !ok || got != operator {
		t.Fatalf("token issued by prior manager did not verify: ok=%v got=%+v", ok, got)
	}
	if _, ok := NewManagerWithSecret(time.Hour, []byte("different-secret")).Verify(token); ok {
		t.Fatal("token verified with a different secret")
	}
	parts := strings.Split(token, ".")
	parts[1] += "x"
	if _, ok := second.Verify(strings.Join(parts, ".")); ok {
		t.Fatal("tampered token verified")
	}
}

func TestSignedTokenExpires(t *testing.T) {
	manager := NewManagerWithSecret(time.Second, []byte("test-secret"))
	token, err := manager.Issue(model.Operator{ID: 1, Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, ok := manager.Verify(token); ok {
		t.Fatal("expired token verified")
	}
}

func TestExtractBearer(t *testing.T) {
	for _, tc := range []struct {
		header, want string
	}{
		{"Bearer abc", "abc"},
		{" bearer   abc ", "abc"},
		{"Basic abc", ""},
		{"Bearer", ""},
		{"Bearerabc", ""},
	} {
		if got := ExtractBearer(tc.header); got != tc.want {
			t.Errorf("ExtractBearer(%q)=%q, want %q", tc.header, got, tc.want)
		}
	}
}
