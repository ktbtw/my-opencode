package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"relay-server/internal/auth"
	"relay-server/internal/model"
)

const (
	tunnelTokenTTL         = 5 * time.Minute
	tunnelIdleTimeout      = 10 * time.Minute
	tunnelPairTimeout      = 5 * time.Minute
	tunnelMaxPerMachine    = 4
	tunnelWebSocketLimit   = 1 << 20
	tunnelTarget           = "127.0.0.1:22"
	tunnelAuthorizedKeyTTL = 24 * time.Hour
)

var (
	errTunnelTokenInvalid = errors.New("invalid or expired tunnel token")
	errTunnelTokenUsed    = errors.New("tunnel token already used")
	errTunnelLimit        = errors.New("too many active tunnels for device")
	errTunnelKeyMismatch  = errors.New("bootstrap token is already bound to another SSH key")
)

type tunnelRole uint8

const (
	tunnelRoleClient tunnelRole = iota + 1
	tunnelRoleDevice
)

type tunnelSession struct {
	id         string
	operatorID int64
	machineID  string
	launcherID string
	target     string
	createdAt  time.Time
	expiresAt  time.Time

	clientToken            [32]byte
	deviceToken            [32]byte
	bootstrapToken         [32]byte
	clientTokenValue       string
	sshUsername            string
	clientUsed             bool
	deviceUsed             bool
	authorizedKeyHash      [32]byte
	authorizedKeyExpiresAt time.Time
	authorizedKeySet       bool
	client                 net.Conn
	device                 net.Conn
	connMu                 sync.RWMutex

	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	doneOnce  sync.Once
	startOnce sync.Once
	activity  atomic.Int64
}

type tunnelManager struct {
	mu         sync.Mutex
	sessions   map[string]*tunnelSession
	tokens     map[[32]byte]*tunnelSession
	bootstraps map[[32]byte]*tunnelSession
	now        func() time.Time
}

type tunnelCredentials struct {
	TunnelID       string
	ClientToken    string
	DeviceToken    string
	BootstrapToken string
	ExpiresAt      time.Time
}

type tunnelBootstrapSnapshot struct {
	TunnelID    string
	ClientToken string
	SSHUsername string
	ExpiresAt   time.Time
}

type tunnelAuthorizedKeySnapshot struct {
	TunnelID   string
	OperatorID int64
	MachineID  string
	LauncherID string
	ExpiresAt  time.Time
}

func newTunnelManager() *tunnelManager {
	return &tunnelManager{
		sessions:   map[string]*tunnelSession{},
		tokens:     map[[32]byte]*tunnelSession{},
		bootstraps: map[[32]byte]*tunnelSession{},
		now:        time.Now,
	}
}

func (m *tunnelManager) create(operatorID int64, machineID, launcherID string) (tunnelCredentials, error) {
	return m.createWithBootstrap(operatorID, machineID, launcherID, "")
}

func (m *tunnelManager) createWithBootstrap(operatorID int64, machineID, launcherID, sshUsername string) (tunnelCredentials, error) {
	now := m.now().UTC()
	id, err := randomTunnelValue(16)
	if err != nil {
		return tunnelCredentials{}, err
	}
	clientToken, err := randomTunnelValue(32)
	if err != nil {
		return tunnelCredentials{}, err
	}
	deviceToken, err := randomTunnelValue(32)
	if err != nil {
		return tunnelCredentials{}, err
	}
	bootstrapToken := ""
	clientTokenValue := ""
	if sshUsername != "" {
		bootstrapToken, err = randomTunnelValue(32)
		if err != nil {
			return tunnelCredentials{}, err
		}
		clientTokenValue = clientToken
	}
	ctx, cancel := context.WithCancel(context.Background())
	session := &tunnelSession{
		id:               "tun_" + id,
		operatorID:       operatorID,
		machineID:        machineID,
		launcherID:       launcherID,
		target:           tunnelTarget,
		createdAt:        now,
		expiresAt:        now.Add(tunnelTokenTTL),
		clientToken:      sha256.Sum256([]byte(clientToken)),
		deviceToken:      sha256.Sum256([]byte(deviceToken)),
		clientTokenValue: clientTokenValue,
		sshUsername:      sshUsername,
		ctx:              ctx,
		cancel:           cancel,
		done:             make(chan struct{}),
	}
	session.activity.Store(now.UnixNano())

	m.mu.Lock()
	m.cleanupExpiredLocked(now)
	active := 0
	for _, current := range m.sessions {
		if current.operatorID == operatorID && current.machineID == machineID {
			active++
		}
	}
	if active >= tunnelMaxPerMachine {
		m.mu.Unlock()
		cancel()
		return tunnelCredentials{}, errTunnelLimit
	}
	m.sessions[session.id] = session
	m.tokens[session.clientToken] = session
	m.tokens[session.deviceToken] = session
	if bootstrapToken != "" {
		session.bootstrapToken = sha256.Sum256([]byte(bootstrapToken))
		m.bootstraps[session.bootstrapToken] = session
	}
	m.mu.Unlock()
	go m.expireUnpaired(session)
	return tunnelCredentials{
		TunnelID: session.id, ClientToken: clientToken, DeviceToken: deviceToken,
		BootstrapToken: bootstrapToken, ExpiresAt: session.expiresAt,
	}, nil
}

func (m *tunnelManager) bootstrap(rawToken string) (tunnelBootstrapSnapshot, error) {
	hash := sha256.Sum256([]byte(strings.TrimSpace(rawToken)))
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.bootstraps[hash]
	if session == nil || hash != session.bootstrapToken || session.clientUsed || now.After(session.expiresAt) {
		return tunnelBootstrapSnapshot{}, errTunnelTokenInvalid
	}
	return tunnelBootstrapSnapshot{
		TunnelID: session.id, ClientToken: session.clientTokenValue,
		SSHUsername: session.sshUsername, ExpiresAt: session.expiresAt,
	}, nil
}

func (m *tunnelManager) bootstrapActive(rawToken, tunnelID string) bool {
	hash := sha256.Sum256([]byte(strings.TrimSpace(rawToken)))
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.bootstraps[hash]
	return session != nil && session.id == tunnelID && !session.clientUsed && !m.now().UTC().After(session.expiresAt)
}

func (m *tunnelManager) bindBootstrapAuthorizedKey(rawToken, canonicalPublicKey string) (tunnelAuthorizedKeySnapshot, error) {
	tokenHash := sha256.Sum256([]byte(strings.TrimSpace(rawToken)))
	keyHash := sha256.Sum256([]byte(canonicalPublicKey))
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.bootstraps[tokenHash]
	if session == nil || tokenHash != session.bootstrapToken || session.clientUsed || now.After(session.expiresAt) {
		return tunnelAuthorizedKeySnapshot{}, errTunnelTokenInvalid
	}
	if session.authorizedKeySet && session.authorizedKeyHash != keyHash {
		return tunnelAuthorizedKeySnapshot{}, errTunnelKeyMismatch
	}
	if !session.authorizedKeySet {
		session.authorizedKeySet = true
		session.authorizedKeyHash = keyHash
		session.authorizedKeyExpiresAt = now.Add(tunnelAuthorizedKeyTTL)
	}
	return tunnelAuthorizedKeySnapshot{
		TunnelID: session.id, OperatorID: session.operatorID, MachineID: session.machineID,
		LauncherID: session.launcherID, ExpiresAt: session.authorizedKeyExpiresAt,
	}, nil
}

func canonicalTunnelPublicKey(raw string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) < 2 || fields[0] != "ssh-ed25519" {
		return "", errors.New("invalid SSH public key")
	}
	decoded, err := base64.StdEncoding.DecodeString(fields[1])
	const ed25519KeyType = "ssh-ed25519"
	if err != nil || len(decoded) != 4+len(ed25519KeyType)+4+32 ||
		binary.BigEndian.Uint32(decoded[:4]) != uint32(len(ed25519KeyType)) ||
		string(decoded[4:4+len(ed25519KeyType)]) != ed25519KeyType ||
		binary.BigEndian.Uint32(decoded[4+len(ed25519KeyType):4+len(ed25519KeyType)+4]) != 32 {
		return "", errors.New("invalid SSH public key")
	}
	return fields[0] + " " + fields[1], nil
}

func (m *tunnelManager) claim(rawToken string, role tunnelRole) (*tunnelSession, error) {
	hash := sha256.Sum256([]byte(strings.TrimSpace(rawToken)))
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.tokens[hash]
	if session == nil || now.After(session.expiresAt) {
		return nil, errTunnelTokenInvalid
	}
	if role == tunnelRoleClient {
		if hash != session.clientToken {
			return nil, errTunnelTokenInvalid
		}
		if session.clientUsed {
			return nil, errTunnelTokenUsed
		}
		session.clientUsed = true
		delete(m.bootstraps, session.bootstrapToken)
		session.clientTokenValue = ""
	} else {
		if hash != session.deviceToken {
			return nil, errTunnelTokenInvalid
		}
		if session.deviceUsed {
			return nil, errTunnelTokenUsed
		}
		session.deviceUsed = true
	}
	delete(m.tokens, hash)
	return session, nil
}

func (m *tunnelManager) attach(session *tunnelSession, role tunnelRole, conn net.Conn) {
	m.mu.Lock()
	if current := m.sessions[session.id]; current != session {
		m.mu.Unlock()
		_ = conn.Close()
		return
	}
	session.connMu.Lock()
	if role == tunnelRoleClient {
		session.client = conn
	} else {
		session.device = conn
	}
	ready := session.client != nil && session.device != nil
	session.connMu.Unlock()
	m.mu.Unlock()
	if ready {
		session.startOnce.Do(func() { go m.bridge(session) })
	}
}

func (m *tunnelManager) bridge(session *tunnelSession) {
	log.Printf("[tunnel] paired tunnel_id=%s operator_id=%d machine_id=%s target=%s", session.id, session.operatorID, session.machineID, session.target)
	session.connMu.RLock()
	clientConn, deviceConn := session.client, session.device
	session.connMu.RUnlock()
	client := &activityConn{Conn: clientConn, activity: &session.activity}
	device := &activityConn{Conn: deviceConn, activity: &session.activity}
	type copyResult struct {
		direction string
		err       error
	}
	results := make(chan copyResult, 2)
	go func() {
		_, err := io.Copy(device, client)
		results <- copyResult{direction: "client_to_device", err: err}
	}()
	go func() {
		_, err := io.Copy(client, device)
		results <- copyResult{direction: "device_to_client", err: err}
	}()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-session.ctx.Done():
			m.finish(session, "cancelled")
			return
		case result := <-results:
			reason := result.direction + " closed"
			if result.err != nil {
				reason += ": " + result.err.Error()
			}
			m.finish(session, reason)
			return
		case <-ticker.C:
			last := time.Unix(0, session.activity.Load())
			if m.now().Sub(last) >= tunnelIdleTimeout {
				m.finish(session, "idle timeout")
				return
			}
		}
	}
}

func (m *tunnelManager) expireUnpaired(session *tunnelSession) {
	timer := time.NewTimer(tunnelPairTimeout)
	defer timer.Stop()
	select {
	case <-session.ctx.Done():
	case <-session.done:
	case <-timer.C:
		m.finish(session, "pair timeout")
	}
}

func (m *tunnelManager) finish(session *tunnelSession, reason string) {
	session.doneOnce.Do(func() {
		session.cancel()
		m.mu.Lock()
		session.connMu.RLock()
		client, device := session.client, session.device
		session.connMu.RUnlock()
		delete(m.sessions, session.id)
		delete(m.tokens, session.clientToken)
		delete(m.tokens, session.deviceToken)
		delete(m.bootstraps, session.bootstrapToken)
		session.clientTokenValue = ""
		m.mu.Unlock()
		if client != nil {
			_ = client.Close()
		}
		if device != nil {
			_ = device.Close()
		}
		close(session.done)
		log.Printf("[tunnel] closed tunnel_id=%s operator_id=%d machine_id=%s reason=%s", session.id, session.operatorID, session.machineID, reason)
	})
}

func (m *tunnelManager) cancelSession(id, reason string) {
	m.mu.Lock()
	session := m.sessions[id]
	m.mu.Unlock()
	if session != nil {
		m.finish(session, reason)
	}
}

func (m *tunnelManager) cancelLauncher(operatorID int64, machineID, launcherID string) {
	m.mu.Lock()
	var sessions []*tunnelSession
	for _, session := range m.sessions {
		if session.operatorID == operatorID && session.machineID == machineID && session.launcherID == launcherID {
			sessions = append(sessions, session)
		}
	}
	m.mu.Unlock()
	for _, session := range sessions {
		m.finish(session, "launcher disconnected")
	}
}

func (m *tunnelManager) cleanupExpiredLocked(now time.Time) {
	var expired []*tunnelSession
	for _, session := range m.sessions {
		session.connMu.RLock()
		unpaired := session.client == nil || session.device == nil
		session.connMu.RUnlock()
		if now.After(session.expiresAt) && unpaired {
			expired = append(expired, session)
		}
	}
	for _, session := range expired {
		delete(m.sessions, session.id)
		delete(m.tokens, session.clientToken)
		delete(m.tokens, session.deviceToken)
		delete(m.bootstraps, session.bootstrapToken)
		session.clientTokenValue = ""
		session.cancel()
		session.doneOnce.Do(func() { close(session.done) })
	}
}

type activityConn struct {
	net.Conn
	activity *atomic.Int64
}

func (c *activityConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.activity.Store(time.Now().UnixNano())
	}
	return n, err
}

func (c *activityConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.activity.Store(time.Now().UnixNano())
	}
	return n, err
}

func randomTunnelValue(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (a *App) createTunnel(w http.ResponseWriter, r *http.Request) {
	if a.tunnels == nil {
		writeAppJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "隧道服务未初始化"})
		return
	}
	operator, ok := a.operatorFromRequest(r)
	if !ok {
		writeAppJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var input struct {
		Username         string `json:"username"`
		BootstrapVersion int    `json:"bootstrap_version"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
			writeAppJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
	}
	username, err := validateTunnelSSHUsername(input.Username)
	if err != nil {
		writeAppJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	machineID := strings.TrimSpace(chi.URLParam(r, "machineID"))
	launcher, ok := a.broker.GetLauncher(machineID, operator.ID)
	if !ok {
		writeAppJSON(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	statusResult, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcher.ID,
		fmt.Sprintf("ssh_status_%d", time.Now().UnixNano()), "device.launcher.ssh_status", map[string]string{"machine_id": machineID})
	if err != nil {
		writeAppJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if statusResult.SSH == nil || !statusResult.SSH.Listening {
		message := "设备 OpenSSH Server 未监听 127.0.0.1:22"
		if statusResult.SSH != nil {
			if statusResult.SSH.Error != "" {
				message = statusResult.SSH.Error
			} else if statusResult.SSH.Message != "" {
				message = statusResult.SSH.Message
			}
		}
		writeAppJSON(w, http.StatusConflict, map[string]any{"error": message, "ssh": statusResult.SSH})
		return
	}
	credentials, err := a.tunnels.createWithBootstrap(operator.ID, machineID, launcher.ID, username)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errTunnelLimit) {
			status = http.StatusTooManyRequests
		}
		writeAppJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	payload := model.DeviceLauncherTunnelOpenPayload{
		MachineID: machineID,
		TunnelID:  credentials.TunnelID,
		Token:     credentials.DeviceToken,
		TunnelURL: tunnelDevicePath,
	}
	if err := a.broker.DispatchForOperator(r.Context(), operator.ID, launcher.ID, model.Envelope{
		Type: "device.launcher.tunnel.open", RequestID: credentials.TunnelID,
		SentAt: time.Now().UTC().Format(time.RFC3339), Payload: payload,
	}); err != nil {
		a.tunnels.cancelSession(credentials.TunnelID, "control dispatch failed")
		writeAppJSON(w, http.StatusConflict, map[string]string{"error": "设备控制连接已断开"})
		return
	}
	response := tunnelCreationResponse(credentials, statusResult.SSH, input.BootstrapVersion)
	writeAppJSON(w, http.StatusCreated, response)
}

func tunnelCreationResponse(credentials tunnelCredentials, ssh *model.SSHStatus, bootstrapVersion int) map[string]any {
	response := map[string]any{
		"tunnel_id":  credentials.TunnelID,
		"target":     tunnelTarget,
		"expires_at": credentials.ExpiresAt,
		"ssh":        ssh,
	}
	if credentials.BootstrapToken != "" {
		base := "/b/" + credentials.BootstrapToken
		response["bootstrap_unix_path"] = base + "/unix"
		response["bootstrap_windows_path"] = base + "/windows"
	}
	if bootstrapVersion < 1 {
		response["token"] = credentials.ClientToken
		response["client_path"] = tunnelClientPath
	}
	return response
}

func (a *App) tunnelClient(w http.ResponseWriter, r *http.Request) {
	a.serveTunnelWebSocket(w, r, tunnelRoleClient)
}

func (a *App) tunnelBootstrapAuthorizedKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if a.tunnels == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rawToken := strings.TrimSpace(chi.URLParam(r, "bootstrapToken"))
	if len(rawToken) != 64 || !validTunnelSHA256(rawToken) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var publicKey string
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "application/json") {
		var input struct {
			PublicKey string `json:"public_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeAppJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
		publicKey = input.PublicKey
	} else {
		if err := r.ParseForm(); err != nil {
			writeAppJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
		publicKey = r.Form.Get("public_key")
	}
	canonical, err := canonicalTunnelPublicKey(publicKey)
	if err != nil {
		writeAppJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	snapshot, err := a.tunnels.bindBootstrapAuthorizedKey(rawToken, canonical)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, errTunnelKeyMismatch) {
			status = http.StatusConflict
		}
		writeAppJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	requestID := fmt.Sprintf("ssh_key_%s_%d", snapshot.TunnelID, time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), snapshot.OperatorID, snapshot.LauncherID, requestID,
		"device.launcher.ssh_authorized_key", model.DeviceLauncherSSHAuthorizedKeyPayload{
			MachineID: snapshot.MachineID, PublicKey: canonical, ExpiresAt: snapshot.ExpiresAt,
		})
	if err != nil {
		writeAppJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeAppJSON(w, http.StatusOK, map[string]any{
		"tunnel_id": snapshot.TunnelID, "machine_id": snapshot.MachineID,
		"expires_at": snapshot.ExpiresAt, "authorized_key": result.SSHAuthorizedKey,
	})
}

func (a *App) tunnelDevice(w http.ResponseWriter, r *http.Request) {
	a.serveTunnelWebSocket(w, r, tunnelRoleDevice)
}

func (a *App) serveTunnelWebSocket(w http.ResponseWriter, r *http.Request, role tunnelRole) {
	session, err := a.tunnels.claim(r.URL.Query().Get("token"), role)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		a.tunnels.cancelSession(session.id, "websocket accept failed")
		return
	}
	conn.SetReadLimit(tunnelWebSocketLimit)
	stream := websocket.NetConn(session.ctx, conn, websocket.MessageBinary)
	a.tunnels.attach(session, role, stream)
	select {
	case <-session.done:
	case <-r.Context().Done():
		a.tunnels.finish(session, "request disconnected")
	}
}

func (a *App) operatorFromRequest(r *http.Request) (model.Operator, bool) {
	token := auth.ExtractBearer(r.Header.Get("Authorization"))
	if token == "" {
		return model.Operator{}, false
	}
	return a.auth.Verify(token)
}

func writeAppJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
