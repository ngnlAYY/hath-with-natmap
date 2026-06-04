package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/bandwidth"
	"github.com/ngnlAYY/hath-with-natter/internal/config"
	"github.com/ngnlAYY/hath-with-natter/internal/ehentai"
	"github.com/ngnlAYY/hath-with-natter/internal/hath"
	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
	"github.com/ngnlAYY/hath-with-natter/internal/process"
	"github.com/ngnlAYY/hath-with-natter/internal/supervisor"
	"github.com/ngnlAYY/hath-with-natter/internal/upnp"
)

type upnpMapper interface {
	AddMapping(ctx context.Context, cfg upnp.Config) (upnp.Mapping, error)
	DeleteMapping(ctx context.Context, cfg upnp.Config) error
}

var newUPnPRenewTicker = func(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)
	return ticker.C, ticker.Stop
}

var sleepUPnPRestart = func(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		log.Fatalf("%v", err)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("hath-natmap", flag.ContinueOnError)
	configPath := flags.String("config", config.DefaultConfigPath, "配置文件路径")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("解析命令行参数失败: %w", err)
	}
	remaining := flags.Args()
	if len(remaining) > 0 && remaining[0] == "notify" {
		if err := natmap.SendNotify(notifySocketPath(), remaining[1:]); err != nil {
			return fmt.Errorf("发送 natmap notify 事件失败: %w", err)
		}
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	clearBandwidth, err := applyBandwidthLimit(ctx, cfg, buildBandwidthLimiter(cfg), bandwidth.CheckNETAdmin)
	if err != nil {
		return err
	}
	defer clearBandwidth()

	updaterHTTPClient, err := buildUpdaterHTTPClient(cfg)
	if err != nil {
		return err
	}
	updater := buildPortUpdater(cfg, ehentai.Client{
		HTTPClient: updaterHTTPClient,
		MemberID:   cfg.EHentai.MemberID,
		PassHash:   cfg.EHentai.PassHash,
		ClientID:   cfg.EHentai.ClientID,
	})

	if cfg.Mapping.Mode == "upnp" {
		runtime := buildRuntime(cfg, nil, nil, "")
		if err := runUPnPModeWithRetry(ctx, cfg, runtime.Hath, updater, upnp.Client{}); err != nil {
			return fmt.Errorf("运行失败: %w", err)
		}
		return nil
	}

	socketPath := notifySocketPath()
	if err := ensureNotifySocketDir(socketPath); err != nil {
		return err
	}
	notifyToken, err := natmap.GenerateNotifyToken()
	if err != nil {
		return err
	}
	listener, events, err := natmap.ListenNotifyWithToken(socketPath, notifyToken)
	if err != nil {
		return fmt.Errorf("启动 natmap notify socket 失败: %w", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			log.Printf("关闭 natmap notify socket 失败: %v", err)
		}
	}()

	runtime := buildRuntime(cfg, listener, events, notifyToken)
	runtime.Updater = updater
	if err := runtime.Run(ctx); err != nil {
		return fmt.Errorf("运行失败: %w", err)
	}
	return nil
}

type skipPortUpdater struct{}

func (skipPortUpdater) UpdatePort(ctx context.Context, port int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	log.Printf("跳过更新 H@H 公网端口 %d", port)
	return nil
}

func buildPortUpdater(cfg config.Config, updater supervisor.PortUpdater) supervisor.PortUpdater {
	if cfg.EHentai.SkipPortUpdate {
		return skipPortUpdater{}
	}
	return updater
}

func buildBandwidthLimiter(cfg config.Config) bandwidth.Limiter {
	return bandwidth.Limiter{
		Interface:             cfg.Bandwidth.Interface,
		UploadLimit:           cfg.Bandwidth.UploadLimit,
		AllowReplaceRootQdisc: cfg.Bandwidth.AllowReplaceRootQdisc,
	}
}

func buildUpdaterHTTPClient(cfg config.Config) (*http.Client, error) {
	client := &http.Client{Timeout: cfg.Network.ExternalUpdateTimeout.Duration}
	transport, err := cloneDefaultHTTPTransport()
	if err != nil {
		return nil, err
	}
	if !cfg.Proxy.Enabled {
		transport.Proxy = nil
		client.Transport = transport
		return client, nil
	}

	proxyURL, err := url.Parse(cfg.Proxy.URL)
	if err != nil {
		return nil, fmt.Errorf("解析代理 URL 失败: %w", err)
	}
	transport.Proxy = http.ProxyURL(proxyURL)
	client.Transport = transport
	return client, nil
}

func cloneDefaultHTTPTransport() (*http.Transport, error) {
	if http.DefaultTransport == nil {
		return nil, fmt.Errorf("默认 HTTP transport 为空，无法克隆")
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("默认 HTTP transport 类型为 %T，无法克隆", http.DefaultTransport)
	}
	if transport == nil {
		return nil, fmt.Errorf("默认 HTTP transport 为空，无法克隆")
	}
	return transport.Clone(), nil
}

func runUPnPModeOnce(ctx context.Context, cfg config.Config, hathController supervisor.HathController, updater supervisor.PortUpdater, mapper upnpMapper) (func(), error) {
	upnpCfg := buildUPnPConfig(cfg)
	operationCtx, cancel := upnpOperationContext(cfg)
	mapping, err := mapper.AddMapping(operationCtx, upnpCfg)
	cancel()
	if err != nil {
		return nil, err
	}

	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.Runtime.ShutdownTimeout.Duration)
		defer cancel()
		if err := mapper.DeleteMapping(cleanupCtx, upnpCfg); err != nil {
			log.Printf("删除 UPnP 端口映射失败: %v", err)
		}
	}

	coordinator := supervisor.Coordinator{Hath: hathController, Updater: updater, BindPort: cfg.Network.BindPort}
	err = coordinator.HandleMapping(ctx, natmap.Mapping{
		PublicAddress:  mapping.PublicAddress,
		PublicPort:     mapping.PublicPort,
		PrivatePort:    mapping.PrivatePort,
		Protocol:       "TCP",
		PrivateAddress: mapping.PrivateAddress,
	})
	if err != nil {
		cleanup()
		return nil, err
	}
	return cleanup, nil
}

func upnpOperationContext(cfg config.Config) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), cfg.Runtime.ShutdownTimeout.Duration)
}

func runUPnPModeWithRetry(ctx context.Context, cfg config.Config, hathController supervisor.HathController, updater supervisor.PortUpdater, mapper upnpMapper) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		if err := runUPnPMode(ctx, cfg, hathController, updater, mapper); err != nil {
			log.Printf("UPnP 运行期错误，准备自动恢复: %v", err)
			if !sleepUPnPRestart(ctx, upnpRestartDelay(cfg)) {
				return nil
			}
			continue
		}
		return nil
	}
}

func upnpRestartDelay(cfg config.Config) time.Duration {
	if cfg.Runtime.RestartDelay.Duration > 0 {
		return cfg.Runtime.RestartDelay.Duration
	}
	if cfg.Runtime.Retry.InitialDelay.Duration > 0 {
		return cfg.Runtime.Retry.InitialDelay.Duration
	}
	return 5 * time.Second
}

func runUPnPMode(ctx context.Context, cfg config.Config, hathController supervisor.HathController, updater supervisor.PortUpdater, mapper upnpMapper) error {
	cleanup, err := runUPnPModeOnce(ctx, cfg, hathController, updater, mapper)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Runtime.ShutdownTimeout.Duration)
		defer cancel()
		if err := hathController.Stop(shutdownCtx); err != nil {
			log.Printf("停止 hath-rust 失败: %v", err)
		}
	}()

	if leaseDurationSeconds := uint32(cfg.UPnP.LeaseDuration); leaseDurationSeconds > 0 {
		interval := time.Duration(leaseDurationSeconds) * time.Second / 2
		tickCh, stopTicker := newUPnPRenewTicker(interval)
		defer stopTicker()

		upnpCfg := buildUPnPConfig(cfg)
		for {
			select {
			case <-ctx.Done():
				cleanup()
				return nil
			case <-tickCh:
				operationCtx, cancel := upnpOperationContext(cfg)
				_, err := mapper.AddMapping(operationCtx, upnpCfg)
				cancel()
				if err != nil {
					if ctx.Err() != nil {
						cleanup()
						return nil
					}
					return fmt.Errorf("续租 UPnP 端口映射失败: %w", err)
				}
			}
		}
	}

	<-ctx.Done()
	cleanup()
	return nil
}

func buildRuntime(cfg config.Config, listener *natmap.Listener, events <-chan natmap.Mapping, notifyToken string) supervisor.Runtime {
	runner := process.OSRunner{}
	natmapRunner := &natmap.ProcessRunner{
		Config: natmap.RunnerConfig{
			BinaryPath:          cfg.Natmap.BinaryPath,
			BindPort:            cfg.Network.BindPort,
			StunServer:          cfg.Natmap.StunServer,
			HTTPKeepaliveServer: cfg.Natmap.HTTPKeepaliveServer,
			KeepaliveInterval:   cfg.Natmap.KeepaliveInterval.Duration,
			NotifyScript:        cfg.Natmap.NotifyScript,
			NotifyToken:         notifyToken,
			AddressFamily:       cfg.Natmap.AddressFamily,
			UDPMode:             cfg.Natmap.UDPMode,
			Interface:           cfg.Natmap.Interface,
			FWMark:              cfg.Natmap.FWMark,
			UDPCheckCycle:       cfg.Natmap.UDPCheckCycle,
		},
		Runner:   runner,
		Listener: listener,
	}
	hathController := &hath.Controller{
		Config: hath.Config{
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
		},
		Runner: runner,
	}

	return supervisor.Runtime{
		Natmap:          natmapRunner,
		Hath:            hathController,
		Events:          events,
		BindPort:        cfg.Network.BindPort,
		RetryDelay:      cfg.Runtime.Retry.InitialDelay.Duration,
		RetryMaxDelay:   cfg.Runtime.Retry.MaxDelay.Duration,
		RestartDelay:    cfg.Runtime.RestartDelay.Duration,
		ShutdownTimeout: cfg.Runtime.ShutdownTimeout.Duration,
	}
}

func applyBandwidthLimit(ctx context.Context, cfg config.Config, limiter bandwidth.Limiter, checkNETAdmin func() error) (func(), error) {
	if !cfg.Bandwidth.Enabled {
		return func() {}, nil
	}
	if err := checkNETAdmin(); err != nil {
		return nil, err
	}

	if err := limiter.Apply(ctx, cfg.Network.BindPort); err != nil {
		return nil, fmt.Errorf("配置上传限速失败: %w", err)
	}
	return func() {
		clearCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := limiter.Clear(clearCtx); err != nil {
			log.Printf("清理上传限速规则失败: %v", err)
		}
	}, nil
}

func notifySocketPath() string {
	if socketPath := os.Getenv("HATH_NATMAP_NOTIFY_SOCKET"); socketPath != "" {
		return socketPath
	}
	return natmap.DefaultNotifySocket
}

func ensureNotifySocketDir(socketPath string) error {
	dir := filepath.Dir(socketPath)
	if dir == "." || dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("natmap notify 运行目录不是目录: %s", dir)
		}
		if dir == filepath.Dir(natmap.DefaultNotifySocket) {
			if err := os.Chmod(dir, 0o700); err != nil {
				return fmt.Errorf("设置 natmap notify 运行目录权限失败: %w", err)
			}
		}
		return ensureNotifySocketDirWritable(dir)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("检查 natmap notify 运行目录失败: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建 natmap notify 运行目录失败: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("设置 natmap notify 运行目录权限失败: %w", err)
	}
	return nil
}

func ensureNotifySocketDirWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".notify-write-test-*")
	if err != nil {
		return fmt.Errorf("natmap notify 运行目录不可写: %w", err)
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return fmt.Errorf("关闭 natmap notify 运行目录写入探针失败: %w", err)
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("清理 natmap notify 运行目录写入探针失败: %w", err)
	}
	return nil
}

func buildUPnPConfig(cfg config.Config) upnp.Config {
	return upnp.Config{
		Port:          cfg.Network.BindPort,
		LeaseDuration: uint32(cfg.UPnP.LeaseDuration),
		Description:   cfg.UPnP.Description,
	}
}
