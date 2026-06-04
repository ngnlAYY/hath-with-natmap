package config_test

import (
	config "github.com/ngnlAYY/hath-with-natter/internal/config"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("写入可执行文件失败: %v", err)
	}
	return path
}

func validConfigYAML(t *testing.T, dir string) string {
	t.Helper()
	natmapPath := writeExecutable(t, dir, "natmap")
	hathPath := writeExecutable(t, dir, "hath-rust")
	dataDir := filepath.Join(dir, "hath")
	return `ehentai:
  member_id: "123456"
  pass_hash: "example-pass-hash"
  client_id: "12345"
  client_key: "example-client-key"
network:
  bind_port: 4567
  external_update_timeout: 60s
natmap:
  binary_path: "` + natmapPath + `"
  stun_server: stun.nextcloud.com:3478
  http_keepalive_server: www.baidu.com:80
  keepalive_interval: 15s
  notify_script: /usr/local/bin/natmap-notify.sh
hath:
  binary_path: "` + hathPath + `"
  data_dir: "` + dataDir + `"
  log_level: info
  force_background_scan: true
  rpc_server_ip: ""
proxy:
  enabled: true
  url: http://127.0.0.1:8080
  use_for_hath_downloads: false
bandwidth:
  enabled: false
  upload_limit: 10mbit
  interface: eth0
runtime:
  shutdown_timeout: 30s
  restart_delay: 5s
  retry:
    initial_delay: 5s
    max_delay: 5m
`
}

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(validConfigYAML(t, dir)), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() 返回错误: %v", err)
	}

	if cfg.Network.BindPort != 4567 {
		t.Fatalf("BindPort = %d, want 4567", cfg.Network.BindPort)
	}
	if cfg.Runtime.Retry.MaxDelay.Duration != 5*time.Minute {
		t.Fatalf("MaxDelay = %v, want 5m", cfg.Runtime.Retry.MaxDelay.Duration)
	}
}

func TestLoadOldConfigWithoutExternalUpdateTimeoutStillLoads(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "  external_update_timeout: 60s\n", "", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Network.ExternalUpdateTimeout.Duration != 60*time.Second {
		t.Fatalf("ExternalUpdateTimeout = %v, want 60s", cfg.Network.ExternalUpdateTimeout.Duration)
	}
}

func TestLoadEHentaiSkipPortUpdate(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "  client_key: \"example-client-key\"\n", "  client_key: \"example-client-key\"\n  skip_port_update: true\n", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if !cfg.EHentai.SkipPortUpdate {
		t.Fatal("EHentai.SkipPortUpdate = false, want true")
	}
}

func TestLoadSkipPortUpdateDoesNotRequireEHentaiCookie(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), `member_id: "123456"`, `member_id: ""`, 1)
	cfgText = strings.Replace(cfgText, `pass_hash: "example-pass-hash"`, `pass_hash: ""`, 1)
	cfgText = strings.Replace(cfgText, "  client_key: \"example-client-key\"\n", "  client_key: \"example-client-key\"\n  skip_port_update: true\n", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if !cfg.EHentai.SkipPortUpdate {
		t.Fatal("EHentai.SkipPortUpdate = false, want true")
	}
}

func TestLoadBandwidthAllowReplaceRootQdisc(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "  interface: eth0\n", "  interface: eth0\n  allow_replace_root_qdisc: true\n", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if !cfg.Bandwidth.AllowReplaceRootQdisc {
		t.Fatal("Bandwidth.AllowReplaceRootQdisc = false, want true")
	}
}

func TestLoadParameterWhitelistConfig(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "notify_script: /usr/local/bin/natmap-notify.sh", `notify_script: /usr/local/bin/natmap-notify.sh
  address_family: ipv6
  udp_mode: true
  interface: eth0
  fwmark: "0x1"
  udp_check_cycle: 12`, 1)
	cfgText = strings.Replace(cfgText, `rpc_server_ip: ""`, `rpc_server_ip: ""
  disable_logging: true
  flush_log: true
  max_connection: 128
  disable_ip_origin_check: true
  disable_flood_control: true
  enable_metrics: true
  disable_server_header: true
  enable_h3: true`, 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	if cfg.Natmap.AddressFamily != "ipv6" {
		t.Fatalf("Natmap.AddressFamily = %q, want ipv6", cfg.Natmap.AddressFamily)
	}
	if !cfg.Natmap.UDPMode {
		t.Fatal("Natmap.UDPMode = false, want true")
	}
	if cfg.Natmap.Interface != "eth0" {
		t.Fatalf("Natmap.Interface = %q, want eth0", cfg.Natmap.Interface)
	}
	if cfg.Natmap.FWMark != "0x1" {
		t.Fatalf("Natmap.FWMark = %q, want 0x1", cfg.Natmap.FWMark)
	}
	if cfg.Natmap.UDPCheckCycle != 12 {
		t.Fatalf("Natmap.UDPCheckCycle = %d, want 12", cfg.Natmap.UDPCheckCycle)
	}
	if !cfg.Hath.DisableLogging || !cfg.Hath.FlushLog || cfg.Hath.MaxConnection != 128 || !cfg.Hath.DisableIPOriginCheck || !cfg.Hath.DisableFloodControl || !cfg.Hath.EnableMetrics || !cfg.Hath.DisableServerHeader || !cfg.Hath.EnableH3 {
		t.Fatalf("Hath whitelist config not loaded correctly: %+v", cfg.Hath)
	}
}

func TestLoadDefaultsMappingModeToNatmap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(validConfigYAML(t, dir)), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Mapping.Mode != "natmap" {
		t.Fatalf("Mapping.Mode = %q, want natmap", cfg.Mapping.Mode)
	}
}

func TestLoadUPnPModeDoesNotRequireNatmapFields(t *testing.T) {
	dir := t.TempDir()
	cfgText := validConfigYAML(t, dir)
	cfgText = regexp.MustCompile(`(?m)^  binary_path: .+\n`).ReplaceAllString(cfgText, "")
	cfgText = strings.Replace(cfgText, "hath:\n", "hath:\n  binary_path: \""+filepath.Join(dir, "hath-rust")+"\"\n", 1)
	cfgText = strings.Replace(cfgText, "natmap:\n", "mapping:\n  mode: upnp\nupnp:\n  lease_duration: 30\n  description: upnp-test\nnatmap:\n", 1)
	cfgText = strings.Replace(cfgText, "  stun_server: stun.nextcloud.com:3478\n", "", 1)
	cfgText = strings.Replace(cfgText, "  http_keepalive_server: www.baidu.com:80\n", "", 1)
	cfgText = strings.Replace(cfgText, "  keepalive_interval: 15s\n", "", 1)
	cfgText = strings.Replace(cfgText, "  notify_script: /usr/local/bin/natmap-notify.sh\n", "", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Mapping.Mode != "upnp" {
		t.Fatalf("Mapping.Mode = %q, want upnp", cfg.Mapping.Mode)
	}
}

func TestLoadUPnPModeIgnoresNatmapOnlyFields(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "natmap:\n", `mapping:
  mode: upnp
upnp:
  lease_duration: 30
  description: upnp-test
natmap:
  address_family: ipx
  interface: 'eth 0'
  fwmark: mark
  udp_check_cycle: -1
`, 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Mapping.Mode != "upnp" {
		t.Fatalf("Mapping.Mode = %q, want upnp", cfg.Mapping.Mode)
	}
}

func TestValidateRejectsInvalidMappingMode(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "natmap:\n", "mapping:\n  mode: pcp\nnatmap:\n", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "mapping.mode") {
		t.Fatalf("config.Load() error = %v, want mapping.mode validation error", err)
	}
}

func TestValidateRejectsInvalidUPnPConfig(t *testing.T) {
	tests := []struct {
		name        string
		replaceText string
		wantError   string
	}{
		{
			name: "negative lease duration",
			replaceText: `mapping:
  mode: upnp
upnp:
  lease_duration: -1
  description: upnp-test
natmap:
`,
			wantError: "upnp.lease_duration",
		},
		{
			name: "lease duration exceeds uint32",
			replaceText: `mapping:
  mode: upnp
upnp:
  lease_duration: 4294967296
  description: upnp-test
natmap:
`,
			wantError: "upnp.lease_duration",
		},
		{
			name: "empty description",
			replaceText: `mapping:
  mode: upnp
upnp:
  lease_duration: 30
  description: ""
natmap:
`,
			wantError: "upnp.description",
		},
		{
			name: "missing description",
			replaceText: `mapping:
  mode: upnp
upnp:
  lease_duration: 0
natmap:
`,
			wantError: "upnp.description",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgText := strings.Replace(validConfigYAML(t, dir), "natmap:\n", tt.replaceText, 1)
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
				t.Fatalf("写入配置失败: %v", err)
			}

			_, err := config.Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("config.Load() error = %v, want %s validation error", err, tt.wantError)
			}
		})
	}
}

func TestValidateRejectsMissingSecret(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), `pass_hash: "example-pass-hash"`, `pass_hash: ""`, 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "ehentai.pass_hash") {
		t.Fatalf("config.Load() error = %v, want ehentai.pass_hash validation error", err)
	}
}

func TestValidateRejectsInvalidPort(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "bind_port: 4567", "bind_port: 70000", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "network.bind_port") {
		t.Fatalf("config.Load() error = %v, want network.bind_port validation error", err)
	}
}

func TestValidateRejectsEnabledProxyWithoutURL(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "url: http://127.0.0.1:8080", "url: ''", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "proxy.url") {
		t.Fatalf("config.Load() error = %v, want proxy.url validation error", err)
	}
}

func TestValidateRequiresProxyURLWhenUsedForHathDownloads(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "  enabled: true", "  enabled: false", 1)
	cfgText = strings.Replace(cfgText, "  url: http://127.0.0.1:8080", "  url: ''", 1)
	cfgText = strings.Replace(cfgText, "  use_for_hath_downloads: false", "  use_for_hath_downloads: true", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "proxy.url") {
		t.Fatalf("config.Load() error = %v, want proxy.url validation error", err)
	}
}

func TestValidateRejectsProxyURLWithCredentials(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "url: http://127.0.0.1:8080", "url: http://user:pass@127.0.0.1:8080", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "proxy.url 不能包含用户名或密码") {
		t.Fatalf("config.Load() error = %v, want proxy.url credential validation error", err)
	}
}

func TestValidateRejectsUnsupportedProxyScheme(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "url: http://127.0.0.1:8080", "url: ftp://127.0.0.1:8080", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "http、https 或 socks5") {
		t.Fatalf("config.Load() error = %v, want unsupported proxy scheme validation error", err)
	}
}

func TestValidateRejectsInvalidBandwidthLimit(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "enabled: false", "enabled: true", 1)
	cfgText = strings.Replace(cfgText, "upload_limit: 10mbit", "upload_limit: '10; rm -rf /'", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "bandwidth.upload_limit") {
		t.Fatalf("config.Load() error = %v, want bandwidth.upload_limit validation error", err)
	}
}

func TestValidateRejectsInvalidBandwidthInterface(t *testing.T) {
	tests := []string{"eth0;rm", "-eth0"}
	for _, iface := range tests {
		t.Run(iface, func(t *testing.T) {
			dir := t.TempDir()
			cfgText := strings.Replace(validConfigYAML(t, dir), "enabled: false", "enabled: true", 1)
			cfgText = strings.Replace(cfgText, "interface: eth0", "interface: '"+iface+"'", 1)
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
				t.Fatalf("写入配置失败: %v", err)
			}

			_, err := config.Load(path)
			if err == nil || !strings.Contains(err.Error(), "bandwidth.interface") {
				t.Fatalf("config.Load() error = %v, want bandwidth.interface validation error", err)
			}
		})
	}
}

func TestValidateRejectsInvalidNatmapAddressFamily(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "notify_script: /usr/local/bin/natmap-notify.sh", "notify_script: /usr/local/bin/natmap-notify.sh\n  address_family: ipx", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "natmap.address_family") {
		t.Fatalf("config.Load() error = %v, want natmap.address_family validation error", err)
	}
}

func TestLoadAcceptsValidNatmapInterface(t *testing.T) {
	tests := []string{"eth0", "192.168.1.2", "2001:db8::1"}
	for _, iface := range tests {
		t.Run(iface, func(t *testing.T) {
			dir := t.TempDir()
			cfgText := strings.Replace(validConfigYAML(t, dir), "notify_script: /usr/local/bin/natmap-notify.sh", "notify_script: /usr/local/bin/natmap-notify.sh\n  interface: '"+iface+"'", 1)
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
				t.Fatalf("写入配置失败: %v", err)
			}

			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("config.Load() error = %v", err)
			}
			if cfg.Natmap.Interface != iface {
				t.Fatalf("Natmap.Interface = %q, want %q", cfg.Natmap.Interface, iface)
			}
		})
	}
}

func TestValidateRejectsInvalidNatmapInterface(t *testing.T) {
	tests := []string{"eth0;rm", "eth 0", "-eth0"}
	for _, iface := range tests {
		t.Run(iface, func(t *testing.T) {
			dir := t.TempDir()
			cfgText := strings.Replace(validConfigYAML(t, dir), "notify_script: /usr/local/bin/natmap-notify.sh", "notify_script: /usr/local/bin/natmap-notify.sh\n  interface: '"+iface+"'", 1)
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
				t.Fatalf("写入配置失败: %v", err)
			}

			_, err := config.Load(path)
			if err == nil || !strings.Contains(err.Error(), "natmap.interface") {
				t.Fatalf("config.Load() error = %v, want natmap.interface validation error", err)
			}
		})
	}
}

func TestLoadAcceptsValidNatmapFWMark(t *testing.T) {
	tests := []string{"10", "010", "0x10"}
	for _, fwmark := range tests {
		t.Run(fwmark, func(t *testing.T) {
			dir := t.TempDir()
			cfgText := strings.Replace(validConfigYAML(t, dir), "notify_script: /usr/local/bin/natmap-notify.sh", "notify_script: /usr/local/bin/natmap-notify.sh\n  fwmark: '"+fwmark+"'", 1)
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
				t.Fatalf("写入配置失败: %v", err)
			}

			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("config.Load() error = %v", err)
			}
			if cfg.Natmap.FWMark != fwmark {
				t.Fatalf("Natmap.FWMark = %q, want %q", cfg.Natmap.FWMark, fwmark)
			}
		})
	}
}

func TestValidateRejectsInvalidNatmapFWMark(t *testing.T) {
	const wantError = "natmap.fwmark 必须是十进制、八进制或 0x 十六进制无符号整数"
	tests := []string{"0x", "0xzz", "1;rm", "mark"}
	for _, fwmark := range tests {
		t.Run(fwmark, func(t *testing.T) {
			dir := t.TempDir()
			cfgText := strings.Replace(validConfigYAML(t, dir), "notify_script: /usr/local/bin/natmap-notify.sh", "notify_script: /usr/local/bin/natmap-notify.sh\n  fwmark: '"+fwmark+"'", 1)
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
				t.Fatalf("写入配置失败: %v", err)
			}

			_, err := config.Load(path)
			if err == nil || err.Error() != wantError {
				t.Fatalf("config.Load() error = %v, want %q", err, wantError)
			}
		})
	}
}

func TestValidateRejectsInvalidNatmapUDPCheckCycle(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "notify_script: /usr/local/bin/natmap-notify.sh", "notify_script: /usr/local/bin/natmap-notify.sh\n  udp_check_cycle: -1", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "natmap.udp_check_cycle") {
		t.Fatalf("config.Load() error = %v, want natmap.udp_check_cycle validation error", err)
	}
}

func TestValidateRejectsInvalidNatmapKeepaliveIntervalPrecision(t *testing.T) {
	tests := []string{"500ms", "1500ms"}
	for _, keepaliveInterval := range tests {
		t.Run(keepaliveInterval, func(t *testing.T) {
			dir := t.TempDir()
			cfgText := strings.Replace(validConfigYAML(t, dir), "keepalive_interval: 15s", "keepalive_interval: "+keepaliveInterval, 1)
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
				t.Fatalf("写入配置失败: %v", err)
			}

			_, err := config.Load(path)
			if err == nil || !strings.Contains(err.Error(), "natmap.keepalive_interval") {
				t.Fatalf("config.Load() error = %v, want natmap.keepalive_interval validation error", err)
			}
		})
	}
}

func TestValidateRejectsNegativeHathMaxConnection(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), `rpc_server_ip: ""`, "rpc_server_ip: \"\"\n  max_connection: -1", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "hath.max_connection") {
		t.Fatalf("config.Load() error = %v, want hath.max_connection validation error", err)
	}
}

func TestEnsureWritableDirRemovesProbeFile(t *testing.T) {
	dir := t.TempDir()
	cfgText := validConfigYAML(t, dir)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	probe := filepath.Join(cfg.Hath.DataDir, ".write-test")
	if _, err := os.Stat(probe); !os.IsNotExist(err) {
		t.Fatalf("probe stat error = %v, want not exist", err)
	}
}
