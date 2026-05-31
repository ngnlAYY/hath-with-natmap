package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "/config/config.yaml"

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var text string
	if err := value.Decode(&text); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return fmt.Errorf("解析时间间隔 %q 失败: %w", text, err)
	}
	d.Duration = parsed
	return nil
}

type Config struct {
	EHentai   EHentaiConfig   `yaml:"ehentai"`
	Network   NetworkConfig   `yaml:"network"`
	Natmap    NatmapConfig    `yaml:"natmap"`
	Hath      HathConfig      `yaml:"hath"`
	Proxy     ProxyConfig     `yaml:"proxy"`
	Bandwidth BandwidthConfig `yaml:"bandwidth"`
	Runtime   RuntimeConfig   `yaml:"runtime"`
}

type EHentaiConfig struct {
	MemberID  string `yaml:"member_id"`
	PassHash  string `yaml:"pass_hash"`
	ClientID  string `yaml:"client_id"`
	ClientKey string `yaml:"client_key"`
}

type NetworkConfig struct {
	BindPort              int      `yaml:"bind_port"`
	ExternalUpdateTimeout Duration `yaml:"external_update_timeout"`
}

type NatmapConfig struct {
	BinaryPath          string   `yaml:"binary_path"`
	StunServer          string   `yaml:"stun_server"`
	HTTPKeepaliveServer string   `yaml:"http_keepalive_server"`
	KeepaliveInterval   Duration `yaml:"keepalive_interval"`
	NotifyScript        string   `yaml:"notify_script"`
}

type HathConfig struct {
	BinaryPath          string `yaml:"binary_path"`
	DataDir             string `yaml:"data_dir"`
	LogLevel            string `yaml:"log_level"`
	ForceBackgroundScan bool   `yaml:"force_background_scan"`
	RPCServerIP         string `yaml:"rpc_server_ip"`
}

type ProxyConfig struct {
	Enabled             bool   `yaml:"enabled"`
	URL                 string `yaml:"url"`
	UseForHathDownloads bool   `yaml:"use_for_hath_downloads"`
}

type BandwidthConfig struct {
	Enabled     bool   `yaml:"enabled"`
	UploadLimit string `yaml:"upload_limit"`
	Interface   string `yaml:"interface"`
}

type RuntimeConfig struct {
	ShutdownTimeout Duration    `yaml:"shutdown_timeout"`
	RestartDelay    Duration    `yaml:"restart_delay"`
	Retry           RetryConfig `yaml:"retry"`
}

type RetryConfig struct {
	InitialDelay Duration `yaml:"initial_delay"`
	MaxDelay     Duration `yaml:"max_delay"`
}

func Load(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("读取配置文件失败: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return Config{}, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if err := requireString("ehentai.member_id", c.EHentai.MemberID); err != nil {
		return err
	}
	if err := requireString("ehentai.pass_hash", c.EHentai.PassHash); err != nil {
		return err
	}
	if err := requireString("ehentai.client_id", c.EHentai.ClientID); err != nil {
		return err
	}
	if err := requireString("ehentai.client_key", c.EHentai.ClientKey); err != nil {
		return err
	}
	if c.Network.BindPort < 1 || c.Network.BindPort > 65535 {
		return fmt.Errorf("network.bind_port 必须是 1 到 65535 之间的端口")
	}
	if c.Network.ExternalUpdateTimeout.Duration <= 0 {
		return fmt.Errorf("network.external_update_timeout 必须大于 0")
	}
	if err := requireExecutable("natmap.binary_path", c.Natmap.BinaryPath); err != nil {
		return err
	}
	if err := requireString("natmap.stun_server", c.Natmap.StunServer); err != nil {
		return err
	}
	if err := requireString("natmap.http_keepalive_server", c.Natmap.HTTPKeepaliveServer); err != nil {
		return err
	}
	if c.Natmap.KeepaliveInterval.Duration <= 0 {
		return fmt.Errorf("natmap.keepalive_interval 必须大于 0")
	}
	if err := requireString("natmap.notify_script", c.Natmap.NotifyScript); err != nil {
		return err
	}
	if err := requireExecutable("hath.binary_path", c.Hath.BinaryPath); err != nil {
		return err
	}
	if err := ensureWritableDir("hath.data_dir", c.Hath.DataDir); err != nil {
		return err
	}
	if err := validateLogLevel(c.Hath.LogLevel); err != nil {
		return err
	}
	if c.Proxy.Enabled {
		if err := requireString("proxy.url", c.Proxy.URL); err != nil {
			return err
		}
		parsed, err := url.Parse(c.Proxy.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("proxy.url 必须是合法代理 URL")
		}
	}
	if c.Bandwidth.Enabled {
		if err := requireString("bandwidth.upload_limit", c.Bandwidth.UploadLimit); err != nil {
			return err
		}
		if err := requireString("bandwidth.interface", c.Bandwidth.Interface); err != nil {
			return err
		}
	}
	if c.Runtime.ShutdownTimeout.Duration <= 0 {
		return fmt.Errorf("runtime.shutdown_timeout 必须大于 0")
	}
	if c.Runtime.RestartDelay.Duration <= 0 {
		return fmt.Errorf("runtime.restart_delay 必须大于 0")
	}
	if c.Runtime.Retry.InitialDelay.Duration <= 0 || c.Runtime.Retry.MaxDelay.Duration <= 0 {
		return fmt.Errorf("runtime.retry 的时间间隔必须大于 0")
	}
	if c.Runtime.Retry.InitialDelay.Duration > c.Runtime.Retry.MaxDelay.Duration {
		return fmt.Errorf("runtime.retry.initial_delay 不能大于 max_delay")
	}
	return nil
}

func requireString(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s 不能为空", name)
	}
	return nil
}

func requireExecutable(name, path string) error {
	if err := requireString(name, path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s 指向的文件不可用: %w", name, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s 必须指向可执行文件", name)
	}
	return nil
}

func ensureWritableDir(name, path string) error {
	if err := requireString(name, path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("创建 %s 失败: %w", name, err)
	}
	probe := filepath.Join(path, ".write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("%s 不可写: %w", name, err)
	}
	_ = os.Remove(probe)
	return nil
}

func validateLogLevel(level string) error {
	switch level {
	case "debug", "info", "warn", "error", "off":
		return nil
	default:
		return fmt.Errorf("hath.log_level 必须是 debug、info、warn、error 或 off")
	}
}
