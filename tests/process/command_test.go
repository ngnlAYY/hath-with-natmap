package process_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	process "github.com/ngnlAYY/hath-with-natter/internal/process"
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

	runner := process.OSRunner{}
	proc, err := runner.Start(context.Background(), process.Spec{
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

	assertDoneClosed(t, proc.Done())
}

func TestOSRunnerContextCancelDoesNotKillProcessBeforeStop(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "ready")
	if os.Getenv("HATH_PROCESS_CONTEXT_HELPER") == "1" {
		ignoreTerminationSignals()
		if err := os.WriteFile(os.Getenv("HATH_PROCESS_READY_FILE"), []byte("ready"), 0o600); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "write ready file: %v\n", err)
			os.Exit(2)
		}
		select {}
	}

	runner := process.OSRunner{}
	startCtx, cancelStart := context.WithCancel(context.Background())
	proc, err := runner.Start(startCtx, process.Spec{
		Name: "context-helper",
		Path: os.Args[0],
		Args: []string{"-test.run=TestOSRunnerContextCancelDoesNotKillProcessBeforeStop"},
		Env: []string{
			"HATH_PROCESS_CONTEXT_HELPER=1",
			"HATH_PROCESS_READY_FILE=" + readyPath,
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
		defer cleanupCancel()
		_ = proc.Stop(cleanupCtx)
	})
	waitForHelperReady(t, readyPath)

	cancelStart()

	select {
	case <-proc.Done():
		t.Fatal("process exited after start context cancellation before Stop()")
	case <-time.After(100 * time.Millisecond):
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelStop()
	err = proc.Stop(stopCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v, want context deadline exceeded", err)
	}
	assertDoneClosed(t, proc.Done())
}

func TestOSRunnerStartMissingPathIncludesProcessName(t *testing.T) {
	runner := process.OSRunner{}
	_, err := runner.Start(context.Background(), process.Spec{
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

func TestOSRunnerPrefixesProcessOutput(t *testing.T) {
	if os.Getenv("HATH_PROCESS_OUTPUT_HELPER") == "1" {
		_, _ = fmt.Fprintln(os.Stdout, "stdout line")
		_, _ = fmt.Fprintln(os.Stderr, "stderr line")
		os.Exit(0)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := process.OSRunner{
		Stdout: &stdout,
		Stderr: &stderr,
	}
	proc, err := runner.Start(context.Background(), process.Spec{
		Name: "natmap",
		Path: os.Args[0],
		Args: []string{"-test.run=TestOSRunnerPrefixesProcessOutput"},
		Env:  []string{"HATH_PROCESS_OUTPUT_HELPER=1"},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	assertDoneClosed(t, proc.Done())

	if got := stdout.String(); got != "[natmap] stdout line\n" {
		t.Fatalf("stdout = %q, want prefixed line", got)
	}
	if got := stderr.String(); got != "[natmap] stderr line\n" {
		t.Fatalf("stderr = %q, want prefixed line", got)
	}
}

func TestOSProcessStopReturnsContextErrorAfterKillOnTimeout(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "ready")
	if os.Getenv("HATH_PROCESS_IGNORE_TERM_HELPER") == "1" {
		ignoreTerminationSignals()
		if err := os.WriteFile(os.Getenv("HATH_PROCESS_READY_FILE"), []byte("ready"), 0o600); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "write ready file: %v\n", err)
			os.Exit(2)
		}
		// Wait until Stop escalates from SIGTERM to SIGKILL.
		select {}
	}

	runner := process.OSRunner{}
	proc, err := runner.Start(context.Background(), process.Spec{
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

func assertDoneClosed(t *testing.T, done <-chan error) {
	t.Helper()
	for {
		select {
		case _, ok := <-done:
			if !ok {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("process did not exit")
		}
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
