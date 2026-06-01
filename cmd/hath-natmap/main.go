package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/ngnlAYY/hath-with-natter/internal/config"
	"github.com/ngnlAYY/hath-with-natter/internal/ehentai"
	"github.com/ngnlAYY/hath-with-natter/internal/hath"
	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
	"github.com/ngnlAYY/hath-with-natter/internal/process"
	"github.com/ngnlAYY/hath-with-natter/internal/supervisor"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	configPath := flag.String("config", config.DefaultConfigPath, "配置文件路径")
	flag.Parse()
	args := flag.Args()
	if len(args) > 0 && args[0] == "notify" {
		if err := natmap.SendNotify(notifySocketPath(), args[1:]); err != nil {
			log.Fatalf("发送 natmap notify 事件失败: %v", err)
		}
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	if err := os.MkdirAll("/run/hath-natmap", 0o700); err != nil {
		log.Fatalf("创建运行目录失败: %v", err)
	}
	if err := os.Chmod("/run/hath-natmap", 0o700); err != nil {
		log.Fatalf("设置运行目录权限失败: %v", err)
	}
	notifyToken, err := natmap.GenerateNotifyToken()
	if err != nil {
		log.Fatalf("%v", err)
	}
	listener, events, err := natmap.ListenNotifyWithToken(notifySocketPath(), notifyToken)
	if err != nil {
		log.Fatalf("启动 natmap notify socket 失败: %v", err)
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
			log.Fatalf("解析代理 URL 失败: %v", err)
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
		RetryDelay:      cfg.Runtime.Retry.InitialDelay.Duration,
		RestartDelay:    cfg.Runtime.RestartDelay.Duration,
		ShutdownTimeout: cfg.Runtime.ShutdownTimeout.Duration,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runtime.Run(ctx); err != nil {
		log.Fatalf("运行失败: %v", err)
	}
}

func notifySocketPath() string {
	if socketPath := os.Getenv("HATH_NATMAP_NOTIFY_SOCKET"); socketPath != "" {
		return socketPath
	}
	return natmap.DefaultNotifySocket
}
