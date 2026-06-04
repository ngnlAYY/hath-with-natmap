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

type OutputRunner interface {
	CommandRunner
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

const (
	capNETAdmin              = 12
	projectRootQdisc         = "qdisc htb 1:"
	projectDefaultClassMinor = "3fed"
	projectDefaultClassID    = "1:" + projectDefaultClassMinor
	noqueueRootQdisc         = "qdisc noqueue"
)

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		return fmt.Errorf("执行命令失败: %s %v: %w", name, args, err)
	}
	return nil
}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("执行命令失败: %s %v: %w", name, args, err)
	}
	return output, nil
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

type rootQdiscState int

const (
	rootQdiscUnknown rootQdiscState = iota
	rootQdiscSafe
	rootQdiscOwned
	rootQdiscForeign
)

type Limiter struct {
	Runner                CommandRunner
	Interface             string
	UploadLimit           string
	AllowReplaceRootQdisc bool
	applied               bool
}

func (l *Limiter) Apply(ctx context.Context, port int) error {
	runner := l.runner()
	if err := l.ensureCanReplaceRootQdisc(ctx, runner); err != nil {
		return fmt.Errorf("配置上传限速失败: %w", err)
	}
	commands := [][]string{
		{"qdisc", "replace", "dev", l.Interface, "root", "handle", "1:", "htb", "default", projectDefaultClassMinor},
		{"class", "replace", "dev", l.Interface, "parent", "1:", "classid", projectDefaultClassID, "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"class", "replace", "dev", l.Interface, "parent", "1:", "classid", "1:10", "htb", "rate", l.UploadLimit, "ceil", l.UploadLimit},
		{"filter", "replace", "dev", l.Interface, "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", fmt.Sprintf("%d", port), "0xffff", "flowid", "1:10"},
	}

	for i, args := range commands {
		if err := runner.Run(ctx, "tc", args...); err != nil {
			return fmt.Errorf("配置上传限速失败: %w", err)
		}
		if i == 0 {
			l.applied = true
		}
	}
	return nil
}

func (l *Limiter) Clear(ctx context.Context) error {
	if !l.applied {
		return nil
	}
	runner := l.runner()
	state, _, err := l.inspectRootQdisc(ctx, runner)
	if err != nil {
		return err
	}
	if state != rootQdiscOwned {
		l.applied = false
		return nil
	}
	if err := runner.Run(ctx, "tc", "qdisc", "del", "dev", l.Interface, "root"); err != nil {
		return fmt.Errorf("清理上传限速规则失败: %w", err)
	}
	l.applied = false
	return nil
}

func (l Limiter) ensureCanReplaceRootQdisc(ctx context.Context, runner CommandRunner) error {
	state, output, err := l.inspectRootQdisc(ctx, runner)
	if err != nil {
		return err
	}
	switch state {
	case rootQdiscSafe, rootQdiscOwned:
		return nil
	case rootQdiscUnknown:
		if l.AllowReplaceRootQdisc {
			return nil
		}
		return fmt.Errorf("当前 runner 不支持检查 tc root qdisc，拒绝覆盖；如确认要继续，请在配置中设置 bandwidth.allow_replace_root_qdisc: true")
	case rootQdiscForeign:
		if l.AllowReplaceRootQdisc {
			return nil
		}
		return fmt.Errorf("检测到网卡 %s 已存在其他 root qdisc，拒绝覆盖；如确认要继续，请在配置中设置 bandwidth.allow_replace_root_qdisc: true；当前 qdisc: %s", l.Interface, singleLine(output))
	default:
		return fmt.Errorf("未知的 root qdisc 检查结果")
	}
}

func (l Limiter) inspectRootQdisc(ctx context.Context, runner CommandRunner) (rootQdiscState, string, error) {
	outputRunner, ok := runner.(OutputRunner)
	if !ok {
		return rootQdiscUnknown, "", nil
	}
	output, err := outputRunner.Output(ctx, "tc", "qdisc", "show", "dev", l.Interface)
	if err != nil {
		return rootQdiscUnknown, "", fmt.Errorf("检查 root qdisc 失败: %w", err)
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return rootQdiscSafe, text, nil
	}
	rootLine := rootQdiscLine(text)
	if rootLine == "" {
		return rootQdiscSafe, text, nil
	}
	switch {
	case strings.HasPrefix(rootLine, noqueueRootQdisc):
		return rootQdiscSafe, text, nil
	case strings.HasPrefix(rootLine, projectRootQdisc):
		if l.applied && hasHTBDefaultClass(rootLine, projectDefaultClassMinor) {
			return rootQdiscOwned, text, nil
		}
		return rootQdiscForeign, text, nil
	default:
		return rootQdiscForeign, text, nil
	}
}

func rootQdiscLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, field := range strings.Fields(line) {
			if field == "root" {
				return line
			}
		}
	}
	return ""
}

func hasHTBDefaultClass(rootLine, classMinor string) bool {
	fields := strings.Fields(rootLine)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "default" && fields[i+1] == classMinor {
			return true
		}
	}
	return false
}

func singleLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "<empty>"
	}
	return strings.Join(strings.Fields(text), " ")
}

func (l Limiter) runner() CommandRunner {
	if l.Runner != nil {
		return l.Runner
	}
	return ExecRunner{}
}
