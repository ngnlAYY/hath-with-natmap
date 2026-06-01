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
	"syscall"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/bandwidth"
	"github.com/ngnlAYY/hath-with-natter/internal/config"
	"github.com/ngnlAYY/hath-with-natter/internal/ehentai"
	"github.com/ngnlAYY/hath-with-natter/internal/hath"
	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
	"github.com/ngnlAYY/hath-with-natter/internal/process"
	"github.com/ngnlAYY/hath-with-natter/internal/supervisor"
)

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

	clearBandwidth, err := applyBandwidthLimit(ctx, cfg, bandwidth.Limiter{Interface: cfg.Bandwidth.Interface, UploadLimit: cfg.Bandwidth.UploadLimit}, bandwidth.CheckNETAdmin)
	if err != nil {
		return err
	}
	defer clearBandwidth()

	if err := os.MkdirAll("/run/hath-natmap", 0o700); err != nil {
		return fmt.Errorf("创建运行目录失败: %w", err)
	}
	if err := os.Chmod("/run/hath-natmap", 0o700); err != nil {
		return fmt.Errorf("设置运行目录权限失败: %w", err)
	}
	notifyToken, err := natmap.GenerateNotifyToken()
	if err != nil {
		return err
	}
	listener, events, err := natmap.ListenNotifyWithToken(notifySocketPath(), notifyToken)
	if err != nil {
		return fmt.Errorf("启动 natmap notify socket 失败: %w", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			log.Printf("关闭 natmap notify socket 失败: %v", err)
		}
	}()

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
		},
		Runner:   runner,
		Listener: listener,
	}
	hathController := &hath.Controller{
		Config: hath.Config{
			BinaryPath:          cfg.Hath.BinaryPath,
			DataDir:             cfg.Hath.DataDir,
			LogLevel:            cfg.Hath.LogLevel,
			ForceBackgroundScan: cfg.Hath.ForceBackgroundScan,
			RPCServerIP:         cfg.Hath.RPCServerIP,
			ProxyURL:            cfg.Proxy.URL,
			UseProxy:            cfg.Proxy.UseForHathDownloads,
			ClientID:            cfg.EHentai.ClientID,
			ClientKey:           cfg.EHentai.ClientKey,
		},
		Runner: runner,
	}

	updaterHTTPClient := http.DefaultClient
	if cfg.Proxy.Enabled {
		proxyURL, err := url.Parse(cfg.Proxy.URL)
		if err != nil {
			return fmt.Errorf("解析代理 URL 失败: %w", err)
		}
		updaterHTTPClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	}
	updater := ehentai.Client{
		HTTPClient: updaterHTTPClient,
		MemberID:   cfg.EHentai.MemberID,
		PassHash:   cfg.EHentai.PassHash,
		ClientID:   cfg.EHentai.ClientID,
	}
	runtime := supervisor.Runtime{
		Natmap:          natmapRunner,
		Hath:            hathController,
		Updater:         updater,
		Events:          events,
		BindPort:        cfg.Network.BindPort,
		RetryDelay:      cfg.Runtime.Retry.InitialDelay.Duration,
		RestartDelay:    cfg.Runtime.RestartDelay.Duration,
		ShutdownTimeout: cfg.Runtime.ShutdownTimeout.Duration,
	}
	if err := runtime.Run(ctx); err != nil {
		return fmt.Errorf("运行失败: %w", err)
	}
	return nil
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
