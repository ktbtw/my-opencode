package process

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	gracefulStopTimeout = 3 * time.Second
	forceStopTimeout    = 7 * time.Second
	portReleaseTimeout  = 5 * time.Second
)

var commandLogFiles sync.Map
var pidLogFiles sync.Map

type LaunchInput struct {
	BinaryName string
	AgentID    string
	Args       []string
	Dir        string
	Env        map[string]string
	StdoutPath string
	StderrPath string
	PrintLogs  bool
}

func Start(input LaunchInput) (*exec.Cmd, error) {
	bin, err := resolveExecutable(input.BinaryName)
	if err != nil {
		return nil, err
	}
	stdout, err := os.OpenFile(input.StdoutPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	stderr, err := os.OpenFile(input.StderrPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	cmd := exec.Command(bin, input.Args...)
	cmd.Dir = input.Dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	configureBackgroundCommand(cmd)
	if input.PrintLogs {
		cmd.Stdout = io.MultiWriter(stdout, newConsoleMirror(os.Stdout, input.AgentID, "stdout"))
		cmd.Stderr = io.MultiWriter(stderr, newConsoleMirror(os.Stderr, input.AgentID, "stderr"))
	}
	env := mergeEnv(os.Environ(), input.Env)
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, err
	}
	closers := []io.Closer{stdout, stderr}
	commandLogFiles.Store(cmd, closers)
	if cmd.Process != nil {
		pidLogFiles.Store(cmd.Process.Pid, closers)
	}
	return cmd, nil
}

func resolveExecutable(name string) (string, error) {
	bin, err := exec.LookPath(name)
	if err != nil && !errors.Is(err, exec.ErrDot) {
		return "", err
	}
	if bin == "" {
		bin = name
	}
	if !filepath.IsAbs(bin) {
		if abs, absErr := filepath.Abs(bin); absErr == nil {
			bin = abs
		}
	}
	return bin, nil
}

func mergeEnv(base []string, patch map[string]string) []string {
	if len(patch) == 0 {
		return base
	}
	index := map[string]int{}
	for i, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		index[envKey(key)] = i
	}
	env := append([]string{}, base...)
	for key, value := range patch {
		normalized := envKey(key)
		item := key + "=" + value
		if i, ok := index[normalized]; ok {
			env[i] = item
			continue
		}
		index[normalized] = len(env)
		env = append(env, item)
	}
	return env
}

func envKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(key)
	}
	return key
}

func DefaultBinaryLogs(runtimeDir string, agentID string) (string, string) {
	base := filepath.Join(runtimeDir, "agents", agentID)
	return filepath.Join(base, "stdout.log"), filepath.Join(base, "stderr.log")
}

type consoleMirror struct {
	out    io.Writer
	agent  string
	stream string
	buf    bytes.Buffer
}

func newConsoleMirror(out io.Writer, agentID string, stream string) *consoleMirror {
	return &consoleMirror{
		out:    out,
		agent:  agentID,
		stream: stream,
	}
}

func (w *consoleMirror) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if _, err := w.buf.Write(p); err != nil {
		return 0, err
	}
	reader := bufio.NewReader(&w.buf)
	var pending bytes.Buffer
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if len(line) > 0 {
				_, _ = pending.WriteString(line)
			}
			break
		}
		if _, writeErr := fmt.Fprintf(w.out, "[agent:%s][%s] %s", w.agent, w.stream, line); writeErr != nil {
			return 0, writeErr
		}
	}
	w.buf.Reset()
	if pending.Len() > 0 {
		if _, err := w.buf.Write(pending.Bytes()); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func Stop(pid int) error {
	ctx, cancel := context.WithTimeout(context.Background(), gracefulStopTimeout+forceStopTimeout)
	defer cancel()
	return StopWithTimeout(ctx, pid, 200*time.Millisecond)
}

// StopProcessesByExecutable stops running processes whose executable path
// matches path, excluding the supplied process IDs.
func StopProcessesByExecutable(ctx context.Context, path string, exclude ...int) (int, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, nil
	}
	excluded := make(map[int]struct{}, len(exclude))
	for _, pid := range exclude {
		if pid > 0 {
			excluded[pid] = struct{}{}
		}
	}
	pids, err := platformProcessesByExecutable(path)
	if err != nil {
		return 0, err
	}
	stopped := 0
	for _, pid := range pids {
		if _, skip := excluded[pid]; skip {
			continue
		}
		if err := StopWithTimeout(ctx, pid, 100*time.Millisecond); err != nil {
			return stopped, err
		}
		stopped++
	}
	return stopped, nil
}

func StopWithTimeout(ctx context.Context, pid int, interval time.Duration) error {
	defer closeLogFilesForPID(pid)
	if runtime.GOOS == "windows" {
		return stopWindowsTree(ctx, pid, interval)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if running, err := isRunning(proc); err != nil || !running {
		return err
	}
	if err := signalGraceful(proc); err != nil {
		return err
	}
	graceCtx, cancelGrace := context.WithTimeout(ctx, gracefulStopTimeout)
	err = WaitForExit(graceCtx, pid, interval)
	cancelGrace()
	if err == nil {
		return nil
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return err
	}
	if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		if running, checkErr := isRunning(proc); checkErr != nil {
			return checkErr
		} else if running {
			return err
		}
	}
	forceCtx, cancelForce := context.WithTimeout(ctx, forceStopTimeout)
	defer cancelForce()
	return WaitForExit(forceCtx, pid, interval)
}

func stopWindowsTree(ctx context.Context, pid int, interval time.Duration) error {
	return platformStopWindowsTree(ctx, pid, interval)
}

func WaitForExit(ctx context.Context, pid int, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		proc, err := os.FindProcess(pid)
		if err != nil {
			return nil
		}
		running, err := isRunning(proc)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func signalGraceful(proc *os.Process) error {
	if runtime.GOOS == "windows" {
		return proc.Kill()
	}
	return proc.Signal(syscall.SIGTERM)
}

func isRunning(proc *os.Process) (bool, error) {
	if runtime.GOOS == "windows" {
		return windowsProcessExists(proc.Pid)
	}
	err := proc.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false, nil
	}
	return false, nil
}

func windowsProcessExists(pid int) (bool, error) {
	return platformWindowsProcessExists(pid)
}

func Wait(cmd *exec.Cmd) error {
	if cmd == nil {
		return errors.New("nil command")
	}
	err := cmd.Wait()
	if closers, ok := commandLogFiles.LoadAndDelete(cmd); ok {
		closeLogFiles(closers.([]io.Closer))
	}
	if cmd.Process != nil {
		closeLogFilesForPID(cmd.Process.Pid)
	}
	return err
}

func WaitAsync(cmd *exec.Cmd) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- Wait(cmd)
		close(ch)
	}()
	return ch
}

func closeLogFilesForPID(pid int) {
	if pid <= 0 {
		return
	}
	if closers, ok := pidLogFiles.LoadAndDelete(pid); ok {
		closeLogFiles(closers.([]io.Closer))
	}
}

func closeLogFiles(closers []io.Closer) {
	for _, closer := range closers {
		_ = closer.Close()
	}
}

func WaitForTCPPort(ctx context.Context, port int, interval time.Duration) error {
	address := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func WaitForHealth(ctx context.Context, port int, interval time.Duration) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func WaitForReady(ctx context.Context, port int, interval time.Duration) error {
	address := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func WaitForPortClosed(ctx context.Context, port int, interval time.Duration) error {
	address := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
		if err != nil {
			if _, pidErr := FindListeningPID(port); pidErr != nil {
				return nil
			}
		} else {
			_ = conn.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// StopListeningProcessUntilPortClosed handles a process that was spawned after
// the initial process-tree snapshot. This is common on Windows when a launcher
// child briefly replaces or reopens the Agent listener during shutdown.
func StopListeningProcessUntilPortClosed(
	ctx context.Context,
	port int,
	interval time.Duration,
) error {
	if port <= 0 {
		return nil
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		pid, findErr := FindListeningPID(port)
		if findErr == nil && pid > 0 && pid != os.Getpid() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			_ = StopWithTimeout(stopCtx, pid, interval)
			cancel()
		} else if findErr != nil {
			address := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
			conn, dialErr := net.DialTimeout("tcp", address, 300*time.Millisecond)
			if dialErr != nil {
				return nil
			}
			_ = conn.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func CleanName(input string) string {
	input = strings.TrimSpace(input)
	input = strings.ReplaceAll(input, " ", "-")
	if input == "" {
		return "agent"
	}
	return input
}

var ErrAgentNotFound = errors.New("agent not found")
var ErrListeningPIDNotFound = errors.New("listening pid not found")

func IsRunning(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}
	return isRunning(proc)
}

func IsHealthy(port int, timeout time.Duration) bool {
	if port <= 0 {
		return false
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

func FindListeningPID(port int) (int, error) {
	if port <= 0 {
		return 0, ErrListeningPIDNotFound
	}
	switch runtime.GOOS {
	case "windows":
		return findListeningPIDWindows(port)
	default:
		return findListeningPIDUnix(port)
	}
}

func findListeningPIDUnix(port int) (int, error) {
	bin, err := exec.LookPath("lsof")
	if err != nil {
		return 0, ErrListeningPIDNotFound
	}
	cmd := exec.Command(bin, "-nP", "-iTCP:"+fmt.Sprintf("%d", port), "-sTCP:LISTEN", "-t")
	out, err := cmd.Output()
	if err != nil {
		return 0, ErrListeningPIDNotFound
	}
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		value := strings.TrimSpace(string(line))
		if value == "" {
			continue
		}
		pid, parseErr := strconv.Atoi(value)
		if parseErr == nil && pid > 0 {
			return pid, nil
		}
	}
	return 0, ErrListeningPIDNotFound
}

func findListeningPIDWindows(port int) (int, error) {
	cmd := exec.Command("netstat", "-ano", "-p", "tcp")
	configureBackgroundCommand(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0, ErrListeningPIDNotFound
	}
	wantPort := fmt.Sprintf(":%d", port)
	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		if !strings.HasSuffix(fields[1], wantPort) {
			continue
		}
		if !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		pid, parseErr := strconv.Atoi(fields[4])
		if parseErr == nil && pid > 0 {
			return pid, nil
		}
	}
	return 0, ErrListeningPIDNotFound
}
