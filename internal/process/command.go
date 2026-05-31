package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
}

type Runner interface {
	Start(ctx context.Context, spec Spec) (Process, error)
}

type OSRunner struct{}

type osProcess struct {
	name    string
	cmd     *exec.Cmd
	done    chan error
	waited  chan struct{}
	waitErr error
	mu      sync.Mutex
}

func (OSRunner) Start(ctx context.Context, spec Spec) (Process, error) {
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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
	}
	return p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited()
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
