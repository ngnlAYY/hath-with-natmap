package bandwidth

import (
	"context"
	"fmt"
	"os/exec"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("执行命令失败: %s %v, 输出: %s: %w", name, args, string(output), err)
	}
	return nil
}

type Limiter struct {
	Runner      CommandRunner
	Interface   string
	UploadLimit string
}

func (l Limiter) Apply(ctx context.Context, port int) error {
	runner := l.runner()
	commands := [][]string{
		{"qdisc", "replace", "dev", l.Interface, "root", "handle", "1:", "htb", "default", "30"},
		{"class", "replace", "dev", l.Interface, "parent", "1:", "classid", "1:30", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"class", "replace", "dev", l.Interface, "parent", "1:", "classid", "1:10", "htb", "rate", l.UploadLimit, "ceil", l.UploadLimit},
		{"filter", "replace", "dev", l.Interface, "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", fmt.Sprintf("%d", port), "0xffff", "flowid", "1:10"},
	}

	for _, args := range commands {
		if err := runner.Run(ctx, "tc", args...); err != nil {
			return fmt.Errorf("配置上传限速失败: %w", err)
		}
	}
	return nil
}

func (l Limiter) Clear(ctx context.Context) error {
	if err := l.runner().Run(ctx, "tc", "qdisc", "del", "dev", l.Interface, "root"); err != nil {
		return fmt.Errorf("清理上传限速规则失败: %w", err)
	}
	return nil
}

func (l Limiter) runner() CommandRunner {
	if l.Runner != nil {
		return l.Runner
	}
	return ExecRunner{}
}
