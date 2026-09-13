package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"testing"
)

func TestDecodeTunnelTicket(t *testing.T) {
	data, err := json.Marshal(tunnelTicket{
		Server:     "https://relay.example/codex",
		ClientPath: "/ws/tunnel/client",
		Token:      "TOKEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := decodeTunnelTicket(base64.RawURLEncoding.EncodeToString(data))
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Server != "https://relay.example/codex" || ticket.ClientPath != "/ws/tunnel/client" || ticket.Token != "TOKEN" {
		t.Fatalf("unexpected ticket: %+v", ticket)
	}
}

func TestDecodeTunnelTicketRejectsIncompletePayload(t *testing.T) {
	data, err := json.Marshal(tunnelTicket{Server: "https://relay.example/codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeTunnelTicket(base64.RawURLEncoding.EncodeToString(data)); err == nil {
		t.Fatal("expected incomplete ticket error")
	}
}

func TestTunnelWebSocketURLPreservesBasePath(t *testing.T) {
	got, err := tunnelWebSocketURL("https://relay.example/codex", "/ws/tunnel/client", "TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://relay.example/codex/ws/tunnel/client?token=TOKEN" {
		t.Fatalf("unexpected URL %q", got)
	}
}

func TestTunnelWebSocketURLUsesWSSForHTTPS(t *testing.T) {
	got, err := tunnelWebSocketURL("https://relay.example/codex", "/ws/tunnel/client", "TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if got[:6] != "wss://" {
		t.Fatalf("expected wss URL, got %q", got)
	}
}

func TestBridgeCopiesBinaryBytes(t *testing.T) {
	input := bytes.NewBuffer([]byte{0x00, 0x01, 0xff, 0x7f})
	stream, peer := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- bridge(ctx, input, &bytes.Buffer{}, stream) }()
	got := make([]byte, 4)
	if _, err := peer.Read(got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x00, 0x01, 0xff, 0x7f}) {
		t.Fatalf("unexpected bytes %x", got)
	}
	_ = peer.Close()
	if err := <-result; err != nil {
		t.Fatalf("bridge returned error: %v", err)
	}
}
