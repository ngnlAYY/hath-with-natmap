package supervisor

import (
	"context"
	"fmt"
	"log"

	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
)

type HathController interface {
	Start(ctx context.Context, port int) error
	Stop(ctx context.Context) error
	Running() bool
}

type PortUpdater interface {
	UpdatePort(ctx context.Context, port int) error
}

type Coordinator struct {
	Hath           HathController
	Updater        PortUpdater
	CurrentMapping natmap.Mapping
}

func (c *Coordinator) HandleMapping(ctx context.Context, mapping natmap.Mapping) error {
	if c.CurrentMapping.SamePublicEndpoint(mapping) {
		if c.Hath.Running() {
			log.Printf("映射未变化且 hath-rust 正在运行，跳过处理")
			return nil
		}

		log.Printf("映射未变化但 hath-rust 未运行，使用本地端口 %d 启动", mapping.PrivatePort)
		if err := c.Hath.Start(ctx, mapping.PrivatePort); err != nil {
			return fmt.Errorf("启动 hath-rust 失败: %w", err)
		}
		return nil
	}

	if c.Hath.Running() {
		log.Printf("映射变化，先停止 hath-rust")
		if err := c.Hath.Stop(ctx); err != nil {
			return fmt.Errorf("停止 hath-rust 失败: %w", err)
		}
	}

	log.Printf("更新 H@H 公网端口为 %d", mapping.PublicPort)
	if err := c.Updater.UpdatePort(ctx, mapping.PublicPort); err != nil {
		return fmt.Errorf("更新 H@H 公网端口失败: %w", err)
	}

	log.Printf("使用本地端口 %d 启动 hath-rust", mapping.PrivatePort)
	if err := c.Hath.Start(ctx, mapping.PrivatePort); err != nil {
		return fmt.Errorf("启动 hath-rust 失败: %w", err)
	}

	c.CurrentMapping = mapping
	return nil
}
