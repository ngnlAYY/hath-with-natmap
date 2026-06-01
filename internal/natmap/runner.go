package natmap

import (
	"context"
	"strconv"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/process"
)

type RunnerConfig struct {
	BinaryPath          string
	BindPort            int
	StunServer          string
	HTTPKeepaliveServer string
	KeepaliveInterval   time.Duration
	NotifyScript        string
	NotifyToken         string
}

func (c RunnerConfig) Args() []string {
	return []string{
		"-4",
		"-b", strconv.Itoa(c.BindPort),
		"-s", c.StunServer,
		"-h", c.HTTPKeepaliveServer,
		"-k", strconv.FormatInt(int64(c.KeepaliveInterval/time.Second), 10),
		"-e", c.NotifyScript,
	}
}

type ProcessRunner struct {
	Config RunnerConfig
	Runner process.Runner
	proc   process.Process
}

func (r *ProcessRunner) Start(ctx context.Context) error {
	runner := r.runner()
	proc, err := runner.Start(ctx, process.Spec{
		Name: "natmap",
		Path: r.Config.BinaryPath,
		Args: r.Config.Args(),
		Env:  r.Config.env(),
	})
	if err != nil {
		return err
	}
	r.proc = proc
	return nil
}

func (r *ProcessRunner) Stop(ctx context.Context) error {
	if r.proc == nil {
		return nil
	}
	proc := r.proc
	r.proc = nil
	return proc.Stop(ctx)
}

func (r *ProcessRunner) Done() <-chan error {
	if r.proc == nil {
		ch := make(chan error)
		close(ch)
		return ch
	}
	return r.proc.Done()
}

func (c RunnerConfig) env() []string {
	if c.NotifyToken == "" {
		return nil
	}
	return []string{NotifyTokenEnv + "=" + c.NotifyToken}
}

func (r *ProcessRunner) runner() process.Runner {
	if r.Runner != nil {
		return r.Runner
	}
	return process.OSRunner{}
}
