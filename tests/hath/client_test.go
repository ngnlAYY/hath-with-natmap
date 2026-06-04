package hath_test

import (
	"context"
	hath "github.com/ngnlAYY/hath-with-natter/internal/hath"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ngnlAYY/hath-with-natter/internal/process"
)

func TestBuildArgs(t *testing.T) {
	cfg := hath.Config{
		BinaryPath:          "/usr/local/bin/hath-rust",
		DataDir:             "/data/hath",
		LogLevel:            "warn",
		ForceBackgroundScan: true,
		RPCServerIP:         "127.0.0.1",
		ProxyURL:            "http://127.0.0.1:8080",
		UseProxy:            true,
	}
	got := cfg.Args(4567)
	want := []string{
		"--cache-dir", "/data/hath/cache",
		"--data-dir", "/data/hath/data",
		"--download-dir", "/data/hath/download",
		"--log-dir", "/data/hath/log",
		"--temp-dir", "/data/hath/tmp",
		"--port", "4567",
		"--proxy", "http://127.0.0.1:8080",
		"--force-background-scan",
		"-qq",
		"--rpc-server-ip", "127.0.0.1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

func TestArgsOmitsOptionalFlagsWhenDisabled(t *testing.T) {
	cfg := hath.Config{
		DataDir:  "/data/hath",
		LogLevel: "unknown",
		ProxyURL: "http://127.0.0.1:8080",
	}
	got := cfg.Args(8080)
	want := []string{
		"--cache-dir", "/data/hath/cache",
		"--data-dir", "/data/hath/data",
		"--download-dir", "/data/hath/download",
		"--log-dir", "/data/hath/log",
		"--temp-dir", "/data/hath/tmp",
		"--port", "8080",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

func TestArgsOmitsProxyWhenProxyURLEmpty(t *testing.T) {
	cfg := hath.Config{
		DataDir:  "/data/hath",
		UseProxy: true,
		ProxyURL: "",
		LogLevel: "debug",
	}
	got := cfg.Args(8080)
	want := []string{
		"--cache-dir", "/data/hath/cache",
		"--data-dir", "/data/hath/data",
		"--download-dir", "/data/hath/download",
		"--log-dir", "/data/hath/log",
		"--temp-dir", "/data/hath/tmp",
		"--port", "8080",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

func TestArgsAppendsWhitelistedFlagsAfterExistingOptions(t *testing.T) {
	cfg := hath.Config{
		DataDir:              "/data/hath",
		LogLevel:             "warn",
		ForceBackgroundScan:  true,
		RPCServerIP:          "127.0.0.1",
		ProxyURL:             "http://127.0.0.1:8080",
		UseProxy:             true,
		DisableLogging:       true,
		FlushLog:             true,
		MaxConnection:        128,
		DisableIPOriginCheck: true,
		DisableFloodControl:  true,
		EnableMetrics:        true,
		DisableServerHeader:  true,
		EnableH3:             true,
	}
	got := cfg.Args(4567)
	want := []string{
		"--cache-dir", "/data/hath/cache",
		"--data-dir", "/data/hath/data",
		"--download-dir", "/data/hath/download",
		"--log-dir", "/data/hath/log",
		"--temp-dir", "/data/hath/tmp",
		"--port", "4567",
		"--proxy", "http://127.0.0.1:8080",
		"--force-background-scan",
		"-qq",
		"--rpc-server-ip", "127.0.0.1",
		"--disable-logging",
		"--flush-log",
		"--max-connection", "128",
		"--disable-ip-origin-check",
		"--disable-flood-control",
		"--enable-metrics",
		"--disable-server-header",
		"--enable-h3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

func TestArgsOmitsMaxConnectionWhenZero(t *testing.T) {
	cfg := hath.Config{
		DataDir:       "/data/hath",
		FlushLog:      true,
		MaxConnection: 0,
	}
	got := cfg.Args(8080)
	want := []string{
		"--cache-dir", "/data/hath/cache",
		"--data-dir", "/data/hath/data",
		"--download-dir", "/data/hath/download",
		"--log-dir", "/data/hath/log",
		"--temp-dir", "/data/hath/tmp",
		"--port", "8080",
		"--flush-log",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

func TestArgsQuietFlagMapping(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		want     []string
	}{
		{name: "debug", logLevel: "debug"},
		{name: "info", logLevel: "info", want: []string{"-q"}},
		{name: "warn", logLevel: "warn", want: []string{"-qq"}},
		{name: "error", logLevel: "error", want: []string{"-qqq"}},
		{name: "off", logLevel: "off", want: []string{"-qqqq"}},
		{name: "unknown", logLevel: "trace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := hath.Config{DataDir: "/data/hath", LogLevel: tt.logLevel}
			got := cfg.Args(1)
			quietFlags := quietFlagsFromArgs(got)
			if !reflect.DeepEqual(quietFlags, tt.want) {
				t.Fatalf("quiet flags = %#v, want %#v in args %#v", quietFlags, tt.want, got)
			}
		})
	}
}

func TestWriteClientLogin(t *testing.T) {
	dir := t.TempDir()
	cfg := hath.Config{DataDir: dir, ClientID: "12345", ClientKey: "secret"}
	if err := cfg.WriteClientLogin(); err != nil {
		t.Fatalf("WriteClientLogin() error = %v", err)
	}
	path := filepath.Join(dir, "data", "client_login")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 client_login 失败: %v", err)
	}
	if string(content) != "12345-secret" {
		t.Fatalf("client_login = %q", string(content))
	}
	dataInfo, err := os.Stat(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("读取 data 目录权限失败: %v", err)
	}
	if got := dataInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("data directory permissions = %v, want %v", got, os.FileMode(0o700))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取 client_login 权限失败: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("client_login permissions = %v, want %v", got, os.FileMode(0o600))
	}
}

func TestWriteClientLoginRejectsEmptyClientID(t *testing.T) {
	cfg := hath.Config{DataDir: t.TempDir(), ClientKey: "secret"}
	if err := cfg.WriteClientLogin(); err == nil || !strings.Contains(err.Error(), "ClientID 不能为空") {
		t.Fatalf("WriteClientLogin() error = %v, want ClientID 不能为空", err)
	}
}

func TestWriteClientLoginRejectsEmptyClientKey(t *testing.T) {
	cfg := hath.Config{DataDir: t.TempDir(), ClientID: "12345"}
	if err := cfg.WriteClientLogin(); err == nil || !strings.Contains(err.Error(), "ClientKey 不能为空") {
		t.Fatalf("WriteClientLogin() error = %v, want ClientKey 不能为空", err)
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
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{done: make(chan error, 1)}
}

func (f *fakeProcess) Done() <-chan error {
	return f.done
}

func (f *fakeProcess) Stop(ctx context.Context) error {
	f.stops++
	return nil
}

func (f *fakeProcess) PID() int {
	return 1234
}

func TestControllerStartWritesLoginAndStartsWithPrivatePort(t *testing.T) {
	dir := t.TempDir()
	proc := newFakeProcess()
	runner := &fakeProcessRunner{proc: proc}
	cfg := hath.Config{
		BinaryPath: "/usr/local/bin/hath-rust",
		DataDir:    dir,
		LogLevel:   "info",
		ClientID:   "12345",
		ClientKey:  "secret",
	}
	controller := &hath.Controller{Config: cfg, Runner: runner}

	if err := controller.Start(context.Background(), 16000); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	login, err := os.ReadFile(filepath.Join(dir, "data", "client_login"))
	if err != nil {
		t.Fatalf("client_login was not written before start: %v", err)
	}
	if string(login) != "12345-secret" {
		t.Fatalf("client_login = %q, want 12345-secret", string(login))
	}
	if len(runner.specs) != 1 {
		t.Fatalf("Start calls = %d, want 1", len(runner.specs))
	}
	got := runner.specs[0]
	if got.Name != "hath-rust" {
		t.Fatalf("spec.Name = %q, want hath-rust", got.Name)
	}
	if got.Path != cfg.BinaryPath {
		t.Fatalf("spec.Path = %q, want %q", got.Path, cfg.BinaryPath)
	}
	if !reflect.DeepEqual(got.Args, cfg.Args(16000)) {
		t.Fatalf("spec.Args = %#v, want %#v", got.Args, cfg.Args(16000))
	}
	if !controller.Running() {
		t.Fatal("Running = false, want true")
	}
}

func TestControllerRunningClearsExitedProcess(t *testing.T) {
	controller, proc := startController(t)
	proc.done <- nil
	close(proc.done)

	if controller.Running() {
		t.Fatal("Running = true after process Done, want false")
	}
	if err := controller.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if proc.stops != 0 {
		t.Fatalf("process stops = %d after Running() cleared exited process, want 0", proc.stops)
	}
}

func TestControllerStopStopsProcessAndClearsIt(t *testing.T) {
	controller, proc := startController(t)

	if err := controller.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if proc.stops != 1 {
		t.Fatalf("process stops = %d, want 1", proc.stops)
	}
	if controller.Running() {
		t.Fatal("Running = true after Stop, want false")
	}
	if err := controller.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop returned error: %v", err)
	}
	if proc.stops != 1 {
		t.Fatalf("process stops after second Stop = %d, want 1", proc.stops)
	}
}

func TestControllerStopNoOpsWhenNotStarted(t *testing.T) {
	controller := &hath.Controller{}

	if err := controller.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
}

func TestControllerLifecycleMethodsAreRaceSafe(t *testing.T) {
	controller, _ := startController(t)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = controller.Stop(context.Background())
		}()
		go func() {
			defer wg.Done()
			_ = controller.Running()
		}()
	}
	wg.Wait()
}

func startController(t *testing.T) (*hath.Controller, *fakeProcess) {
	t.Helper()

	dir := t.TempDir()
	proc := newFakeProcess()
	runner := &fakeProcessRunner{proc: proc}
	controller := &hath.Controller{
		Config: hath.Config{
			BinaryPath: "/usr/local/bin/hath-rust",
			DataDir:    dir,
			ClientID:   "12345",
			ClientKey:  "secret",
		},
		Runner: runner,
	}
	if err := controller.Start(context.Background(), 16000); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	return controller, proc
}

func quietFlagsFromArgs(args []string) []string {
	var flags []string
	for _, arg := range args {
		switch arg {
		case "-q", "-qq", "-qqq", "-qqqq":
			flags = append(flags, arg)
		}
	}
	return flags
}
