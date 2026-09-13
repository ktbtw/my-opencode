package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"launcher/internal/config"
	"launcher/internal/model"
)

const launcherTunnelTarget = "127.0.0.1:22"

func handleTunnelOpen(ctx context.Context, env envelope, cfg config.Config) {
	var payload model.TunnelOpenPayload
	if err := decodePayload(env.Payload, &payload); err != nil {
		log.Printf("[launcher.tunnel] invalid open payload request_id=%s err=%v", env.RequestID, err)
		return
	}
	if payload.MachineID != cfg.Relay.MachineID || payload.TunnelID == "" || payload.Token == "" {
		log.Printf("[launcher.tunnel] rejected mismatched open request tunnel_id=%s", payload.TunnelID)
		return
	}
	local, err := net.DialTimeout("tcp", launcherTunnelTarget, 5*time.Second)
	if err != nil {
		log.Printf("[launcher.tunnel] local ssh unavailable tunnel_id=%s err=%v", payload.TunnelID, err)
		return
	}
	defer local.Close()
	tunnelURL, err := resolveTunnelURL(cfg.Relay.URL, payload.TunnelURL, payload.Token)
	if err != nil {
		log.Printf("[launcher.tunnel] invalid tunnel url tunnel_id=%s err=%v", payload.TunnelID, err)
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dialCtx, tunnelURL, nil)
	cancel()
	if err != nil {
		log.Printf("[launcher.tunnel] websocket dial failed tunnel_id=%s err=%v", payload.TunnelID, err)
		return
	}
	conn.SetReadLimit(tunnelWebSocketLimit)
	stream := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	defer stream.Close()
	log.Printf("[launcher.tunnel] connected tunnel_id=%s target=%s", payload.TunnelID, launcherTunnelTarget)
	type copyResult struct {
		direction string
		err       error
	}
	results := make(chan copyResult, 2)
	go func() {
		_, copyErr := io.Copy(stream, local)
		results <- copyResult{direction: "sshd_to_tunnel", err: copyErr}
	}()
	go func() {
		_, copyErr := io.Copy(local, stream)
		results <- copyResult{direction: "tunnel_to_sshd", err: copyErr}
	}()
	select {
	case <-ctx.Done():
		log.Printf("[launcher.tunnel] context closed tunnel_id=%s err=%v", payload.TunnelID, ctx.Err())
	case result := <-results:
		log.Printf("[launcher.tunnel] stream ended tunnel_id=%s direction=%s err=%v", payload.TunnelID, result.direction, result.err)
	}
	log.Printf("[launcher.tunnel] closed tunnel_id=%s", payload.TunnelID)
}

const tunnelWebSocketLimit = 1 << 20

func resolveTunnelURL(relayURL, tunnelPath, token string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(relayURL))
	if err != nil || base.Host == "" {
		return "", fmt.Errorf("invalid relay URL")
	}
	path := strings.TrimSpace(tunnelPath)
	if path == "" {
		path = "/ws/tunnel/device"
	}
	if parsed, parseErr := url.Parse(path); parseErr == nil && parsed.IsAbs() {
		base = parsed
	} else {
		prefix := strings.TrimSuffix(base.Path, "/ws/device")
		base.Path = strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(path, "/")
		base.RawPath = ""
		base.RawQuery = ""
	}
	query := base.Query()
	query.Set("token", token)
	base.RawQuery = query.Encode()
	return base.String(), nil
}

func decodePayload(input any, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}
