package app_test

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/app"
	"github.com/ngnlAYY/hath-with-natter/internal/bandwidth"
	"github.com/ngnlAYY/hath-with-natter/internal/config"
	"github.com/ngnlAYY/hath-with-natter/internal/hath"
	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
	"github.com/ngnlAYY/hath-with-natter/internal/upnp"
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

type fakeRoundTripper struct{}

func (fakeRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("fake round tripper")
}

func TestBuildPortUpdaterSkipsEHentaiPortUpdate(t *testing.T) {
	var logs bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	}()

	updater := app.BuildPortUpdater(config.Config{EHentai: config.EHentaiConfig{SkipPortUpdate: true}}, ehentaiPanicUpdater{})
	if err := updater.UpdatePort(context.Background(), 4567); err != nil {
		t.Fatalf("UpdatePort() error = %v", err)
	}
	if !strings.Contains(logs.String(), "跳过更新 H@H 公网端口") {
		t.Fatalf("log output = %q, want skip port update log", logs.String())
	}
}

func TestBuildPortUpdaterSkippedUpdateRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	updater := app.BuildPortUpdater(config.Config{EHentai: config.EHentaiConfig{SkipPortUpdate: true}}, ehentaiPanicUpdater{})
	if err := updater.UpdatePort(ctx, 4567); !errors.Is(err, context.Canceled) {
		t.Fatalf("UpdatePort() error = %v, want context.Canceled", err)
	}
}

type ehentaiPanicUpdater struct{}

func (ehentaiPanicUpdater) UpdatePort(ctx context.Context, port int) error {
	return errors.New("real updater called")
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

	runtime := app.BuildRuntime(cfg, nil, nil, "notify-token")

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

	client, err := app.BuildUpdaterHTTPClient(cfg)
	if err != nil {
		t.Fatalf("app.BuildUpdaterHTTPClient returned error: %v", err)
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

	client, err := app.BuildUpdaterHTTPClient(cfg)
	if err != nil {
		t.Fatalf("app.BuildUpdaterHTTPClient returned error: %v", err)
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
		if _, err := app.BuildUpdaterHTTPClient(config.Config{}); err == nil {
			t.Fatalf("app.BuildUpdaterHTTPClient returned nil error, want error")
		}
	})

	t.Run("non-http-transport", func(t *testing.T) {
		http.DefaultTransport = fakeRoundTripper{}
		if _, err := app.BuildUpdaterHTTPClient(config.Config{}); err == nil {
			t.Fatalf("app.BuildUpdaterHTTPClient returned nil error, want error")
		}
	})
}

func TestEnsureNotifySocketDirUsesConfiguredSocketParent(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "custom", "notify.sock")

	if err := app.EnsureNotifySocketDir(socketPath); err != nil {
		t.Fatalf("app.EnsureNotifySocketDir() error = %v", err)
	}

	info, err := os.Stat(filepath.Dir(socketPath))
	if err != nil {
		t.Fatalf("socket parent stat error = %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("socket parent is not a directory")
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("socket parent mode = %o, want 700", got)
	}
}

func TestEnsureNotifySocketDirDoesNotChmodExistingParent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	if err := app.EnsureNotifySocketDir(filepath.Join(dir, "notify.sock")); err != nil {
		t.Fatalf("app.EnsureNotifySocketDir() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("socket parent stat error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("socket parent mode = %o, want 755", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("socket parent read error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("socket parent entries = %#v, want none", entries)
	}
}

func TestEnsureNotifySocketDirRejectsFileParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := app.EnsureNotifySocketDir(filepath.Join(parent, "notify.sock"))
	if err == nil || !strings.Contains(err.Error(), "不是目录") {
		t.Fatalf("app.EnsureNotifySocketDir() error = %v, want not directory error", err)
	}
}

func TestBuildUPnPConfigUsesBindPortLeaseAndDescription(t *testing.T) {
	cfg := config.Config{
		Network: config.NetworkConfig{BindPort: 4567},
		UPnP: config.UPnPConfig{
			LeaseDuration: 30,
			Description:   "upnp-test",
		},
	}

	got := app.BuildUPnPConfig(cfg)
	want := upnp.Config{
		Port:          4567,
		LeaseDuration: 30,
		Description:   "upnp-test",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("app.BuildUPnPConfig() = %#v, want %#v", got, want)
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
			Retry: config.RetryConfig{
				InitialDelay: config.Duration{Duration: 3 * time.Second},
				MaxDelay:     config.Duration{Duration: 12 * time.Second},
			},
		},
	}
	runtime := app.BuildRuntime(cfg, nil, nil, "notify-token")

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

	limiter := app.BuildBandwidthLimiter(cfg)
	limiter.Runner = runner

	clear, err := app.ApplyBandwidthLimit(context.Background(), cfg, limiter, nilNETAdmin)
	if err != nil {
		t.Fatalf("app.ApplyBandwidthLimit returned error: %v", err)
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

	clear, err := app.ApplyBandwidthLimit(context.Background(), cfg, bandwidth.Limiter{
		Runner:      runner,
		Interface:   cfg.Bandwidth.Interface,
		UploadLimit: cfg.Bandwidth.UploadLimit,
	}, nilNETAdmin)
	if err != nil {
		t.Fatalf("app.ApplyBandwidthLimit returned error: %v", err)
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

	clear, err := app.ApplyBandwidthLimit(context.Background(), cfg, bandwidth.Limiter{Runner: runner}, nilNETAdmin)
	if err != nil {
		t.Fatalf("app.ApplyBandwidthLimit returned error: %v", err)
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

	_, err := app.ApplyBandwidthLimit(context.Background(), cfg, bandwidth.Limiter{
		Runner:      runner,
		Interface:   cfg.Bandwidth.Interface,
		UploadLimit: cfg.Bandwidth.UploadLimit,
	}, nilNETAdmin)
	if err == nil || !strings.Contains(err.Error(), "配置上传限速失败") {
		t.Fatalf("app.ApplyBandwidthLimit error = %v, want bandwidth apply error", err)
	}
}

func TestApplyBandwidthLimitReturnsNETAdminError(t *testing.T) {
	runner := &fakeBandwidthRunner{}
	cfg := config.Config{
		Network:   config.NetworkConfig{BindPort: 16000},
		Bandwidth: config.BandwidthConfig{Enabled: true, Interface: "eth0", UploadLimit: "10mbit"},
	}

	_, err := app.ApplyBandwidthLimit(context.Background(), cfg, bandwidth.Limiter{Runner: runner}, func() error {
		return errors.New("missing net admin")
	})
	if err == nil || !strings.Contains(err.Error(), "missing net admin") {
		t.Fatalf("app.ApplyBandwidthLimit error = %v, want NET_ADMIN error", err)
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

	clear, err := app.ApplyBandwidthLimit(context.Background(), cfg, bandwidth.Limiter{
		Runner:      runner,
		Interface:   cfg.Bandwidth.Interface,
		UploadLimit: cfg.Bandwidth.UploadLimit,
	}, nilNETAdmin)
	if err != nil {
		t.Fatalf("app.ApplyBandwidthLimit returned error: %v", err)
	}
	clear()

	if !strings.Contains(logs.String(), "清理上传限速规则失败") {
		t.Fatalf("log output = %q, want clear failure", logs.String())
	}
}
