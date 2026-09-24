package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDeviceInboxReadsPongDuringSlowHandlerAndPreservesOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	received := make(chan string, 8)
	serverConn := make(chan *websocket.Conn, 1)
	finished := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		serverConn <- c
		finished <- consumeDeviceMessages(ctx, c, func(data []byte) error {
			if string(data) == "0" {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			received <- string(data)
			return nil
		}, nil)
	}))
	defer srv.Close()
	client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	client.CloseRead(ctx)
	c := <-serverConn
	for i := 0; i < 8; i++ {
		if err := client.Write(ctx, websocket.MessageText, []byte(fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pingCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err := c.Ping(pingCtx); err != nil {
		t.Fatalf("storage handling blocked pong: %v", err)
	}
	close(release)
	for i := 0; i < 8; i++ {
		select {
		case got := <-received:
			if got != fmt.Sprint(i) {
				t.Fatalf("order: got %s want %d", got, i)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	client.CloseNow()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("reader did not stop")
	}
}
