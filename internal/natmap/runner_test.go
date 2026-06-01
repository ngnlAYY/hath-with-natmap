package natmap

import (
	"bytes"
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/process"
)

func TestRunnerConfigArgsBuildsBindModeArguments(t *testing.T) {
	tests := []struct {
		name          string
		addressFamily string
		wantAF        string
	}{
		{name: "default address family", addressFamily: "", wantAF: "-4"},
		{name: "explicit ipv4", addressFamily: "ipv4", wantAF: "-4"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := RunnerConfig{
				AddressFamily:       tc.addressFamily,
				BindPort:            16000,
				StunServer:          "stun.example.com:3478",
				HTTPKeepaliveServer: "https://keepalive.example.com",
				KeepaliveInterval:   30 * time.Second,
				NotifyScript:        "/usr/local/bin/natmap-notify",
			}

			got := cfg.Args()
			want := []string{
				tc.wantAF,
				"-b", "16000",
				"-s", "stun.example.com:3478",
				"-h", "https://keepalive.example.com",
				"-k", "30",
				"-e", "/usr/local/bin/natmap-notify",
			}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Args() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestRunnerConfigArgsBuildsIPv6UDPAndWhitelistArguments(t *testing.T) {
	cfg := RunnerConfig{
		AddressFamily:       "ipv6",
		UDPMode:             true,
		BindPort:            16000,
		StunServer:          "stun.example.com:3478",
		HTTPKeepaliveServer: "https://keepalive.example.com",
		KeepaliveInterval:   30 * time.Second,
		NotifyScript:        "/usr/local/bin/natmap-notify",
		Interface:           "eth0",
		FWMark:              "0x66",
		UDPCheckCycle:       7,
	}

	got := cfg.Args()
	want := []string{
		"-6",
		"-u",
		"-b", "16000",
		"-s", "stun.example.com:3478",
		"-h", "https://keepalive.example.com",
		"-k", "30",
		"-e", "/usr/local/bin/natmap-notify",
		"-i", "eth0",
		"-f", "0x66",
		"-c", "7",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

type fakeProcessRunner struct {
	specs []process.Spec
	proc  *fakeProcess
	err   error
}

func (f *fakeProcessRunner) Start(ctx context.Context, spec process.Spec) (process.Process, error) {
	f.specs = append(f.specs, spec)
	if f.err != nil {
		return nil, f.err
	}
	if f.proc == nil {
		f.proc = newFakeProcess()
	}
	return f.proc, nil
}

type fakeProcess struct {
	done  chan error
	stops int
	pid   int
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{done: make(chan error, 1), pid: 1234}
}

func (f *fakeProcess) Done() <-chan error {
	return f.done
}

func (f *fakeProcess) Stop(ctx context.Context) error {
	f.stops++
	return nil
}

func (f *fakeProcess) PID() int {
	return f.pid
}

func TestGenerateNotifyTokenReturnsHexToken(t *testing.T) {
	token, err := GenerateNotifyToken()
	if err != nil {
		t.Fatalf("GenerateNotifyToken returned error: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("token length = %d, want 64", len(token))
	}
	for _, char := range token {
		if !strings.ContainsRune("0123456789abcdef", char) {
			t.Fatalf("token contains non-hex character %q", char)
		}
	}
}

func TestProcessRunnerStartBuildsNatmapSpec(t *testing.T) {
	proc := newFakeProcess()
	runner := &fakeProcessRunner{proc: proc}
	cfg := RunnerConfig{
		BinaryPath:          "/usr/local/bin/natmap",
		BindPort:            16000,
		StunServer:          "stun.example.com:3478",
		HTTPKeepaliveServer: "https://keepalive.example.com",
		KeepaliveInterval:   30 * time.Second,
		NotifyScript:        "/usr/local/bin/natmap-notify",
	}
	processRunner := &ProcessRunner{Config: cfg, Runner: runner}

	if err := processRunner.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if len(runner.specs) != 1 {
		t.Fatalf("Start calls = %d, want 1", len(runner.specs))
	}
	got := runner.specs[0]
	if got.Name != "natmap" {
		t.Fatalf("spec.Name = %q, want natmap", got.Name)
	}
	if got.Path != cfg.BinaryPath {
		t.Fatalf("spec.Path = %q, want %q", got.Path, cfg.BinaryPath)
	}
	if !reflect.DeepEqual(got.Args, cfg.Args()) {
		t.Fatalf("spec.Args = %#v, want %#v", got.Args, cfg.Args())
	}
	if processRunner.Done() != proc.Done() {
		t.Fatalf("Done did not return underlying process channel")
	}
}

func TestProcessRunnerStartAllowsStartedNatmapPID(t *testing.T) {
	proc := newFakeProcess()
	proc.pid = os.Getpid()
	runner := &fakeProcessRunner{proc: proc}
	listener := &Listener{token: "secret-token", allowed: make(map[int]string)}
	processRunner := &ProcessRunner{Runner: runner, Listener: listener}

	if err := processRunner.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if !listener.pidAllowed(os.Getpid()) {
		t.Fatal("started natmap PID was not allowed")
	}
}

func TestProcessRunnerStopRevokesStartedNatmapPID(t *testing.T) {
	proc := newFakeProcess()
	proc.pid = os.Getpid()
	runner := &fakeProcessRunner{proc: proc}
	listener := &Listener{token: "secret-token", allowed: make(map[int]string)}
	processRunner := &ProcessRunner{Runner: runner, Listener: listener}

	if err := processRunner.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := processRunner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if listener.pidAllowed(os.Getpid()) {
		t.Fatal("stopped natmap PID remained allowed")
	}
}

func TestListenerRejectsAllowedPIDWithChangedStartTime(t *testing.T) {
	listener := &Listener{token: "secret-token", allowed: map[int]string{os.Getpid(): "different-start-time"}}
	if listener.pidAllowed(os.Getpid()) {
		t.Fatal("pidAllowed accepted matching PID with different start time")
	}
}

func TestSendNotifyRejectsMissingNotifyToken(t *testing.T) {
	t.Setenv(NotifyTokenEnv, "")
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	listener, _, err := ListenNotifyWithToken(socketPath, "secret-token")
	if err != nil {
		t.Fatalf("ListenNotifyWithToken returned error: %v", err)
	}
	defer listener.Close()
	listener.AllowPID(os.Getpid())

	err = SendNotify(socketPath, validNotifyArgs())
	if err == nil || !strings.Contains(err.Error(), "natmap notify token 未配置") {
		t.Fatalf("SendNotify error = %v, want missing token", err)
	}
}

func TestProcessRunnerStartIncludesNotifyTokenEnv(t *testing.T) {
	runner := &fakeProcessRunner{proc: newFakeProcess()}
	cfg := RunnerConfig{
		BinaryPath:          "/usr/local/bin/natmap",
		BindPort:            16000,
		StunServer:          "stun.example.com:3478",
		HTTPKeepaliveServer: "https://keepalive.example.com",
		KeepaliveInterval:   30 * time.Second,
		NotifyScript:        "/usr/local/bin/natmap-notify",
		NotifyToken:         "secret-token",
	}
	processRunner := &ProcessRunner{Config: cfg, Runner: runner}

	if err := processRunner.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if len(runner.specs) != 1 {
		t.Fatalf("Start calls = %d, want 1", len(runner.specs))
	}
	want := NotifyTokenEnv + "=secret-token"
	if !containsString(runner.specs[0].Env, want) {
		t.Fatalf("spec.Env = %#v, want to contain %q", runner.specs[0].Env, want)
	}
}

func TestProcessRunnerStartOmitsNotifyTokenEnvWhenEmpty(t *testing.T) {
	runner := &fakeProcessRunner{proc: newFakeProcess()}
	cfg := RunnerConfig{
		BinaryPath:          "/usr/local/bin/natmap",
		BindPort:            16000,
		StunServer:          "stun.example.com:3478",
		HTTPKeepaliveServer: "https://keepalive.example.com",
		KeepaliveInterval:   30 * time.Second,
		NotifyScript:        "/usr/local/bin/natmap-notify",
	}
	processRunner := &ProcessRunner{Config: cfg, Runner: runner}

	if err := processRunner.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if len(runner.specs[0].Env) != 0 {
		t.Fatalf("spec.Env = %#v, want empty", runner.specs[0].Env)
	}
}

func TestProcessRunnerStopAndDoneAreNoOpWhenNotStarted(t *testing.T) {
	processRunner := &ProcessRunner{}

	if err := processRunner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if processRunner.Done() != nil {
		t.Fatal("Done channel = non-nil, want nil before start")
	}
}

func TestProcessRunnerStopStopsProcessAndClearsIt(t *testing.T) {
	proc := newFakeProcess()
	processRunner := &ProcessRunner{proc: proc}

	if err := processRunner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if proc.stops != 1 {
		t.Fatalf("process stops = %d, want 1", proc.stops)
	}

	if processRunner.Done() != nil {
		t.Fatal("Done channel = non-nil after Stop, want nil")
	}
}

func TestProcessRunnerLifecycleMethodsAreRaceSafe(t *testing.T) {
	processRunner := &ProcessRunner{proc: newFakeProcess()}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = processRunner.Stop(context.Background())
		}()
		go func() {
			defer wg.Done()
			_ = processRunner.Done()
		}()
	}
	wg.Wait()
}

func TestListenNotifyWithTokenReceivesMatchingTokenFromAllowedSender(t *testing.T) {
	t.Setenv(NotifyTokenEnv, "secret-token")
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	listener, events, err := ListenNotifyWithToken(socketPath, "secret-token")
	if err != nil {
		t.Fatalf("ListenNotifyWithToken returned error: %v", err)
	}
	defer listener.Close()
	listener.AllowPID(os.Getpid())

	if err := SendNotify(socketPath, validNotifyArgs()); err != nil {
		t.Fatalf("SendNotify returned error: %v", err)
	}

	select {
	case mapping := <-events:
		if mapping.PublicPort != 45678 {
			t.Fatalf("PublicPort = %d, want 45678", mapping.PublicPort)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for authenticated notify mapping")
	}
}

func TestListenNotifyWithTokenRejectsUnauthorizedPeerWithMatchingToken(t *testing.T) {
	t.Setenv(NotifyTokenEnv, "secret-token")
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	listener, events, err := ListenNotifyWithToken(socketPath, "secret-token")
	if err != nil {
		t.Fatalf("ListenNotifyWithToken returned error: %v", err)
	}
	defer listener.Close()

	logs := captureLogs(t)
	if err := SendNotify(socketPath, validNotifyArgs()); err != nil {
		t.Fatalf("SendNotify returned error: %v", err)
	}

	assertNoMapping(t, events)
	waitUntilStringContains(t, logs, "natmap notify 鉴权失败")
	assertLogDoesNotLeakNotifyContent(t, logs.String())
}

func TestListenNotifyWithTokenRejectsMissingTokenWithoutLeakingLogContent(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	listener, events, err := ListenNotifyWithToken(socketPath, "secret-token")
	if err != nil {
		t.Fatalf("ListenNotifyWithToken returned error: %v", err)
	}
	defer listener.Close()
	listener.AllowPID(os.Getpid())

	logs := captureLogs(t)
	if err := sendRawNotify(socketPath, `{"PublicAddress":"203.0.113.10","PublicPort":45678,"IP4P":"192.0.2.20","PrivatePort":16000,"Protocol":"TCP","PrivateAddress":"10.0.0.2"}`); err != nil {
		t.Fatalf("sendRawNotify returned error: %v", err)
	}

	assertNoMapping(t, events)
	waitUntilStringContains(t, logs, "natmap notify 鉴权失败")
	logText := logs.String()
	if !strings.Contains(logText, "natmap notify 鉴权失败") {
		t.Fatalf("log output = %q, want authentication failure", logText)
	}
	assertLogDoesNotLeakNotifyContent(t, logText)
}

func TestListenNotifyWithTokenRejectsWrongTokenWithoutLeakingLogContent(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	listener, events, err := ListenNotifyWithToken(socketPath, "secret-token")
	if err != nil {
		t.Fatalf("ListenNotifyWithToken returned error: %v", err)
	}
	defer listener.Close()
	listener.AllowPID(os.Getpid())

	logs := captureLogs(t)
	if err := sendRawNotify(socketPath, `{"token":"wrong-token","mapping":{"PublicAddress":"203.0.113.10","PublicPort":45678,"IP4P":"192.0.2.20","PrivatePort":16000,"Protocol":"TCP","PrivateAddress":"10.0.0.2"}}`); err != nil {
		t.Fatalf("sendRawNotify returned error: %v", err)
	}

	assertNoMapping(t, events)
	waitUntilStringContains(t, logs, "natmap notify 鉴权失败")
	logText := logs.String()
	if !strings.Contains(logText, "natmap notify 鉴权失败") {
		t.Fatalf("log output = %q, want authentication failure", logText)
	}
	assertLogDoesNotLeakNotifyContent(t, logText)
}

func TestListenNotifyReceivesMappingSentBySendNotify(t *testing.T) {
	t.Setenv(NotifyTokenEnv, "secret-token")
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	listener, events, err := ListenNotify(socketPath)
	if err != nil {
		t.Fatalf("ListenNotify returned error: %v", err)
	}
	defer listener.Close()
	listener.AllowPID(os.Getpid())

	err = SendNotify(socketPath, validNotifyArgs())
	if err != nil {
		t.Fatalf("SendNotify returned error: %v", err)
	}

	select {
	case mapping := <-events:
		if mapping.PublicPort != 45678 {
			t.Fatalf("PublicPort = %d, want 45678", mapping.PublicPort)
		}
		if mapping.PrivatePort != 16000 {
			t.Fatalf("PrivatePort = %d, want 16000", mapping.PrivatePort)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notify mapping")
	}
}

func validNotifyArgs() []string {
	return []string{
		"203.0.113.10",
		"45678",
		"192.0.2.20",
		"16000",
		"TCP",
		"10.0.0.2",
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func sendRawNotify(socketPath string, payload string) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(payload + "\n"))
	return err
}

func assertNoMapping(t *testing.T, events <-chan Mapping) {
	t.Helper()
	select {
	case mapping := <-events:
		t.Fatalf("received unexpected mapping: %+v", mapping)
	case <-time.After(100 * time.Millisecond):
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	})
	return buf
}

func waitUntilStringContains(t *testing.T, logs *lockedBuffer, want string) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if strings.Contains(logs.String(), want) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for log containing %q; logs=%q", want, logs.String())
		case <-tick.C:
		}
	}
}

func assertLogDoesNotLeakNotifyContent(t *testing.T, logText string) {
	t.Helper()
	for _, leaked := range []string{"secret-token", "wrong-token", "203.0.113.10", "45678", "16000", "10.0.0.2"} {
		if strings.Contains(logText, leaked) {
			t.Fatalf("log output leaked %q: %q", leaked, logText)
		}
	}
}
