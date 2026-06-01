package supervisor

import (
	"context"
	"fmt"
	"log"
	"time"

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

type NatmapProcess interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Done() <-chan error
}

type Runtime struct {
	Natmap          NatmapProcess
	Hath            HathController
	Updater         PortUpdater
	Events          <-chan natmap.Mapping
	BindPort        int
	RetryDelay      time.Duration
	RestartDelay    time.Duration
	ShutdownTimeout time.Duration
}

type Coordinator struct {
	Hath           HathController
	Updater        PortUpdater
	CurrentMapping natmap.Mapping
	BindPort       int
}

func (r *Runtime) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}

	coordinator := &Coordinator{Hath: r.Hath, Updater: r.Updater, BindPort: r.BindPort}
	for {
		if ctx.Err() != nil {
			r.stopAllWithTimeout()
			return nil
		}

		log.Printf("启动 natmap 进程")
		if err := r.Natmap.Start(ctx); err != nil {
			log.Printf("启动 natmap 失败，稍后重试: %v", err)
			if !sleep(ctx, r.retryDelay()) {
				r.stopAllWithTimeout()
				return nil
			}
			continue
		}

		if err := r.runUntilRestart(ctx, coordinator); err != nil {
			log.Printf("运行期错误，准备自动恢复: %v", err)
		}
		if ctx.Err() != nil {
			r.stopAllWithTimeout()
			return nil
		}
		r.stopAll(ctx)
		if !sleep(ctx, r.restartDelay()) {
			r.stopAllWithTimeout()
			return nil
		}
	}
}

func (r *Runtime) validate() error {
	if r == nil {
		return fmt.Errorf("runtime 未初始化")
	}
	if r.Natmap == nil {
		return fmt.Errorf("natmap 进程未配置")
	}
	if r.Hath == nil {
		return fmt.Errorf("hath 控制器未配置")
	}
	if r.Updater == nil {
		return fmt.Errorf("端口更新器未配置")
	}
	if r.Events == nil {
		return fmt.Errorf("natmap 事件通道未配置")
	}
	if r.BindPort <= 0 || r.BindPort > 65535 {
		return fmt.Errorf("本地固定端口未配置")
	}
	return nil
}

func (r *Runtime) runUntilRestart(ctx context.Context, coordinator *Coordinator) error {
	natmapDone := r.Natmap.Done()
	if natmapDone == nil {
		return fmt.Errorf("natmap 进程未运行")
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case mapping, ok := <-r.Events:
			if !ok {
				return fmt.Errorf("natmap notify 通道已关闭")
			}
			log.Printf("收到 natmap 映射: %s:%d -> 本地端口 %d", mapping.PublicAddress, mapping.PublicPort, mapping.PrivatePort)
			if err := coordinator.HandleMapping(ctx, mapping); err != nil {
				return fmt.Errorf("处理 natmap 映射失败: %w", err)
			}
		case err, ok := <-natmapDone:
			if !ok {
				return fmt.Errorf("natmap 进程状态通道已关闭")
			}
			if err != nil {
				return fmt.Errorf("natmap 进程退出: %w", err)
			}
			return fmt.Errorf("natmap 进程退出")
		}
	}
}

func (r *Runtime) stopAll(ctx context.Context) {
	if r.Hath != nil {
		if err := r.Hath.Stop(ctx); err != nil {
			log.Printf("停止 hath-rust 失败: %v", err)
		}
	}
	if r.Natmap != nil {
		if err := r.Natmap.Stop(ctx); err != nil {
			log.Printf("停止 natmap 失败: %v", err)
		}
	}
}

func (r *Runtime) stopAllWithTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), r.shutdownTimeout())
	defer cancel()
	r.stopAll(ctx)
}

func (r *Runtime) shutdownTimeout() time.Duration {
	if r.ShutdownTimeout > 0 {
		return r.ShutdownTimeout
	}
	return 30 * time.Second
}

func (r *Runtime) retryDelay() time.Duration {
	if r.RetryDelay > 0 {
		return r.RetryDelay
	}
	return 5 * time.Second
}

func (r *Runtime) restartDelay() time.Duration {
	if r.RestartDelay > 0 {
		return r.RestartDelay
	}
	return r.retryDelay()
}

func sleep(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Coordinator) HandleMapping(ctx context.Context, mapping natmap.Mapping) error {
	if c == nil {
		return fmt.Errorf("supervisor 协调器未初始化")
	}
	if c.Hath == nil {
		return fmt.Errorf("hath 控制器未配置")
	}
	if c.Updater == nil {
		return fmt.Errorf("端口更新器未配置")
	}
	if c.BindPort <= 0 || c.BindPort > 65535 {
		return fmt.Errorf("本地固定端口未配置")
	}
	if mapping.PrivatePort != c.BindPort {
		return fmt.Errorf("natmap 本地端口 %d 与配置固定端口 %d 不一致", mapping.PrivatePort, c.BindPort)
	}

	if c.CurrentMapping.SamePublicEndpoint(mapping) {
		if c.Hath.Running() {
			log.Printf("映射未变化且 hath-rust 正在运行，跳过处理")
			return nil
		}

		log.Printf("映射未变化但 hath-rust 未运行，使用本地端口 %d 启动", c.BindPort)
		if err := c.Hath.Start(ctx, c.BindPort); err != nil {
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

	log.Printf("使用本地端口 %d 启动 hath-rust", c.BindPort)
	if err := c.Hath.Start(ctx, c.BindPort); err != nil {
		return fmt.Errorf("启动 hath-rust 失败: %w", err)
	}

	c.CurrentMapping = mapping
	return nil
}
