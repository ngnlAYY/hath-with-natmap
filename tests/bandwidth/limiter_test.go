package bandwidth_test

import (
	"context"
	"errors"
	bandwidth "github.com/ngnlAYY/hath-with-natter/internal/bandwidth"
	"reflect"
	"strings"
	"testing"
)

const (
	ownedRootQdiscOutput      = "qdisc htb 1: root refcnt 2 default 3fed\n"
	foreignRootQdiscOutput    = "qdisc fq_codel 0: root refcnt 2 limit 1024p flows 1024 quantum 1514 target 5.0ms interval 100.0ms memory_limit 4Mb ecn drop_batch 64\n"
	foreignHTBRootQdiscOutput = "qdisc htb 1: root refcnt 2 default 30\n"
	noqueueRootQdiscOutput    = "qdisc noqueue 0: root refcnt 2\n"
	nonRootProjectQdiscOutput = "qdisc fq_codel 0: root refcnt 2\nqdisc htb 1: parent 10:1 refcnt 2 default 30\n"
)

type fakeOutputCommandRunner struct {
	calls      [][]string
	runErrs    []error
	outputErrs []error
	outputs    [][]byte
}

func (f *fakeOutputCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if len(f.runErrs) == 0 {
		return nil
	}
	err := f.runErrs[0]
	f.runErrs = f.runErrs[1:]
	return err
}

func (f *fakeOutputCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	var output []byte
	if len(f.outputs) > 0 {
		output = f.outputs[0]
		f.outputs = f.outputs[1:]
	} else {
		output = []byte(ownedRootQdiscOutput)
	}
	if len(f.outputErrs) == 0 {
		return output, nil
	}
	err := f.outputErrs[0]
	f.outputErrs = f.outputErrs[1:]
	return output, err
}

type fakeCommandRunner struct {
	calls   [][]string
	runErrs []error
}

func (f *fakeCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if len(f.runErrs) == 0 {
		return nil
	}
	err := f.runErrs[0]
	f.runErrs = f.runErrs[1:]
	return err
}

func TestApplyBuildsTCRules(t *testing.T) {
	fake := &fakeOutputCommandRunner{outputs: [][]byte{[]byte(noqueueRootQdiscOutput)}}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}

	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	want := [][]string{
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "4567", "0xffff", "flowid", "1:10"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestApplyAllowsClearAfterPartialApply(t *testing.T) {
	runnerErr := errors.New("class failed")
	fake := &fakeOutputCommandRunner{
		outputs: [][]byte{[]byte(noqueueRootQdiscOutput), []byte(ownedRootQdiscOutput)},
		runErrs: []error{nil, runnerErr},
	}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}

	err := limiter.Apply(context.Background(), 4567)
	if err == nil {
		t.Fatal("Apply() error = nil, want error")
	}
	if !errors.Is(err, runnerErr) {
		t.Fatalf("Apply() error does not wrap runner error: %v", err)
	}

	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	want := [][]string{
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "del", "dev", "eth0", "root"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestApplyRejectsForeignRootQdiscWithoutOptIn(t *testing.T) {
	fake := &fakeOutputCommandRunner{outputs: [][]byte{[]byte(foreignRootQdiscOutput)}}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}

	err := limiter.Apply(context.Background(), 4567)
	if err == nil {
		t.Fatal("Apply() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "拒绝覆盖") {
		t.Fatalf("Apply() error = %q, want 拒绝覆盖", err.Error())
	}
	if !strings.Contains(err.Error(), "bandwidth.allow_replace_root_qdisc: true") {
		t.Fatalf("Apply() error = %q, want opt-in hint", err.Error())
	}

	want := [][]string{{"tc", "qdisc", "show", "dev", "eth0"}}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestApplyRejectsForeignRootWhenNonRootQdiscMatchesProjectMarker(t *testing.T) {
	fake := &fakeOutputCommandRunner{outputs: [][]byte{[]byte(nonRootProjectQdiscOutput)}}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}

	err := limiter.Apply(context.Background(), 4567)
	if err == nil {
		t.Fatal("Apply() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "拒绝覆盖") {
		t.Fatalf("Apply() error = %q, want 拒绝覆盖", err.Error())
	}

	want := [][]string{{"tc", "qdisc", "show", "dev", "eth0"}}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestApplyRejectsPreexistingHTBRootQdiscWithoutOptIn(t *testing.T) {
	fake := &fakeOutputCommandRunner{outputs: [][]byte{[]byte(foreignHTBRootQdiscOutput)}}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}

	err := limiter.Apply(context.Background(), 4567)
	if err == nil {
		t.Fatal("Apply() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "拒绝覆盖") {
		t.Fatalf("Apply() error = %q, want 拒绝覆盖", err.Error())
	}

	want := [][]string{{"tc", "qdisc", "show", "dev", "eth0"}}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestApplyAllowsForeignRootQdiscWithOptIn(t *testing.T) {
	fake := &fakeOutputCommandRunner{outputs: [][]byte{[]byte(foreignRootQdiscOutput)}}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit", AllowReplaceRootQdisc: true}

	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	want := [][]string{
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "4567", "0xffff", "flowid", "1:10"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestApplyRejectsUnsupportedRunnerWithoutOptIn(t *testing.T) {
	fake := &fakeCommandRunner{}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}

	err := limiter.Apply(context.Background(), 4567)
	if err == nil {
		t.Fatal("Apply() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "拒绝覆盖") {
		t.Fatalf("Apply() error = %q, want 拒绝覆盖", err.Error())
	}
	if !strings.Contains(err.Error(), "bandwidth.allow_replace_root_qdisc: true") {
		t.Fatalf("Apply() error = %q, want opt-in hint", err.Error())
	}
	if len(fake.calls) != 0 {
		t.Fatalf("calls = %#v, want none", fake.calls)
	}
}

func TestApplyAllowsUnsupportedRunnerWithOptIn(t *testing.T) {
	fake := &fakeCommandRunner{}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit", AllowReplaceRootQdisc: true}

	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	want := [][]string{
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "4567", "0xffff", "flowid", "1:10"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestClearDeletesOwnedRootQdisc(t *testing.T) {
	fake := &fakeOutputCommandRunner{outputs: [][]byte{[]byte(noqueueRootQdiscOutput), []byte(ownedRootQdiscOutput)}}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}
	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	want := [][]string{
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "4567", "0xffff", "flowid", "1:10"},
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "del", "dev", "eth0", "root"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestClearSkipsForeignRootQdisc(t *testing.T) {
	fake := &fakeOutputCommandRunner{outputs: [][]byte{[]byte(foreignRootQdiscOutput), []byte(foreignRootQdiscOutput)}}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit", AllowReplaceRootQdisc: true}
	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	want := [][]string{
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "4567", "0xffff", "flowid", "1:10"},
		{"tc", "qdisc", "show", "dev", "eth0"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestClearSkipsPreexistingHTBRootQdiscWithoutApply(t *testing.T) {
	fake := &fakeOutputCommandRunner{outputs: [][]byte{[]byte(foreignHTBRootQdiscOutput)}}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0"}

	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("calls = %#v, want none", fake.calls)
	}
}

func TestClearSkipsForeignHTBRootQdiscAfterApply(t *testing.T) {
	fake := &fakeOutputCommandRunner{outputs: [][]byte{[]byte(foreignHTBRootQdiscOutput), []byte(foreignHTBRootQdiscOutput)}}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit", AllowReplaceRootQdisc: true}
	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	want := [][]string{
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "4567", "0xffff", "flowid", "1:10"},
		{"tc", "qdisc", "show", "dev", "eth0"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestClearDoesNotDeleteTwice(t *testing.T) {
	fake := &fakeOutputCommandRunner{outputs: [][]byte{[]byte(noqueueRootQdiscOutput), []byte(ownedRootQdiscOutput), []byte(ownedRootQdiscOutput)}}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}
	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("first Clear() error = %v", err)
	}
	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("second Clear() error = %v", err)
	}

	want := [][]string{
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "4567", "0xffff", "flowid", "1:10"},
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "del", "dev", "eth0", "root"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestClearSkipsWhenRunnerCannotInspectOwnership(t *testing.T) {
	fake := &fakeCommandRunner{}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0"}

	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("calls = %#v, want none", fake.calls)
	}
}

func TestClearRetainsAppliedWhenRootQdiscCheckFails(t *testing.T) {
	fake := &fakeOutputCommandRunner{
		outputErrs: []error{nil, errors.New("show failed")},
		outputs:    [][]byte{[]byte(noqueueRootQdiscOutput), []byte(ownedRootQdiscOutput), []byte(ownedRootQdiscOutput)},
	}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}
	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	err := limiter.Clear(context.Background())
	if err == nil || !strings.Contains(err.Error(), "检查 root qdisc 失败") {
		t.Fatalf("first Clear() error = %v, want root qdisc inspection error", err)
	}
	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("second Clear() error = %v", err)
	}

	want := [][]string{
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "4567", "0xffff", "flowid", "1:10"},
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "del", "dev", "eth0", "root"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestExecRunnerErrorDoesNotIncludeCommandOutput(t *testing.T) {
	err := bandwidth.ExecRunner{}.Run(context.Background(), "false")
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if strings.Contains(err.Error(), "输出") {
		t.Fatalf("Run() error included command output context: %v", err)
	}
}

func TestApplyWrapsRunnerErrorWithChineseContext(t *testing.T) {
	runnerErr := errors.New("runner failed")
	fake := &fakeOutputCommandRunner{
		outputs: [][]byte{[]byte(noqueueRootQdiscOutput)},
		runErrs: []error{runnerErr},
	}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}

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
	fake := &fakeOutputCommandRunner{
		outputs: [][]byte{[]byte(noqueueRootQdiscOutput), []byte(ownedRootQdiscOutput)},
		runErrs: []error{nil, nil, nil, nil, runnerErr},
	}
	limiter := bandwidth.Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}
	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

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
