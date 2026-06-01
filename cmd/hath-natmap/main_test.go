package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"reflect"
	"strings"
	"testing"

	"github.com/ngnlAYY/hath-with-natter/internal/bandwidth"
	"github.com/ngnlAYY/hath-with-natter/internal/config"
)

type fakeBandwidthRunner struct {
	calls [][]string
	errs  []error
}

func (f *fakeBandwidthRunner) Run(ctx context.Context, name string, args ...string) error {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if len(f.errs) == 0 {
		return nil
	}
	err := f.errs[0]
	f.errs = f.errs[1:]
	return err
}

func nilNETAdmin() error {
	return nil
}

func TestApplyBandwidthLimitAppliesAndClearsWhenEnabled(t *testing.T) {
	runner := &fakeBandwidthRunner{}
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 16000},
		Bandwidth: config.BandwidthConfig{
			Enabled:     true,
			Interface:   "eth0",
			UploadLimit: "10mbit",
		},
	}

	clear, err := applyBandwidthLimit(context.Background(), cfg, bandwidth.Limiter{
		Runner:      runner,
		Interface:   cfg.Bandwidth.Interface,
		UploadLimit: cfg.Bandwidth.UploadLimit,
	}, nilNETAdmin)
	if err != nil {
		t.Fatalf("applyBandwidthLimit returned error: %v", err)
	}
	clear()

	want := [][]string{
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "30"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:30", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "16000", "0xffff", "flowid", "1:10"},
		{"tc", "qdisc", "del", "dev", "eth0", "root"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("tc calls = %#v, want %#v", runner.calls, want)
	}
}

func TestApplyBandwidthLimitSkipsWhenDisabled(t *testing.T) {
	runner := &fakeBandwidthRunner{}
	cfg := config.Config{Bandwidth: config.BandwidthConfig{Enabled: false}}

	clear, err := applyBandwidthLimit(context.Background(), cfg, bandwidth.Limiter{Runner: runner}, nilNETAdmin)
	if err != nil {
		t.Fatalf("applyBandwidthLimit returned error: %v", err)
	}
	clear()

	if len(runner.calls) != 0 {
		t.Fatalf("tc calls = %#v, want none", runner.calls)
	}
}

func TestApplyBandwidthLimitReturnsApplyError(t *testing.T) {
	runner := &fakeBandwidthRunner{errs: []error{errors.New("tc failed")}}
	cfg := config.Config{
		Network:   config.NetworkConfig{BindPort: 16000},
		Bandwidth: config.BandwidthConfig{Enabled: true, Interface: "eth0", UploadLimit: "10mbit"},
	}

	_, err := applyBandwidthLimit(context.Background(), cfg, bandwidth.Limiter{
		Runner:      runner,
		Interface:   cfg.Bandwidth.Interface,
		UploadLimit: cfg.Bandwidth.UploadLimit,
	}, nilNETAdmin)
	if err == nil || !strings.Contains(err.Error(), "配置上传限速失败") {
		t.Fatalf("applyBandwidthLimit error = %v, want bandwidth apply error", err)
	}
}

func TestApplyBandwidthLimitReturnsNETAdminError(t *testing.T) {
	runner := &fakeBandwidthRunner{}
	cfg := config.Config{
		Network:   config.NetworkConfig{BindPort: 16000},
		Bandwidth: config.BandwidthConfig{Enabled: true, Interface: "eth0", UploadLimit: "10mbit"},
	}

	_, err := applyBandwidthLimit(context.Background(), cfg, bandwidth.Limiter{Runner: runner}, func() error {
		return errors.New("missing net admin")
	})
	if err == nil || !strings.Contains(err.Error(), "missing net admin") {
		t.Fatalf("applyBandwidthLimit error = %v, want NET_ADMIN error", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("tc calls = %#v, want none", runner.calls)
	}
}

func TestApplyBandwidthLimitLogsClearError(t *testing.T) {
	runner := &fakeBandwidthRunner{errs: []error{nil, nil, nil, nil, errors.New("clear failed")}}
	cfg := config.Config{
		Network:   config.NetworkConfig{BindPort: 16000},
		Bandwidth: config.BandwidthConfig{Enabled: true, Interface: "eth0", UploadLimit: "10mbit"},
	}
	var logs bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	}()

	clear, err := applyBandwidthLimit(context.Background(), cfg, bandwidth.Limiter{
		Runner:      runner,
		Interface:   cfg.Bandwidth.Interface,
		UploadLimit: cfg.Bandwidth.UploadLimit,
	}, nilNETAdmin)
	if err != nil {
		t.Fatalf("applyBandwidthLimit returned error: %v", err)
	}
	clear()

	if !strings.Contains(logs.String(), "清理上传限速规则失败") {
		t.Fatalf("log output = %q, want clear failure", logs.String())
	}
}
