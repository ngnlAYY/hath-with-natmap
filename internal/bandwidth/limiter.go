package bandwidth

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

const capNETAdmin = 12

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		return fmt.Errorf("执行命令失败: %s %v: %w", name, args, err)
	}
	return nil
}

func CheckNETAdmin() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("tc 上传限速仅支持 Linux")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return fmt.Errorf("读取进程 capability 失败: %w", err)
	}
	return checkNETAdminStatus(status)
}

func checkNETAdminStatus(status []byte) error {
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "CapBnd:") {
			fields := strings.Fields(line)
			if len(fields) != 2 {
				return fmt.Errorf("解析进程 capability 失败")
			}
			capabilityMask, err := strconv.ParseUint(fields[1], 16, 64)
			if err != nil {
				return fmt.Errorf("解析进程 capability 失败: %w", err)
			}
			if capabilityMask&(1<<capNETAdmin) == 0 {
				return fmt.Errorf("缺少 NET_ADMIN capability bounding set，无法配置 tc 上传限速")
			}
			return nil
		}
	}
	return fmt.Errorf("进程 capability 信息缺少 CapBnd")
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
