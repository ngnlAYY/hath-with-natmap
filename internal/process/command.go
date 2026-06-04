package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"syscall"
)

type Spec struct {
	Name string
	Path string
	Args []string
	Env  []string
	Dir  string
}

type Process interface {
	Done() <-chan error
	Stop(ctx context.Context) error
	PID() int
}

type Runner interface {
	Start(ctx context.Context, spec Spec) (Process, error)
}

type OSRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

type osProcess struct {
	name    string
	cmd     *exec.Cmd
	done    chan error
	waited  chan struct{}
	waitErr error
	mu      sync.Mutex
}

func (r OSRunner) Start(ctx context.Context, spec Spec) (Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("启动进程 %q 失败: %w", spec.Name, err)
	}

	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.Stdout, cmd.Stderr = r.outputWriters(spec.Name)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动进程 %q 失败: %w", spec.Name, err)
	}

	done := make(chan error, 1)
	proc := &osProcess{
		name:   spec.Name,
		cmd:    cmd,
		done:   done,
		waited: make(chan struct{}),
	}
	go func() {
		err := cmd.Wait()
		proc.setWaitErr(err)
		done <- err
		close(done)
		close(proc.waited)
	}()

	return proc, nil
}

func (p *osProcess) Done() <-chan error {
	return p.done
}

func (p *osProcess) PID() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *osProcess) setWaitErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.waitErr = err
}

func (p *osProcess) getWaitErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

func (p *osProcess) Stop(ctx context.Context) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	if p.hasExited() {
		return p.waitError(p.getWaitErr(), false)
	}
	if err := p.signal(syscall.SIGTERM); err != nil {
		return err
	}
	select {
	case <-p.waited:
		return p.waitError(p.getWaitErr(), true)
	case <-ctx.Done():
	}

	if err := p.signal(syscall.SIGKILL); err != nil {
		return err
	}
	<-p.waited
	err := p.getWaitErr()
	if err == nil {
		return nil
	}
	if !isSignalExit(err) {
		return p.waitError(err, true)
	}
	return ctx.Err()
}

func (p *osProcess) signal(sig syscall.Signal) error {
	pid := p.cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	if err == nil {
		if err := syscall.Kill(-pgid, sig); err != nil && !isProcessDone(err) {
			return fmt.Errorf("发送信号 %s 到进程 %q 失败: %w", sig, p.name, err)
		}
		return nil
	}
	if !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("获取进程 %q 的进程组失败: %w", p.name, err)
	}
	if err := p.cmd.Process.Signal(sig); err != nil && !isProcessDone(err) {
		return fmt.Errorf("发送信号 %s 到进程 %q 失败: %w", sig, p.name, err)
	}
	return nil
}

func (p *osProcess) hasExited() bool {
	select {
	case <-p.waited:
		return true
	default:
		return false
	}
}

func (p *osProcess) waitError(err error, afterStop bool) error {
	if err == nil {
		return nil
	}
	if afterStop && isSignalExit(err) {
		return nil
	}
	return fmt.Errorf("等待进程 %q 退出失败: %w", p.name, err)
}

func isSignalExit(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled()
}

func isProcessDone(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

func (r OSRunner) stdout() io.Writer {
	if r.Stdout != nil {
		return r.Stdout
	}
	return os.Stdout
}

func (r OSRunner) stderr() io.Writer {
	if r.Stderr != nil {
		return r.Stderr
	}
	return os.Stderr
}

func (r OSRunner) outputWriters(name string) (io.Writer, io.Writer) {
	stdout := r.stdout()
	stderr := r.stderr()
	if sameWriter(stdout, stderr) {
		mu := &sync.Mutex{}
		return r.outputWriter(name, stdout, mu), r.outputWriter(name, stderr, mu)
	}
	return r.outputWriter(name, stdout, &sync.Mutex{}), r.outputWriter(name, stderr, &sync.Mutex{})
}

func sameWriter(left io.Writer, right io.Writer) bool {
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if !leftValue.IsValid() || !rightValue.IsValid() {
		return !leftValue.IsValid() && !rightValue.IsValid()
	}
	if leftValue.Type() != rightValue.Type() || !leftValue.Type().Comparable() {
		return false
	}
	return left == right
}

func (r OSRunner) outputWriter(name string, writer io.Writer, mu *sync.Mutex) io.Writer {
	if name == "" {
		return writer
	}
	return &linePrefixWriter{prefix: []byte("[" + name + "] "), writer: writer, mu: mu, atLineStart: true}
}

type linePrefixWriter struct {
	prefix      []byte
	writer      io.Writer
	mu          *sync.Mutex
	atLineStart bool
}

func (w *linePrefixWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	written := 0
	for len(data) > 0 {
		if w.atLineStart {
			if _, err := writeFull(w.writer, w.prefix); err != nil {
				return written, err
			}
			w.atLineStart = false
		}

		lineEnd := bytes.IndexByte(data, '\n')
		if lineEnd == -1 {
			n, err := writeFull(w.writer, data)
			written += n
			return written, err
		}

		line := data[:lineEnd+1]
		n, err := writeFull(w.writer, line)
		written += n
		if err != nil {
			return written, err
		}
		data = data[lineEnd+1:]
		w.atLineStart = true
	}
	return written, nil
}

func writeFull(writer io.Writer, data []byte) (int, error) {
	written := 0
	for written < len(data) {
		n, err := writer.Write(data[written:])
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}
