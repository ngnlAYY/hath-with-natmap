package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/bandwidth"
	"github.com/ngnlAYY/hath-with-natter/internal/config"
	"github.com/ngnlAYY/hath-with-natter/internal/hath"
	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
	"github.com/ngnlAYY/hath-with-natter/internal/upnp"
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

type fakeMainHath struct {
	running   bool
	starts    []int
	stopCalls int
}

func (f *fakeMainHath) Start(ctx context.Context, port int) error {
	f.running = true
	f.starts = append(f.starts, port)
	return nil
}

func (f *fakeMainHath) Stop(ctx context.Context) error {
	f.running = false
	f.stopCalls++
	return nil
}

func (f *fakeMainHath) Running() bool {
	return f.running
}

type fakeMainUpdater struct {
	ports     []int
	errUpdate error
}

func (f *fakeMainUpdater) UpdatePort(ctx context.Context, port int) error {
	if f.errUpdate != nil {
		return f.errUpdate
	}
	f.ports = append(f.ports, port)
	return nil
}

type fakeMainUPnPMapper struct {
	addCfg            upnp.Config
	addCalls          int
	addResult         upnp.Mapping
	addErr            error
	addCtxErr         error
	addHadDeadline    bool
	onAdd             func(int, upnp.Config)
	deleteCfg         upnp.Config
	deleteCalls       int
	deleteErr         error
	deleteCtxErr      error
	deleteHadDeadline bool
}

func (f *fakeMainUPnPMapper) AddMapping(ctx context.Context, cfg upnp.Config) (upnp.Mapping, error) {
	f.addCfg = cfg
	f.addCalls++
	f.addCtxErr = ctx.Err()
	_, f.addHadDeadline = ctx.Deadline()
	if f.onAdd != nil {
		f.onAdd(f.addCalls, cfg)
	}
	if f.addErr != nil {
		return upnp.Mapping{}, f.addErr
	}
	return f.addResult, nil
}

func (f *fakeMainUPnPMapper) DeleteMapping(ctx context.Context, cfg upnp.Config) error {
	f.deleteCfg = cfg
	f.deleteCalls++
	f.deleteCtxErr = ctx.Err()
	_, f.deleteHadDeadline = ctx.Deadline()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return nil
}

func nilNETAdmin() error {
	return nil
}

func TestBuildUpdaterHTTPClientUsesExternalUpdateTimeout(t *testing.T) {
	cfg := config.Config{
		Network: config.NetworkConfig{
			ExternalUpdateTimeout: config.Duration{Duration: 42 * time.Second},
		},
		Proxy: config.ProxyConfig{Enabled: false},
	}

	client, err := buildUpdaterHTTPClient(cfg)
	if err != nil {
		t.Fatalf("buildUpdaterHTTPClient returned error: %v", err)
	}
	if client.Timeout != 42*time.Second {
		t.Fatalf("client.Timeout = %s, want %s", client.Timeout, 42*time.Second)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("transport.Proxy != nil, want nil")
	}
}

func TestBuildUpdaterHTTPClientKeepsProxyAndTimeout(t *testing.T) {
	cfg := config.Config{
		Network: config.NetworkConfig{
			ExternalUpdateTimeout: config.Duration{Duration: 42 * time.Second},
		},
		Proxy: config.ProxyConfig{
			Enabled: true,
			URL:     "http://127.0.0.1:8080",
		},
	}

	client, err := buildUpdaterHTTPClient(cfg)
	if err != nil {
		t.Fatalf("buildUpdaterHTTPClient returned error: %v", err)
	}
	if client.Timeout != 42*time.Second {
		t.Fatalf("client.Timeout = %s, want %s", client.Timeout, 42*time.Second)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("transport.Proxy = nil, want non-nil")
	}
}

func TestBuildRuntimeWiresWhitelistConfig(t *testing.T) {
	cfg := config.Config{
		EHentai: config.EHentaiConfig{
			MemberID:  "member",
			PassHash:  "pass",
			ClientID:  "client",
			ClientKey: "key",
		},
		Network: config.NetworkConfig{BindPort: 16000},
		Natmap: config.NatmapConfig{
			BinaryPath:          "/bin/natmap",
			StunServer:          "stun.example:3478",
			HTTPKeepaliveServer: "https://keepalive.example/ping",
			KeepaliveInterval:   config.Duration{Duration: 30 * time.Second},
			NotifyScript:        "/usr/local/bin/hath-natmap notify",
			AddressFamily:       "ipv6",
			UDPMode:             true,
			Interface:           "eth0",
			FWMark:              "0x20",
			UDPCheckCycle:       45,
		},
		Hath: config.HathConfig{
			BinaryPath:           "/bin/hath",
			DataDir:              "/var/lib/hath",
			LogLevel:             "warn",
			ForceBackgroundScan:  true,
			RPCServerIP:          "127.0.0.1",
			DisableLogging:       true,
			FlushLog:             true,
			MaxConnection:        128,
			DisableIPOriginCheck: true,
			DisableFloodControl:  true,
			EnableMetrics:        true,
			DisableServerHeader:  true,
			EnableH3:             true,
		},
		Proxy: config.ProxyConfig{
			URL:                 "socks5://127.0.0.1:1080",
			UseForHathDownloads: true,
		},
		Runtime: config.RuntimeConfig{
			ShutdownTimeout: config.Duration{Duration: 10 * time.Second},
			RestartDelay:    config.Duration{Duration: 20 * time.Second},
			Retry:           config.RetryConfig{InitialDelay: config.Duration{Duration: 3 * time.Second}},
		},
	}
	runtime := buildRuntime(cfg, nil, nil, "notify-token")

	natmapRunner, ok := runtime.Natmap.(*natmap.ProcessRunner)
	if !ok {
		t.Fatalf("runtime.Natmap = %T, want *natmap.ProcessRunner", runtime.Natmap)
	}
	wantNatmap := natmap.RunnerConfig{
		BinaryPath:          cfg.Natmap.BinaryPath,
		BindPort:            cfg.Network.BindPort,
		StunServer:          cfg.Natmap.StunServer,
		HTTPKeepaliveServer: cfg.Natmap.HTTPKeepaliveServer,
		KeepaliveInterval:   cfg.Natmap.KeepaliveInterval.Duration,
		NotifyScript:        cfg.Natmap.NotifyScript,
		NotifyToken:         "notify-token",
		AddressFamily:       cfg.Natmap.AddressFamily,
		UDPMode:             cfg.Natmap.UDPMode,
		Interface:           cfg.Natmap.Interface,
		FWMark:              cfg.Natmap.FWMark,
		UDPCheckCycle:       cfg.Natmap.UDPCheckCycle,
	}
	if !reflect.DeepEqual(natmapRunner.Config, wantNatmap) {
		t.Fatalf("natmap config = %#v, want %#v", natmapRunner.Config, wantNatmap)
	}

	hathController, ok := runtime.Hath.(*hath.Controller)
	if !ok {
		t.Fatalf("runtime.Hath = %T, want *hath.Controller", runtime.Hath)
	}
	wantHath := hath.Config{
		BinaryPath:           cfg.Hath.BinaryPath,
		DataDir:              cfg.Hath.DataDir,
		LogLevel:             cfg.Hath.LogLevel,
		ForceBackgroundScan:  cfg.Hath.ForceBackgroundScan,
		RPCServerIP:          cfg.Hath.RPCServerIP,
		ProxyURL:             cfg.Proxy.URL,
		UseProxy:             cfg.Proxy.UseForHathDownloads,
		ClientID:             cfg.EHentai.ClientID,
		ClientKey:            cfg.EHentai.ClientKey,
		DisableLogging:       cfg.Hath.DisableLogging,
		FlushLog:             cfg.Hath.FlushLog,
		MaxConnection:        cfg.Hath.MaxConnection,
		DisableIPOriginCheck: cfg.Hath.DisableIPOriginCheck,
		DisableFloodControl:  cfg.Hath.DisableFloodControl,
		EnableMetrics:        cfg.Hath.EnableMetrics,
		DisableServerHeader:  cfg.Hath.DisableServerHeader,
		EnableH3:             cfg.Hath.EnableH3,
	}
	if !reflect.DeepEqual(hathController.Config, wantHath) {
		t.Fatalf("hath config = %#v, want %#v", hathController.Config, wantHath)
	}

	if runtime.BindPort != cfg.Network.BindPort {
		t.Fatalf("runtime.BindPort = %d, want %d", runtime.BindPort, cfg.Network.BindPort)
	}
	if runtime.RetryDelay != cfg.Runtime.Retry.InitialDelay.Duration {
		t.Fatalf("runtime.RetryDelay = %s, want %s", runtime.RetryDelay, cfg.Runtime.Retry.InitialDelay.Duration)
	}
	if runtime.RestartDelay != cfg.Runtime.RestartDelay.Duration {
		t.Fatalf("runtime.RestartDelay = %s, want %s", runtime.RestartDelay, cfg.Runtime.RestartDelay.Duration)
	}
	if runtime.ShutdownTimeout != cfg.Runtime.ShutdownTimeout.Duration {
		t.Fatalf("runtime.ShutdownTimeout = %s, want %s", runtime.ShutdownTimeout, cfg.Runtime.ShutdownTimeout.Duration)
	}
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
