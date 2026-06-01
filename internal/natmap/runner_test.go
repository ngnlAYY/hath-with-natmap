package natmap

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/process"
)

func TestRunnerConfigArgsBuildsBindModeArguments(t *testing.T) {
	cfg := RunnerConfig{
		BindPort:            16000,
		StunServer:          "stun.example.com:3478",
		HTTPKeepaliveServer: "https://keepalive.example.com",
		KeepaliveInterval:   30 * time.Second,
		NotifyScript:        "/usr/local/bin/natmap-notify",
	}

	got := cfg.Args()
	want := []string{
		"-4",
		"-b", "16000",
		"-s", "stun.example.com:3478",
		"-h", "https://keepalive.example.com",
		"-k", "30",
		"-e", "/usr/local/bin/natmap-notify",
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

func TestProcessRunnerStopAndDoneAreNoOpWhenNotStarted(t *testing.T) {
	processRunner := &ProcessRunner{}

	if err := processRunner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	select {
	case _, ok := <-processRunner.Done():
		if ok {
			t.Fatal("Done channel open, want closed")
		}
	case <-time.After(time.Second):
		t.Fatal("Done channel did not close")
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

	select {
	case _, ok := <-processRunner.Done():
		if ok {
			t.Fatal("Done channel open after Stop, want closed nil-process channel")
		}
	case <-time.After(time.Second):
		t.Fatal("Done channel did not close after Stop")
	}
}

func TestListenNotifyReceivesMappingSentBySendNotify(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	listener, events, err := ListenNotify(socketPath)
	if err != nil {
		t.Fatalf("ListenNotify returned error: %v", err)
	}
	defer listener.Close()

	err = SendNotify(socketPath, []string{
		"203.0.113.10",
		"45678",
		"192.0.2.20",
		"16000",
		"TCP",
		"10.0.0.2",
	})
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
