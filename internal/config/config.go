package config

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigPath            = "/config/config.yaml"
	defaultExternalUpdateTimeout = 60 * time.Second
	defaultMappingMode           = "natmap"
	mappingModeNatmap            = "natmap"
	mappingModeUPnP              = "upnp"
	maxUPnPDescriptionLength     = 64
)

var (
	bandwidthInterfacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,14}$`)
	bandwidthLimitPattern     = regexp.MustCompile(`^[1-9][0-9]*(bit|kbit|mbit|gbit)$`)
	natmapInterfacePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)
)

type Duration struct {
	time.Duration
	isSet bool
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
	d.isSet = true
	return nil
}

type Config struct {
	EHentai   EHentaiConfig   `yaml:"ehentai"`
	Network   NetworkConfig   `yaml:"network"`
	Mapping   MappingConfig   `yaml:"mapping"`
	UPnP      UPnPConfig      `yaml:"upnp"`
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

type MappingConfig struct {
	Mode string `yaml:"mode"`
}

type UPnPConfig struct {
	LeaseDuration int    `yaml:"lease_duration"`
	Description   string `yaml:"description"`
}

type NatmapConfig struct {
	BinaryPath          string   `yaml:"binary_path"`
	StunServer          string   `yaml:"stun_server"`
	HTTPKeepaliveServer string   `yaml:"http_keepalive_server"`
	KeepaliveInterval   Duration `yaml:"keepalive_interval"`
	NotifyScript        string   `yaml:"notify_script"`
	AddressFamily       string   `yaml:"address_family"`
	UDPMode             bool     `yaml:"udp_mode"`
	Interface           string   `yaml:"interface"`
	FWMark              string   `yaml:"fwmark"`
	UDPCheckCycle       int      `yaml:"udp_check_cycle"`
}

type HathConfig struct {
	BinaryPath           string `yaml:"binary_path"`
	DataDir              string `yaml:"data_dir"`
	LogLevel             string `yaml:"log_level"`
	ForceBackgroundScan  bool   `yaml:"force_background_scan"`
	RPCServerIP          string `yaml:"rpc_server_ip"`
	DisableLogging       bool   `yaml:"disable_logging"`
	FlushLog             bool   `yaml:"flush_log"`
	MaxConnection        int    `yaml:"max_connection"`
	DisableIPOriginCheck bool   `yaml:"disable_ip_origin_check"`
	DisableFloodControl  bool   `yaml:"disable_flood_control"`
	EnableMetrics        bool   `yaml:"enable_metrics"`
	DisableServerHeader  bool   `yaml:"disable_server_header"`
	EnableH3             bool   `yaml:"enable_h3"`
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
	if !cfg.Network.ExternalUpdateTimeout.isSet {
		cfg.Network.ExternalUpdateTimeout.Duration = defaultExternalUpdateTimeout
	}
	if cfg.Mapping.Mode == "" {
		cfg.Mapping.Mode = defaultMappingMode
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
	if err := validateMappingMode(c.Mapping.Mode); err != nil {
		return err
	}
	if c.Mapping.Mode == mappingModeUPnP {
		if err := validateUPnPConfig(c.UPnP); err != nil {
			return err
		}
	}
	if c.Mapping.Mode == mappingModeNatmap {
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
		if err := validateNatmapAddressFamily(c.Natmap.AddressFamily); err != nil {
			return err
		}
		if err := validateNatmapInterface(c.Natmap.Interface); err != nil {
			return err
		}
		if err := validateNatmapFWMark(c.Natmap.FWMark); err != nil {
			return err
		}
		if c.Natmap.UDPCheckCycle < 0 {
			return fmt.Errorf("natmap.udp_check_cycle 不能小于 0")
		}
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
	if c.Hath.MaxConnection < 0 {
		return fmt.Errorf("hath.max_connection 必须大于等于 0")
	}
	if c.Proxy.Enabled || c.Proxy.UseForHathDownloads {
		if err := requireString("proxy.url", c.Proxy.URL); err != nil {
			return err
		}
		parsed, err := url.Parse(c.Proxy.URL)
		if err != nil || parsed.Host == "" || !isAllowedProxyScheme(parsed.Scheme) {
			return fmt.Errorf("proxy.url 必须是合法的 http、https 或 socks5 代理 URL")
		}
		if parsed.User != nil {
			return fmt.Errorf("proxy.url 不能包含用户名或密码")
		}
	}
	if c.Bandwidth.Enabled {
		if err := validateBandwidthLimit(c.Bandwidth.UploadLimit); err != nil {
			return err
		}
		if err := validateBandwidthInterface(c.Bandwidth.Interface); err != nil {
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
	if err := os.Remove(probe); err != nil {
		return fmt.Errorf("清理 %s 写入探针失败: %w", name, err)
	}
	return nil
}

func isAllowedProxyScheme(scheme string) bool {
	switch scheme {
	case "http", "https", "socks5":
		return true
	default:
		return false
	}
}

func validateBandwidthLimit(limit string) error {
	if !bandwidthLimitPattern.MatchString(limit) {
		return fmt.Errorf("bandwidth.upload_limit 必须是正整数加 bit、kbit、mbit 或 gbit 单位")
	}
	return nil
}

func validateMappingMode(mode string) error {
	switch mode {
	case mappingModeNatmap, mappingModeUPnP:
		return nil
	default:
		return fmt.Errorf("mapping.mode 必须是 %q 或 %q", mappingModeNatmap, mappingModeUPnP)
	}
}

func validateUPnPConfig(cfg UPnPConfig) error {
	if cfg.LeaseDuration < 0 {
		return fmt.Errorf("upnp.lease_duration 不能小于 0")
	}
	if cfg.LeaseDuration > math.MaxUint32 {
		return fmt.Errorf("upnp.lease_duration 不能大于 %d", uint64(math.MaxUint32))
	}
	if cfg.Description == "" {
		return fmt.Errorf("upnp.description 不能为空")
	}
	if len(cfg.Description) > maxUPnPDescriptionLength {
		return fmt.Errorf("upnp.description 不能超过 %d 字节", maxUPnPDescriptionLength)
	}
	return nil
}

func validateBandwidthInterface(name string) error {
	if !bandwidthInterfacePattern.MatchString(name) {
		return fmt.Errorf("bandwidth.interface 必须是 1 到 15 位的网络接口名")
	}
	return nil
}

func validateNatmapAddressFamily(addressFamily string) error {
	switch addressFamily {
	case "", "ipv4", "ipv6":
		return nil
	default:
		return fmt.Errorf("natmap.address_family 必须是 ipv4 或 ipv6")
	}
}

func validateNatmapInterface(value string) error {
	if value == "" {
		return nil
	}
	if ip := net.ParseIP(value); ip != nil {
		return nil
	}
	if !natmapInterfacePattern.MatchString(value) || strings.Contains(value, "..") {
		return fmt.Errorf("natmap.interface 必须是合法的网卡名或 IP 地址")
	}
	return nil
}

func validateNatmapFWMark(value string) error {
	if value == "" {
		return nil
	}
	if _, err := strconv.ParseUint(value, 0, 32); err != nil {
		return fmt.Errorf("natmap.fwmark 必须是十进制、八进制或 0x 十六进制无符号整数")
	}
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
