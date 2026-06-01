package bandwidth

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeCommandRunner struct {
	calls [][]string
	err   error
}

func (f *fakeCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.err
}

func TestApplyBuildsTCRules(t *testing.T) {
	fake := &fakeCommandRunner{}
	limiter := Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}

	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	want := [][]string{
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "30"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:30", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "4567", "0xffff", "flowid", "1:10"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestClearDeletesRootQdisc(t *testing.T) {
	fake := &fakeCommandRunner{}
	limiter := Limiter{Runner: fake, Interface: "eth0"}

	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	want := [][]string{{"tc", "qdisc", "del", "dev", "eth0", "root"}}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestExecRunnerErrorDoesNotIncludeCommandOutput(t *testing.T) {
	err := ExecRunner{}.Run(context.Background(), "false")
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if strings.Contains(err.Error(), "输出") {
		t.Fatalf("Run() error included command output context: %v", err)
	}
}

func TestApplyWrapsRunnerErrorWithChineseContext(t *testing.T) {
	runnerErr := errors.New("runner failed")
	fake := &fakeCommandRunner{err: runnerErr}
	limiter := Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}

	err := limiter.Apply(context.Background(), 4567)
	if err == nil {
		t.Fatal("Apply() error = nil, want error")
	}
	if !errors.Is(err, runnerErr) {
		t.Fatalf("Apply() error does not wrap runner error: %v", err)
	}
	if !strings.Contains(err.Error(), "配置上传限速失败") {
		t.Fatalf("Apply() error = %q, want Chinese context", err.Error())
	}
}

func TestClearWrapsRunnerErrorWithChineseContext(t *testing.T) {
	runnerErr := errors.New("runner failed")
	fake := &fakeCommandRunner{err: runnerErr}
	limiter := Limiter{Runner: fake, Interface: "eth0"}

	err := limiter.Clear(context.Background())
	if err == nil {
		t.Fatal("Clear() error = nil, want error")
	}
	if !errors.Is(err, runnerErr) {
		t.Fatalf("Clear() error does not wrap runner error: %v", err)
	}
	if !strings.Contains(err.Error(), "清理上传限速规则失败") {
		t.Fatalf("Clear() error = %q, want Chinese context", err.Error())
	}
}
