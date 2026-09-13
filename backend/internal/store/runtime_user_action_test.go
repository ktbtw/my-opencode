package store

import "testing"

func TestRuntimeUserActionNotificationClaimIsIdempotent(t *testing.T) {
	memory := NewMemory(nil)
	claimed, err := memory.ClaimRuntimeUserActionNotification(7, "preflight_1", "ida-uac:preflight_1", "email")
	if err != nil {
		t.Fatalf("claim notification: %v", err)
	}
	if !claimed {
		t.Fatal("expected first notification claim to succeed")
	}
	claimed, err = memory.ClaimRuntimeUserActionNotification(7, "preflight_1", "ida-uac:preflight_1", "email")
	if err != nil {
		t.Fatalf("claim duplicate notification: %v", err)
	}
	if claimed {
		t.Fatal("expected duplicate notification claim to be ignored")
	}
	claimed, err = memory.ClaimRuntimeUserActionNotification(7, "preflight_1", "ida-uac:preflight_1", "push")
	if err != nil || !claimed {
		t.Fatalf("expected a different channel to have its own claim, claimed=%t err=%v", claimed, err)
	}
}
