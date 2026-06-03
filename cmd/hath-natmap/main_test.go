package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/bandwidth"
	"github.com/ngnlAYY/hath-with-natter/internal/config"
	"github.com/ngnlAYY/hath-with-natter/internal/hath"
	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
)

type fakeBandwidthRunner struct {
	calls      [][]string
	runErrs    []error
	outputErrs []error
	outputs    [][]byte
}

func (f *fakeBandwidthRunner) Run(ctx context.Context, name string, args ...string) error {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if len(f.runErrs) == 0 {
		return nil
	}
	err := f.runErrs[0]
	f.runErrs = f.runErrs[1:]
	return err
}

func (f *fakeBandwidthRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	var output []byte
	if len(f.outputs) > 0 {
		output = f.outputs[0]
		f.outputs = f.outputs[1:]
	}
	if len(f.outputErrs) == 0 {
		return output, nil
	}
	err := f.outputErrs[0]
	f.outputErrs = f.outputErrs[1:]
	return output, err
}

func nilNETAdmin() error {
	return nil
}

type fakeRoundTripper struct{}

func (fakeRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("fake round tripper")
}

func TestBuildRuntimeWiresRetryMaxDelay(t *testing.T) {
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 16000},
		Runtime: config.RuntimeConfig{
			Retry: config.RetryConfig{
				InitialDelay: config.Duration{Duration: 3 * time.Second},
				MaxDelay:     config.Duration{Duration: 12 * time.Second},
			},
		},
	}

	runtime := buildRuntime(cfg, nil, nil, "notify-token")

	if runtime.RetryDelay != cfg.Runtime.Retry.InitialDelay.Duration {
		t.Fatalf("runtime.RetryDelay = %s, want %s", runtime.RetryDelay, cfg.Runtime.Retry.InitialDelay.Duration)
	}
	if runtime.RetryMaxDelay != cfg.Runtime.Retry.MaxDelay.Duration {
		t.Fatalf("runtime.RetryMaxDelay = %s, want %s", runtime.RetryMaxDelay, cfg.Runtime.Retry.MaxDelay.Duration)
	}
}

func TestBuildUpdaterHTTPClientClonesDefaultTransport(t *testing.T) {
	cfg := config.Config{
		Network: config.NetworkConfig{ExternalUpdateTimeout: config.Duration{Duration: 15 * time.Second}},
	}
	defaultTransport := http.DefaultTransport.(*http.Transport)
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}
	originalProxyURL, originalProxyErr := defaultTransport.Proxy(req)

	client, err := buildUpdaterHTTPClient(cfg)
	if err != nil {
		t.Fatalf("buildUpdaterHTTPClient returned error: %v", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport = %T, want *http.Transport", client.Transport)
	}

	if transport == defaultTransport {
		t.Fatalf("client.Transport = %p, want cloned transport distinct from default %p", transport, defaultTransport)
	}
	if transport.MaxIdleConns != defaultTransport.MaxIdleConns {
		t.Fatalf("transport.MaxIdleConns = %d, want %d", transport.MaxIdleConns, defaultTransport.MaxIdleConns)
	}
	if transport.Proxy != nil {
		t.Fatalf("transport.Proxy = %p, want nil when proxy disabled", transport.Proxy)
	}
	defaultProxyURL, defaultProxyErr := defaultTransport.Proxy(req)
	if defaultProxyErr != originalProxyErr || defaultProxyURL != originalProxyURL {
		t.Fatalf("http.DefaultTransport.Proxy result changed from (%v, %v) to (%v, %v)", originalProxyURL, originalProxyErr, defaultProxyURL, defaultProxyErr)
	}
}

func TestBuildUpdaterHTTPClientUsesConfiguredProxy(t *testing.T) {
	cfg := config.Config{
		Network: config.NetworkConfig{ExternalUpdateTimeout: config.Duration{Duration: 15 * time.Second}},
		Proxy: config.ProxyConfig{
			Enabled: true,
			URL:     "http://127.0.0.1:8080",
		},
	}

	client, err := buildUpdaterHTTPClient(cfg)
	if err != nil {
		t.Fatalf("buildUpdaterHTTPClient returned error: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatalf("transport.Proxy = nil, want configured proxy")
	}

	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy returned error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != cfg.Proxy.URL {
		t.Fatalf("transport.Proxy returned %v, want %s", proxyURL, cfg.Proxy.URL)
	}
}

func TestBuildUpdaterHTTPClientReturnsErrorForInvalidDefaultTransport(t *testing.T) {
	originalDefaultTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalDefaultTransport })

	t.Run("nil", func(t *testing.T) {
		http.DefaultTransport = nil
		if _, err := buildUpdaterHTTPClient(config.Config{}); err == nil {
			t.Fatalf("buildUpdaterHTTPClient returned nil error, want error")
		}
	})

	t.Run("non-http-transport", func(t *testing.T) {
		http.DefaultTransport = fakeRoundTripper{}
		if _, err := buildUpdaterHTTPClient(config.Config{}); err == nil {
			t.Fatalf("buildUpdaterHTTPClient returned nil error, want error")
		}
	})
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
			Retry: config.RetryConfig{
				InitialDelay: config.Duration{Duration: 3 * time.Second},
				MaxDelay:     config.Duration{Duration: 12 * time.Second},
			},
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

func TestApplyBandwidthLimitWiresAllowReplaceRootQdisc(t *testing.T) {
	runner := &fakeBandwidthRunner{outputs: [][]byte{[]byte("qdisc fq_codel 0: root refcnt 2\n")}}
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 16000},
		Bandwidth: config.BandwidthConfig{
			Enabled:               true,
			Interface:             "eth0",
			UploadLimit:           "10mbit",
			AllowReplaceRootQdisc: true,
		},
	}

	limiter := buildBandwidthLimiter(cfg)
	limiter.Runner = runner

	clear, err := applyBandwidthLimit(context.Background(), cfg, limiter, nilNETAdmin)
	if err != nil {
		t.Fatalf("applyBandwidthLimit returned error: %v", err)
	}
	_ = clear

	want := [][]string{
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "16000", "0xffff", "flowid", "1:10"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("tc calls = %#v, want %#v", runner.calls, want)
	}
}

func TestApplyBandwidthLimitAppliesAndClearsWhenEnabled(t *testing.T) {
	runner := &fakeBandwidthRunner{outputs: [][]byte{
		[]byte("qdisc noqueue 0: root refcnt 2\n"),
		[]byte("qdisc htb 1: root refcnt 2 default 3fed\n"),
	}}
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
		{"tc", "qdisc", "show", "dev", "eth0"},
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "3fed"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:3fed", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "16000", "0xffff", "flowid", "1:10"},
		{"tc", "qdisc", "show", "dev", "eth0"},
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
	runner := &fakeBandwidthRunner{
		outputs: [][]byte{[]byte("qdisc noqueue 0: root refcnt 2\n")},
		runErrs: []error{errors.New("tc failed")},
	}
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
	runner := &fakeBandwidthRunner{
		outputs: [][]byte{
			[]byte("qdisc noqueue 0: root refcnt 2\n"),
			[]byte("qdisc htb 1: root refcnt 2 default 3fed\n"),
		},
		runErrs: []error{nil, nil, nil, nil, errors.New("clear failed")},
	}
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
