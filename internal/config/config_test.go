package config

import (
	"os"
	"path/filepath"
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

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() 返回错误: %v", err)
	}

	if cfg.Network.BindPort != 4567 {
		t.Fatalf("BindPort = %d, want 4567", cfg.Network.BindPort)
	}
	if cfg.Runtime.Retry.MaxDelay.Duration != 5*time.Minute {
		t.Fatalf("MaxDelay = %v, want 5m", cfg.Runtime.Retry.MaxDelay.Duration)
	}
}

func TestValidateRejectsMissingSecret(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), `pass_hash: "example-pass-hash"`, `pass_hash: ""`, 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "ehentai.pass_hash") {
		t.Fatalf("Load() error = %v, want ehentai.pass_hash validation error", err)
	}
}

func TestValidateRejectsInvalidPort(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "bind_port: 4567", "bind_port: 70000", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "network.bind_port") {
		t.Fatalf("Load() error = %v, want network.bind_port validation error", err)
	}
}

func TestValidateRejectsEnabledProxyWithoutURL(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "url: http://127.0.0.1:8080", "url: ''", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "proxy.url") {
		t.Fatalf("Load() error = %v, want proxy.url validation error", err)
	}
}

func TestValidateRejectsUnsupportedProxyScheme(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "url: http://127.0.0.1:8080", "url: ftp://127.0.0.1:8080", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "http、https 或 socks5") {
		t.Fatalf("Load() error = %v, want unsupported proxy scheme validation error", err)
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

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "bandwidth.upload_limit") {
		t.Fatalf("Load() error = %v, want bandwidth.upload_limit validation error", err)
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

			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "bandwidth.interface") {
				t.Fatalf("Load() error = %v, want bandwidth.interface validation error", err)
			}
		})
	}
}

func TestEnsureWritableDirRemovesProbeFile(t *testing.T) {
	dir := t.TempDir()
	cfgText := validConfigYAML(t, dir)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	probe := filepath.Join(cfg.Hath.DataDir, ".write-test")
	if _, err := os.Stat(probe); !os.IsNotExist(err) {
		t.Fatalf("probe stat error = %v, want not exist", err)
	}
}
