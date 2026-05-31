package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	cmd  *exec.Cmd
	done chan error
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
	go func() {
		done <- cmd.Wait()
		close(done)
	}()

	return &osProcess{
		cmd:  cmd,
		done: done,
	}, nil
}

func (p *osProcess) Done() <-chan error {
	return p.done
}

func (p *osProcess) Stop(ctx context.Context) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	p.signal(syscall.SIGTERM)
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
	}

	p.signal(syscall.SIGKILL)
	<-p.done
	return ctx.Err()
}

func (p *osProcess) signal(sig syscall.Signal) {
	pid := p.cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	if err == nil {
		_ = syscall.Kill(-pgid, sig)
		return
	}
	_ = p.cmd.Process.Signal(sig)
}
