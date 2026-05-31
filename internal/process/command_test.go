package process

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOSRunnerStartAndStop(t *testing.T) {
	if os.Getenv("HATH_PROCESS_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		return
	}

	runner := OSRunner{}
	proc, err := runner.Start(context.Background(), Spec{
		Name: "helper",
		Path: os.Args[0],
		Args: []string{"-test.run=TestOSRunnerStartAndStop"},
		Env:  []string{"HATH_PROCESS_HELPER=1"},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := proc.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	select {
	case _, ok := <-proc.Done():
		if ok {
			t.Fatal("Done() channel should be closed after process exits")
		}
	case <-time.After(time.Second):
		t.Fatal("process did not exit")
	}
}

func TestOSRunnerStartMissingPathIncludesProcessName(t *testing.T) {
	runner := OSRunner{}
	_, err := runner.Start(context.Background(), Spec{
		Name: "missing-helper",
		Path: "/path/to/missing/hath-process-helper",
	})
	if err == nil {
		t.Fatal("Start() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing-helper") {
		t.Fatalf("Start() error = %q, want process name", err.Error())
	}
}

func TestOSProcessStopReturnsContextErrorAfterKillOnTimeout(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "ready")
	if os.Getenv("HATH_PROCESS_IGNORE_TERM_HELPER") == "1" {
		ignoreTerminationSignals()
		if err := os.WriteFile(os.Getenv("HATH_PROCESS_READY_FILE"), []byte("ready"), 0o600); err != nil {
			panic(err)
		}
		select {}
	}

	runner := OSRunner{}
	proc, err := runner.Start(context.Background(), Spec{
		Name: "ignore-term-helper",
		Path: os.Args[0],
		Args: []string{"-test.run=TestOSProcessStopReturnsContextErrorAfterKillOnTimeout"},
		Env: []string{
			"HATH_PROCESS_IGNORE_TERM_HELPER=1",
			"HATH_PROCESS_READY_FILE=" + readyPath,
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForHelperReady(t, readyPath)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = proc.Stop(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v, want context deadline exceeded", err)
	}

	select {
	case <-proc.Done():
	case <-time.After(time.Second):
		t.Fatal("process did not exit after timeout kill")
	}
}

func waitForHelperReady(t *testing.T, readyPath string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyPath); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("helper did not become ready")
}

func ignoreTerminationSignals() {
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	go func() {
		for range term {
		}
	}()
}
