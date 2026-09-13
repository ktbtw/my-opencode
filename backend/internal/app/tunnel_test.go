package app

import (
	"crypto/sha256"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"relay-server/internal/model"
)

func TestTunnelBootstrapAuthorizedKeyBindingIsStableAndTargeted(t *testing.T) {
	m := newTunnelManager()
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	credentials, err := m.createWithBootstrap(7, "machine-a", "launcher:machine-a", "demo")
	if err != nil {
		t.Fatal(err)
	}
	defer m.cancelSession(credentials.TunnelID, "test cleanup")
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDQLa1lVd+AE1vXkONHkSzznDI3AOd87KEbIpNKSNsbl"
	first, err := m.bindBootstrapAuthorizedKey(credentials.BootstrapToken, key)
	if err != nil {
		t.Fatal(err)
	}
	if first.TunnelID != credentials.TunnelID || first.OperatorID != 7 || first.MachineID != "machine-a" || first.LauncherID != "launcher:machine-a" {
		t.Fatalf("binding targeted the wrong launcher: %+v", first)
	}
	if !first.ExpiresAt.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("unexpected authorization expiry: %s", first.ExpiresAt)
	}
	now = now.Add(time.Minute)
	retry, err := m.bindBootstrapAuthorizedKey(credentials.BootstrapToken, key)
	if err != nil || !retry.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("same-key retry was not idempotent: snapshot=%+v err=%v", retry, err)
	}
	other := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGeWzmZjZpwQDOkCd4bQfKSpKxj9gEcg8oS56B6WgD+q"
	if _, err := m.bindBootstrapAuthorizedKey(credentials.BootstrapToken, other); !errors.Is(err, errTunnelKeyMismatch) {
		t.Fatalf("expected different-key rejection, got %v", err)
	}
	if _, err := m.claim(credentials.ClientToken, tunnelRoleClient); err != nil {
		t.Fatal(err)
	}
	if _, err := m.bindBootstrapAuthorizedKey(credentials.BootstrapToken, key); !errors.Is(err, errTunnelTokenInvalid) {
		t.Fatalf("expected claimed bootstrap rejection, got %v", err)
	}
}

func TestCanonicalTunnelPublicKey(t *testing.T) {
	raw := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDQLa1lVd+AE1vXkONHkSzznDI3AOd87KEbIpNKSNsbl comment"
	canonical, err := canonicalTunnelPublicKey(raw)
	if err != nil || canonical != strings.TrimSuffix(raw, " comment") {
		t.Fatalf("unexpected canonical key: %q err=%v", canonical, err)
	}
	for _, invalid := range []string{"", "ssh-rsa AAAA", "ssh-ed25519 invalid***"} {
		if _, err := canonicalTunnelPublicKey(invalid); err == nil {
			t.Fatalf("expected key rejection for %q", invalid)
		}
	}
}

func TestTunnelBootstrapRemainsReadableUntilClientClaim(t *testing.T) {
	m := newTunnelManager()
	credentials, err := m.createWithBootstrap(7, "machine-a", "launcher:machine-a", "三闻鱼")
	if err != nil {
		t.Fatal(err)
	}
	defer m.cancelSession(credentials.TunnelID, "test cleanup")
	if credentials.BootstrapToken == "" || credentials.BootstrapToken == credentials.ClientToken {
		t.Fatalf("unexpected bootstrap credentials: %+v", credentials)
	}
	hash := sha256.Sum256([]byte(credentials.BootstrapToken))
	if m.bootstraps[hash] == nil {
		t.Fatal("bootstrap token hash was not indexed")
	}
	for i := 0; i < 2; i++ {
		snapshot, err := m.bootstrap(credentials.BootstrapToken)
		if err != nil {
			t.Fatalf("bootstrap lookup %d: %v", i, err)
		}
		if snapshot.ClientToken != credentials.ClientToken || snapshot.SSHUsername != "三闻鱼" {
			t.Fatalf("unexpected bootstrap snapshot: %+v", snapshot)
		}
	}
	if _, err := m.claim(credentials.DeviceToken, tunnelRoleDevice); err != nil {
		t.Fatalf("device claim: %v", err)
	}
	if _, err := m.bootstrap(credentials.BootstrapToken); err != nil {
		t.Fatalf("device claim consumed bootstrap: %v", err)
	}
	if _, err := m.claim(credentials.ClientToken, tunnelRoleClient); err != nil {
		t.Fatalf("client claim: %v", err)
	}
	if _, err := m.bootstrap(credentials.BootstrapToken); err != errTunnelTokenInvalid {
		t.Fatalf("expected bootstrap removal after client claim, got %v", err)
	}
}

func TestTunnelCreationResponseHidesLegacyCredentialsForBootstrapClients(t *testing.T) {
	credentials := tunnelCredentials{
		TunnelID:       "tun_test",
		ClientToken:    "raw-client-token",
		BootstrapToken: "bootstrap-token",
		ExpiresAt:      time.Now().UTC().Add(time.Minute),
	}
	response := tunnelCreationResponse(credentials, &model.SSHStatus{Listening: true}, 1)
	if _, exists := response["token"]; exists {
		t.Fatal("bootstrap response exposed the legacy client token")
	}
	if _, exists := response["client_path"]; exists {
		t.Fatal("bootstrap response exposed the legacy client path")
	}
	if response["bootstrap_unix_path"] == "" || response["bootstrap_windows_path"] == "" {
		t.Fatalf("bootstrap response is incomplete: %+v", response)
	}
}

func TestTunnelCreationResponseKeepsTemporaryLegacyCompatibility(t *testing.T) {
	credentials := tunnelCredentials{
		TunnelID:       "tun_test",
		ClientToken:    "raw-client-token",
		BootstrapToken: "bootstrap-token",
		ExpiresAt:      time.Now().UTC().Add(time.Minute),
	}
	response := tunnelCreationResponse(credentials, nil, 0)
	if response["token"] != credentials.ClientToken || response["client_path"] != tunnelClientPath {
		t.Fatalf("legacy response is incomplete: %+v", response)
	}
}

func TestTunnelBootstrapConcurrentReadsAndCancellation(t *testing.T) {
	m := newTunnelManager()
	credentials, err := m.createWithBootstrap(7, "machine-a", "launcher:machine-a", "demo")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 32)
	for i := 0; i < cap(errorsCh); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.bootstrap(credentials.BootstrapToken)
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent bootstrap lookup: %v", err)
		}
	}
	m.cancelSession(credentials.TunnelID, "test cancellation")
	if _, err := m.bootstrap(credentials.BootstrapToken); err != errTunnelTokenInvalid {
		t.Fatalf("expected bootstrap removal after cancellation, got %v", err)
	}
}

func TestTunnelBootstrapExpiresWithSession(t *testing.T) {
	m := newTunnelManager()
	now := time.Now().UTC()
	m.now = func() time.Time { return now }
	credentials, err := m.createWithBootstrap(7, "machine-a", "launcher:machine-a", "demo")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(tunnelTokenTTL + time.Second)
	if _, err := m.bootstrap(credentials.BootstrapToken); err != errTunnelTokenInvalid {
		t.Fatalf("expected expired bootstrap rejection, got %v", err)
	}
	m.mu.Lock()
	m.cleanupExpiredLocked(now)
	_, sessionExists := m.sessions[credentials.TunnelID]
	bootstrapCount := len(m.bootstraps)
	m.mu.Unlock()
	if sessionExists || bootstrapCount != 0 {
		t.Fatalf("expired bootstrap state remained: session=%v bootstraps=%d", sessionExists, bootstrapCount)
	}
}

func TestTunnelManagerClaimsTokensOnceAndPairs(t *testing.T) {
	m := newTunnelManager()
	credentials, err := m.create(7, "machine-a", "launcher:machine-a")
	if err != nil {
		t.Fatal(err)
	}
	session, err := m.claim(credentials.ClientToken, tunnelRoleClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.claim(credentials.ClientToken, tunnelRoleClient); err == nil {
		t.Fatal("expected replay rejection")
	}
	clientA, clientB := net.Pipe()
	deviceA, deviceB := net.Pipe()
	defer clientB.Close()
	defer deviceB.Close()
	m.attach(session, tunnelRoleClient, clientA)
	deviceSession, err := m.claim(credentials.DeviceToken, tunnelRoleDevice)
	if err != nil || deviceSession != session {
		t.Fatalf("expected same session for device claim, session=%v err=%v", deviceSession, err)
	}
	m.attach(session, tunnelRoleDevice, deviceA)

	message := []byte("ssh-test")
	go func() { _, _ = clientB.Write(message) }()
	buf := make([]byte, len(message))
	if _, err := deviceB.Read(buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(message) {
		t.Fatalf("unexpected bridged payload %q", buf)
	}
	m.finish(session, "test")
}

func TestTunnelBootstrapPairWindowIsFiveMinutes(t *testing.T) {
	if tunnelTokenTTL != 5*time.Minute || tunnelPairTimeout != 5*time.Minute {
		t.Fatalf("unexpected Tunnel bootstrap window: ttl=%s pair_timeout=%s", tunnelTokenTTL, tunnelPairTimeout)
	}
}

func TestTunnelManagerEnforcesConcurrentLimitAtomically(t *testing.T) {
	m := newTunnelManager()
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < tunnelMaxPerMachine+8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.create(1, "machine-a", "launcher:machine-a"); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != tunnelMaxPerMachine {
		t.Fatalf("expected exactly %d sessions, got %d", tunnelMaxPerMachine, successes)
	}
}

func TestTunnelManagerExpiresUnpairedSession(t *testing.T) {
	m := newTunnelManager()
	now := time.Now().UTC()
	m.now = func() time.Time { return now }
	credentials, err := m.create(1, "machine-a", "launcher:machine-a")
	if err != nil {
		t.Fatal(err)
	}
	session, err := m.claim(credentials.ClientToken, tunnelRoleClient)
	if err != nil {
		t.Fatal(err)
	}
	m.finish(session, "test expiration")
	if _, err := m.claim(credentials.DeviceToken, tunnelRoleDevice); err != errTunnelTokenInvalid {
		t.Fatalf("expected token removal after finish, got %v", err)
	}
}

func TestTunnelManagerBindsSessionToLauncherAndMachine(t *testing.T) {
	m := newTunnelManager()
	credentials, err := m.create(7, "machine-a", "launcher:machine-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.claim(credentials.DeviceToken, tunnelRoleClient); err == nil {
		t.Fatal("expected device token to be rejected by client role")
	}
	if _, err := m.claim(credentials.ClientToken, tunnelRoleDevice); err == nil {
		t.Fatal("expected client token to be rejected by device role")
	}
	session, err := m.claim(credentials.ClientToken, tunnelRoleClient)
	if err != nil {
		t.Fatal(err)
	}
	m.cancelLauncher(7, "machine-a", "launcher:other")
	m.mu.Lock()
	if m.sessions[session.id] == nil {
		t.Fatal("session was removed for a different launcher")
	}
	m.mu.Unlock()
	m.cancelLauncher(7, "machine-a", "launcher:machine-a")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[session.id] != nil {
		t.Fatal("session remained after launcher disconnect")
	}
	if _, ok := m.tokens[session.clientToken]; ok {
		t.Fatal("client token remained after session cleanup")
	}
}
