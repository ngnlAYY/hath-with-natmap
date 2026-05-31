package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ngnlAYY/hath-with-natter/internal/config"
	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if len(os.Args) > 1 && os.Args[1] == "notify" {
		if err := natmap.SendNotify(notifySocketPath(), os.Args[2:]); err != nil {
			log.Fatalf("发送 natmap notify 事件失败: %v", err)
		}
		return
	}

	configPath := flag.String("config", config.DefaultConfigPath, "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	fmt.Printf("配置加载成功，监听端口: %d\n", cfg.Network.BindPort)
}

func notifySocketPath() string {
	if socketPath := os.Getenv("HATH_NATMAP_NOTIFY_SOCKET"); socketPath != "" {
		return socketPath
	}
	return natmap.DefaultNotifySocket
}
