package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

	assertDoneClosed(t, proc.Done())
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

func TestOSProcessStopAlreadyExitedReturnsNilWithExpiredContext(t *testing.T) {
	proc := &osProcess{
		name:   "already-exited",
		cmd:    startExitedTestCommand(t),
		done:   delayedDone(nil, 20*time.Millisecond),
		waited: closedWaited(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := proc.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v, want nil", err)
	}
}

func TestOSProcessStopReturnsUnexpectedWaitError(t *testing.T) {
	waitErr := fmt.Errorf("wait failed")
	proc := &osProcess{
		name:    "bad-wait",
		cmd:     startExitedTestCommand(t),
		done:    closedDone(waitErr),
		waited:  closedWaited(),
		waitErr: waitErr,
	}

	err := proc.Stop(context.Background())
	if !errors.Is(err, waitErr) {
		t.Fatalf("Stop() error = %v, want wrapped wait error", err)
	}
	if !strings.Contains(err.Error(), "bad-wait") {
		t.Fatalf("Stop() error = %q, want process name", err.Error())
	}
}

func TestProcessExitHelper(t *testing.T) {
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

func startExitedTestCommand(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestProcessExitHelper")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	return cmd
}

func closedDone(err error) chan error {
	done := make(chan error, 1)
	done <- err
	close(done)
	return done
}

func delayedDone(err error, delay time.Duration) chan error {
	done := make(chan error, 1)
	go func() {
		time.Sleep(delay)
		done <- err
		close(done)
	}()
	return done
}

func closedWaited() chan struct{} {
	waited := make(chan struct{})
	close(waited)
	return waited
}

func assertDoneClosed(t *testing.T, done <-chan error) {
	t.Helper()
	for {
		select {
		case _, ok := <-done:
			if !ok {
				return
			}
		case <-time.After(time.Second):
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
