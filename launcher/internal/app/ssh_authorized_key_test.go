//go:build !windows

package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"launcher/internal/model"

	"golang.org/x/crypto/ssh"
)

func TestSSHInstallAuthorizedKeyWritesAndDeduplicates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " test-client"

	svc := &service{}
	first := svc.SSHInstallAuthorizedKey(raw)
	if !first.Installed || first.AlreadyExists || first.Error != "" {
		t.Fatalf("unexpected first install result: %+v", first)
	}
	if first.Fingerprint != ssh.FingerprintSHA256(key) {
		t.Fatalf("unexpected fingerprint: %s", first.Fingerprint)
	}
	second := svc.SSHInstallAuthorizedKey(raw)
	if !second.Installed || !second.AlreadyExists || second.Error != "" {
		t.Fatalf("unexpected duplicate result: %+v", second)
	}

	dir := filepath.Join(home, ".ssh")
	path := filepath.Join(dir, "authorized_keys")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(string(data)), "\n") != 0 {
		t.Fatalf("expected one authorized key, got %q", data)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected SSH permissions: dir=%o file=%o", dirInfo.Mode().Perm(), fileInfo.Mode().Perm())
	}
}

func TestSSHInstallAuthorizedKeyRejectsInvalidInput(t *testing.T) {
	result := (&service{}).SSHInstallAuthorizedKey("-----BEGIN OPENSSH PRIVATE KEY-----")
	if result.Error == "" || result.Installed {
		t.Fatalf("expected invalid key error, got %+v", result)
	}
}

func TestSSHInstallManagedAuthorizedKeyReplacesOnlyMatchingManagedEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	managed := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	manualPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manualKey, err := ssh.NewPublicKey(manualPublic)
	if err != nil {
		t.Fatal(err)
	}
	manual := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(manualKey))) + " manual-key"
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "authorized_keys")
	old := `expiry-time="20260101000000Z" ` + managed + " " + sshManagedAuthorizedKeyComment
	if err := os.WriteFile(path, []byte(manual+"\n"+old+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	expiresAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	input := model.SSHManagedAuthorizedKeyInput{PublicKey: managed + " ignored-comment", ExpiresAt: expiresAt}
	first := (&service{}).SSHInstallManagedAuthorizedKey(input)
	if !first.Installed || first.AlreadyExists || first.Error != "" || !first.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("unexpected first managed install: %+v", first)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	expected := `expiry-time="` + expiresAt.Format("20060102150405Z") + `" ` + managed + " " + sshManagedAuthorizedKeyComment
	if !strings.Contains(content, manual) || !strings.Contains(content, expected) || strings.Contains(content, old) {
		t.Fatalf("managed rewrite did not preserve manual keys or replace old entry: %q", content)
	}
	if strings.Count(content, sshManagedAuthorizedKeyComment) != 1 {
		t.Fatalf("expected one managed entry, got %q", content)
	}
	second := (&service{}).SSHInstallManagedAuthorizedKey(input)
	if !second.Installed || !second.AlreadyExists || second.Error != "" {
		t.Fatalf("unexpected idempotent managed install: %+v", second)
	}
}

func TestSSHInstallManagedAuthorizedKeyValidatesAndClampsExpiry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	before := time.Now().UTC()
	result := (&service{}).SSHInstallManagedAuthorizedKey(model.SSHManagedAuthorizedKeyInput{
		PublicKey: raw, ExpiresAt: before.Add(72 * time.Hour),
	})
	if !result.Installed || result.Error != "" {
		t.Fatalf("unexpected clamped install result: %+v", result)
	}
	if result.ExpiresAt.After(before.Add(sshManagedAuthorizedKeyTTL+time.Second)) || result.ExpiresAt.Before(before.Add(sshManagedAuthorizedKeyTTL-time.Second)) {
		t.Fatalf("expiry was not clamped to 24 hours: %s", result.ExpiresAt)
	}
	for _, input := range []model.SSHManagedAuthorizedKeyInput{
		{PublicKey: "invalid", ExpiresAt: time.Now().Add(time.Hour)},
		{PublicKey: raw, ExpiresAt: time.Now().Add(-time.Minute)},
	} {
		if invalid := (&service{}).SSHInstallManagedAuthorizedKey(input); invalid.Error == "" || invalid.Installed {
			t.Fatalf("expected managed input rejection: %+v", invalid)
		}
	}
}
