package natmap

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/process"
)

type RunnerConfig struct {
	BinaryPath          string
	AddressFamily       string
	UDPMode             bool
	BindPort            int
	StunServer          string
	HTTPKeepaliveServer string
	KeepaliveInterval   time.Duration
	NotifyScript        string
	Interface           string
	FWMark              string
	UDPCheckCycle       int
	NotifyToken         string
}

func (c RunnerConfig) Args() []string {
	args := []string{"-4"}
	if c.AddressFamily == "ipv6" {
		args[0] = "-6"
	}
	if c.UDPMode {
		args = append(args, "-u")
	}
	args = append(args,
		"-b", strconv.Itoa(c.BindPort),
		"-s", c.StunServer,
		"-h", c.HTTPKeepaliveServer,
		"-k", strconv.FormatInt(int64(c.KeepaliveInterval/time.Second), 10),
		"-e", c.NotifyScript,
	)
	if c.Interface != "" {
		args = append(args, "-i", c.Interface)
	}
	if c.FWMark != "" {
		args = append(args, "-f", c.FWMark)
	}
	if c.UDPCheckCycle > 0 {
		args = append(args, "-c", strconv.Itoa(c.UDPCheckCycle))
	}
	return args
}

type ProcessRunner struct {
	Config   RunnerConfig
	Runner   process.Runner
	Listener *Listener
	mu       sync.Mutex
	proc     process.Process
	pid      int
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
	pid := proc.PID()
	if r.Listener != nil {
		r.Listener.AllowPID(pid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.proc = proc
	r.pid = pid
	return nil
}

func (r *ProcessRunner) Stop(ctx context.Context) error {
	r.mu.Lock()
	proc := r.proc
	pid := r.pid
	r.proc = nil
	r.pid = 0
	r.mu.Unlock()
	if r.Listener != nil {
		r.Listener.RevokePID(pid)
	}
	if proc == nil {
		return nil
	}
	return proc.Stop(ctx)
}

func (r *ProcessRunner) Done() <-chan error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.proc == nil {
		return nil
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
