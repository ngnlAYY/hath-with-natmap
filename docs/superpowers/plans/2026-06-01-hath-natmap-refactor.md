# Hath Natmap Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current Python/Natter project with a Go Docker-only orchestrator that runs `natmap`, updates Hentai@Home, manages `hath-rust`, and optionally configures `tc` upload limiting.

**Architecture:** The Go binary is the container entrypoint. It validates YAML config, starts `natmap` in bind mode, receives notify events through a local Unix socket helper, updates Hentai@Home only when the public endpoint changes, manages `hath-rust`, and applies `tc` rules when bandwidth limiting is enabled. External binaries remain external release artifacts bundled by Docker build.

**Tech Stack:** Go 1.23+, `gopkg.in/yaml.v3`, `golang.org/x/net/html`, Docker Buildx, Alpine Linux runtime, `iproute2`/`tc`, POSIX shell.

---

## Scope Check

The spec covers one deployable system with tightly coupled components: config, natmap integration, Hentai@Home update, hath-rust lifecycle, bandwidth limiting, Docker packaging, tests, and docs. Keep it as one plan because each task produces a working slice of the same orchestrator and each commit remains independently reviewable.

## File Structure Map

Create or replace these files:

```text
cmd/hath-natmap/main.go                 # CLI entrypoint and natmap notify subcommand
internal/config/config.go               # YAML schema, defaults, validation
internal/config/config_test.go          # Config validation tests
internal/natmap/notify.go               # natmap notify argument parsing and mapping comparison
internal/natmap/notify_test.go          # notify parser tests
internal/natmap/runner.go               # natmap command args and event runner interfaces
internal/natmap/socket.go               # Unix socket notify sender/listener
internal/natmap/runner_test.go          # natmap command and socket tests
internal/hath/client.go                 # hath-rust command args and client_login writer
internal/hath/client_test.go            # hath-rust command and file tests
internal/ehentai/settings.go            # Hentai@Home settings form fetch/parse/update
internal/ehentai/settings_test.go       # HTTP and HTML parser tests
internal/process/command.go             # OS child-process abstraction
internal/process/command_test.go        # process abstraction tests with helper subprocess
internal/bandwidth/limiter.go           # tc command construction/application/cleanup
internal/bandwidth/limiter_test.go      # tc limiter tests with fake command runner
internal/supervisor/supervisor.go       # state machine and orchestration loop
internal/supervisor/supervisor_test.go  # supervisor state transition tests
go.mod                                  # Go module definition
go.sum                                  # Go dependency checksums
configs/config.example.yaml             # New config example
scripts/natmap-notify.sh                # Shell bridge from natmap to Go notify subcommand
Dockerfile                              # Multi-stage Go build and binary bundling
README.md                               # Chinese quick start
README.legacy.md                        # Previous README preserved for one release cycle
README.md                               # New Chinese README replaces old content
docs/configuration.md                   # Chinese config reference
docs/docker.md                          # Chinese Docker build/run guide
docs/bandwidth-limit.md                 # Chinese tc bandwidth guide
docs/troubleshooting.md                 # Chinese troubleshooting guide
.github/workflows/docker-image.yml      # Buildx platform matrix and image build
```

Remove these legacy files in the final cleanup task:

```text
main.py
natter.py
requirements.txt
config.yaml.example
```

Use module path `github.com/ngnlAYY/hath-with-natter` from the current `origin` remote.

---

### Task 1: Initialize Go Module and Config Package

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Create Go module file**

Write `go.mod`:

```go
module github.com/ngnlAYY/hath-with-natter

go 1.23

require gopkg.in/yaml.v3 v3.0.1
```

- [ ] **Step 2: Write failing config tests**

Create `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 3: Run config tests and verify failure**

Run:

```bash
go test ./internal/config
```

Expected: FAIL because package `internal/config` has tests but no implementation.

- [ ] **Step 4: Implement config package**

Create `internal/config/config.go`:

```go
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
		if parsed, err := url.Parse(c.Proxy.URL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
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
```

- [ ] **Step 5: Run config tests and verify pass**

Run:

```bash
go test ./internal/config
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod internal/config/config.go internal/config/config_test.go
git commit -m "feat: add Go config loader"
```

---

### Task 2: Add Natmap Notify Parsing and Command Construction

**Files:**
- Create: `internal/natmap/notify.go`
- Create: `internal/natmap/notify_test.go`
- Create: `internal/natmap/runner.go`
- Create: `internal/natmap/runner_test.go`

- [ ] **Step 1: Write failing natmap tests**

Create `internal/natmap/notify_test.go`:

```go
package natmap

import "testing"

func TestParseNotifyArgs(t *testing.T) {
	mapping, err := ParseNotifyArgs([]string{"203.0.113.10", "45678", "ip4p", "4567", "TCP", "192.168.1.2"})
	if err != nil {
		t.Fatalf("ParseNotifyArgs() error = %v", err)
	}
	if mapping.PublicAddress != "203.0.113.10" || mapping.PublicPort != 45678 || mapping.PrivatePort != 4567 || mapping.Protocol != "TCP" {
		t.Fatalf("mapping = %+v", mapping)
	}
}

func TestParseNotifyArgsRejectsWrongCount(t *testing.T) {
	_, err := ParseNotifyArgs([]string{"203.0.113.10"})
	if err == nil {
		t.Fatal("ParseNotifyArgs() error = nil, want error")
	}
}

func TestSameEndpoint(t *testing.T) {
	left := Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, PrivatePort: 4567, Protocol: "TCP"}
	right := Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, PrivatePort: 4567, Protocol: "TCP"}
	changed := Mapping{PublicAddress: "203.0.113.10", PublicPort: 45679, PrivatePort: 4567, Protocol: "TCP"}
	if !left.SamePublicEndpoint(right) {
		t.Fatal("SamePublicEndpoint() = false, want true")
	}
	if left.SamePublicEndpoint(changed) {
		t.Fatal("SamePublicEndpoint() = true, want false")
	}
}
```

Create `internal/natmap/runner_test.go`:

```go
package natmap

import (
	"reflect"
	"testing"
	"time"
)

func TestBuildArgsUsesBindModeWithoutDaemon(t *testing.T) {
	cfg := RunnerConfig{
		BinaryPath:          "/usr/local/bin/natmap",
		BindPort:            4567,
		StunServer:          "stun.nextcloud.com:3478",
		HTTPKeepaliveServer: "www.baidu.com:80",
		KeepaliveInterval:   15 * time.Second,
		NotifyScript:        "/usr/local/bin/natmap-notify.sh",
	}
	got := cfg.Args()
	want := []string{"-4", "-b", "4567", "-s", "stun.nextcloud.com:3478", "-h", "www.baidu.com:80", "-k", "15", "-e", "/usr/local/bin/natmap-notify.sh"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}
```

- [ ] **Step 2: Run natmap tests and verify failure**

Run:

```bash
go test ./internal/natmap
```

Expected: FAIL because `ParseNotifyArgs`, `Mapping`, and `RunnerConfig` are undefined.

- [ ] **Step 3: Implement natmap notify parsing**

Create `internal/natmap/notify.go`:

```go
package natmap

import (
	"fmt"
	"strconv"
)

// Mapping 是 natmap notify 回调提供的一次公网映射结果。
type Mapping struct {
	PublicAddress  string
	PublicPort     int
	IP4P           string
	PrivatePort    int
	Protocol       string
	PrivateAddress string
}

func ParseNotifyArgs(args []string) (Mapping, error) {
	if len(args) != 6 {
		return Mapping{}, fmt.Errorf("natmap notify 参数数量为 %d，期望 6", len(args))
	}
	publicPort, err := strconv.Atoi(args[1])
	if err != nil {
		return Mapping{}, fmt.Errorf("解析公网端口失败: %w", err)
	}
	privatePort, err := strconv.Atoi(args[3])
	if err != nil {
		return Mapping{}, fmt.Errorf("解析本地端口失败: %w", err)
	}
	if args[4] != "TCP" {
		return Mapping{}, fmt.Errorf("仅支持 TCP 映射，收到协议 %q", args[4])
	}
	return Mapping{
		PublicAddress:  args[0],
		PublicPort:     publicPort,
		IP4P:           args[2],
		PrivatePort:    privatePort,
		Protocol:       args[4],
		PrivateAddress: args[5],
	}, nil
}

func (m Mapping) SamePublicEndpoint(other Mapping) bool {
	return m.PublicAddress == other.PublicAddress && m.PublicPort == other.PublicPort && m.Protocol == other.Protocol
}
```

- [ ] **Step 4: Implement natmap command args**

Create `internal/natmap/runner.go`:

```go
package natmap

import (
	"strconv"
	"time"
)

type RunnerConfig struct {
	BinaryPath          string
	BindPort            int
	StunServer          string
	HTTPKeepaliveServer string
	KeepaliveInterval   time.Duration
	NotifyScript        string
}

func (c RunnerConfig) Args() []string {
	return []string{
		"-4",
		"-b", strconv.Itoa(c.BindPort),
		"-s", c.StunServer,
		"-h", c.HTTPKeepaliveServer,
		"-k", strconv.Itoa(int(c.KeepaliveInterval.Seconds())),
		"-e", c.NotifyScript,
	}
}
```

- [ ] **Step 5: Run natmap tests and verify pass**

Run:

```bash
go test ./internal/natmap
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/natmap/notify.go internal/natmap/notify_test.go internal/natmap/runner.go internal/natmap/runner_test.go
git commit -m "feat: add natmap command parsing"
```

---

### Task 3: Add Hath-Rust Command Builder and Client Login Writer

**Files:**
- Create: `internal/hath/client.go`
- Create: `internal/hath/client_test.go`

- [ ] **Step 1: Write failing hath tests**

Create `internal/hath/client_test.go`:

```go
package hath

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildArgs(t *testing.T) {
	cfg := Config{
		BinaryPath:          "/usr/local/bin/hath-rust",
		DataDir:             "/data/hath",
		LogLevel:            "warn",
		ForceBackgroundScan: true,
		RPCServerIP:         "127.0.0.1",
		ProxyURL:            "http://127.0.0.1:8080",
		UseProxy:            true,
	}
	got := cfg.Args(4567)
	want := []string{
		"--cache-dir", "/data/hath/cache",
		"--data-dir", "/data/hath/data",
		"--download-dir", "/data/hath/download",
		"--log-dir", "/data/hath/log",
		"--temp-dir", "/data/hath/tmp",
		"--port", "4567",
		"--proxy", "http://127.0.0.1:8080",
		"--force-background-scan",
		"-qq",
		"--rpc-server-ip", "127.0.0.1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

func TestWriteClientLogin(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, ClientID: "12345", ClientKey: "secret"}
	if err := cfg.WriteClientLogin(); err != nil {
		t.Fatalf("WriteClientLogin() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "data", "client_login"))
	if err != nil {
		t.Fatalf("读取 client_login 失败: %v", err)
	}
	if string(content) != "12345-secret" {
		t.Fatalf("client_login = %q", string(content))
	}
}
```

- [ ] **Step 2: Run hath tests and verify failure**

Run:

```bash
go test ./internal/hath
```

Expected: FAIL because package has no implementation.

- [ ] **Step 3: Implement hath package**

Create `internal/hath/client.go`:

```go
package hath

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	BinaryPath          string
	DataDir             string
	LogLevel            string
	ForceBackgroundScan bool
	RPCServerIP         string
	ProxyURL            string
	UseProxy            bool
	ClientID            string
	ClientKey           string
}

func (c Config) Args(port int) []string {
	args := []string{
		"--cache-dir", filepath.Join(c.DataDir, "cache"),
		"--data-dir", filepath.Join(c.DataDir, "data"),
		"--download-dir", filepath.Join(c.DataDir, "download"),
		"--log-dir", filepath.Join(c.DataDir, "log"),
		"--temp-dir", filepath.Join(c.DataDir, "tmp"),
		"--port", strconv.Itoa(port),
	}
	if c.UseProxy {
		args = append(args, "--proxy", c.ProxyURL)
	}
	if c.ForceBackgroundScan {
		args = append(args, "--force-background-scan")
	}
	if quiet := quietFlag(c.LogLevel); quiet != "" {
		args = append(args, quiet)
	}
	if c.RPCServerIP != "" {
		args = append(args, "--rpc-server-ip", c.RPCServerIP)
	}
	return args
}

func (c Config) WriteClientLogin() error {
	path := filepath.Join(c.DataDir, "data")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("创建 hath 数据目录失败: %w", err)
	}
	content := fmt.Sprintf("%s-%s", c.ClientID, c.ClientKey)
	if err := os.WriteFile(filepath.Join(path, "client_login"), []byte(content), 0o600); err != nil {
		return fmt.Errorf("写入 client_login 失败: %w", err)
	}
	return nil
}

func quietFlag(level string) string {
	switch level {
	case "debug":
		return ""
	case "info":
		return "-q"
	case "warn":
		return "-qq"
	case "error":
		return "-qqq"
	case "off":
		return "-qqqq"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run hath tests and verify pass**

Run:

```bash
go test ./internal/hath
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hath/client.go internal/hath/client_test.go
git commit -m "feat: add hath-rust command builder"
```

---

### Task 4: Add Hentai@Home Settings Client

**Files:**
- Create: `internal/ehentai/settings.go`
- Create: `internal/ehentai/settings_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add HTML parser dependency**

Run:

```bash
go get golang.org/x/net/html
```

Expected: `go.mod` and `go.sum` update successfully.

- [ ] **Step 2: Write failing e-hentai tests**

Create `internal/ehentai/settings_test.go`:

```go
package ehentai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseSettingsForm(t *testing.T) {
	html := `<html><body><form>
<input type="text" name="f_port" value="1234">
<input type="text" name="name" value="client">
<input type="checkbox" name="enabled" checked="checked">
<input type="checkbox" name="ignored">
</form></body></html>`
	form, locked, err := ParseSettingsForm(strings.NewReader(html))
	if err != nil {
		t.Fatalf("ParseSettingsForm() error = %v", err)
	}
	if locked {
		t.Fatal("locked = true, want false")
	}
	if got := form.Get("f_port"); got != "1234" {
		t.Fatalf("f_port = %q", got)
	}
	if got := form.Get("enabled"); got != "on" {
		t.Fatalf("enabled = %q", got)
	}
	if form.Has("ignored") {
		t.Fatal("unchecked checkbox should not be included")
	}
}

func TestParseSettingsFormDetectsLockedPort(t *testing.T) {
	html := `<input name="f_port" value="1234" disabled="disabled">`
	_, locked, err := ParseSettingsForm(strings.NewReader(html))
	if err != nil {
		t.Fatalf("ParseSettingsForm() error = %v", err)
	}
	if !locked {
		t.Fatal("locked = false, want true")
	}
}

func TestUpdatePortPostsExistingFieldsWithNewPort(t *testing.T) {
	var postedPort string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<input name="f_port" value="1111"><input name="name" value="client">`)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		postedPort = r.Form.Get("f_port")
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	client := Client{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
		MemberID:   "123456",
		PassHash:   "pass",
		ClientID:   "999",
	}
	if err := client.UpdatePort(context.Background(), 45678); err != nil {
		t.Fatalf("UpdatePort() error = %v", err)
	}
	if postedPort != "45678" {
		t.Fatalf("postedPort = %q, want 45678", postedPort)
	}
}

func TestUpdatePortReturnsLockedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<input name="f_port" value="1111" disabled="disabled">`)
	}))
	defer server.Close()

	client := Client{HTTPClient: server.Client(), BaseURL: server.URL, MemberID: "123", PassHash: "pass", ClientID: "999"}
	err := client.UpdatePort(context.Background(), 45678)
	if err == nil || !IsPortLocked(err) {
		t.Fatalf("UpdatePort() error = %v, want port locked", err)
	}
}
```

- [ ] **Step 3: Run e-hentai tests and verify failure**

Run:

```bash
go test ./internal/ehentai
```

Expected: FAIL because `Client`, `ParseSettingsForm`, and `IsPortLocked` are undefined.

- [ ] **Step 4: Implement e-hentai settings client**

Create `internal/ehentai/settings.go`:

```go
package ehentai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var ErrPortLocked = errors.New("Hentai@Home 设置页暂时锁定端口")

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	HTTPClient HTTPDoer
	BaseURL    string
	MemberID   string
	PassHash   string
	ClientID   string
}

func IsPortLocked(err error) bool {
	return errors.Is(err, ErrPortLocked)
}

func (c Client) UpdatePort(ctx context.Context, port int) error {
	form, err := c.fetchForm(ctx)
	if err != nil {
		return err
	}
	form.Set("f_port", strconv.Itoa(port))
	return c.postForm(ctx, form)
}

func (c Client) fetchForm(ctx context.Context) (url.Values, error) {
	requestURL := c.settingsURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	c.addCookies(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取 Hentai@Home 设置页失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("获取 Hentai@Home 设置页失败: HTTP %d", resp.StatusCode)
	}
	form, locked, err := ParseSettingsForm(resp.Body)
	if err != nil {
		return nil, err
	}
	if locked {
		return nil, ErrPortLocked
	}
	return form, nil
}

func (c Client) postForm(ctx context.Context, form url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.settingsURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	c.addCookies(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("提交 Hentai@Home 端口失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("提交 Hentai@Home 端口失败: HTTP %d", resp.StatusCode)
	}
	return nil
}

func ParseSettingsForm(reader io.Reader) (url.Values, bool, error) {
	root, err := html.Parse(reader)
	if err != nil {
		return nil, false, fmt.Errorf("解析 Hentai@Home 设置页失败: %w", err)
	}
	values := url.Values{}
	locked := false
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			name := attr(n, "name")
			if name != "" {
				inputType := strings.ToLower(attr(n, "type"))
				if name == "f_port" && hasAttr(n, "disabled") {
					locked = true
				}
				if inputType == "checkbox" {
					if hasAttr(n, "checked") {
						values.Set(name, checkboxValue(n))
					}
				} else {
					values.Set(name, attr(n, "value"))
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if !values.Has("f_port") {
		return nil, locked, fmt.Errorf("Hentai@Home 设置页缺少 f_port 字段")
	}
	return values, locked, nil
}

func (c Client) settingsURL() string {
	base := c.BaseURL
	if base == "" {
		base = "https://e-hentai.org"
	}
	return fmt.Sprintf("%s/hentaiathome.php?cid=%s&act=settings", strings.TrimRight(base, "/"), url.QueryEscape(c.ClientID))
}

func (c Client) addCookies(req *http.Request) {
	req.AddCookie(&http.Cookie{Name: "ipb_member_id", Value: c.MemberID})
	req.AddCookie(&http.Cookie{Name: "ipb_pass_hash", Value: c.PassHash})
}

func (c Client) httpClient() HTTPDoer {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, name string) bool {
	for _, a := range n.Attr {
		if a.Key == name {
			return true
		}
	}
	return false
}

func checkboxValue(n *html.Node) string {
	if value := attr(n, "value"); value != "" {
		return value
	}
	return "on"
}
```

- [ ] **Step 5: Run e-hentai tests and verify pass**

Run:

```bash
go test ./internal/ehentai
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/ehentai/settings.go internal/ehentai/settings_test.go
git commit -m "feat: add Hentai@Home settings client"
```

---

### Task 5: Add Process Runner Abstraction

**Files:**
- Create: `internal/process/command.go`
- Create: `internal/process/command_test.go`

- [ ] **Step 1: Write failing process tests**

Create `internal/process/command_test.go`:

```go
package process

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestOSRunnerStartAndStop(t *testing.T) {
	if os.Getenv("HATH_PROCESS_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		return
	}

	runner := OSRunner{}
	proc, err := runner.Start(context.Background(), Spec{
		Name: "helper",
		Path: os.Args[0],
		Args: []string{"-test.run=TestOSRunnerStartAndStop"},
		Env:  []string{"HATH_PROCESS_HELPER=1"},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := proc.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-proc.Done():
	case <-time.After(time.Second):
		t.Fatal("process did not exit")
	}
}
```

- [ ] **Step 2: Run process tests and verify failure**

Run:

```bash
go test ./internal/process
```

Expected: FAIL because `OSRunner`, `Spec`, and `Process` are undefined.

- [ ] **Step 3: Implement process runner**

Create `internal/process/command.go`:

```go
package process

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
)

type Spec struct {
	Name string
	Path string
	Args []string
	Env  []string
	Dir  string
}

type Process interface {
	Done() <-chan error
	Stop(ctx context.Context) error
}

type Runner interface {
	Start(ctx context.Context, spec Spec) (Process, error)
}

type OSRunner struct{}

func (OSRunner) Start(ctx context.Context, spec Spec) (Process, error) {
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动进程 %s 失败: %w", spec.Name, err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	return &osProcess{name: spec.Name, cmd: cmd, done: done}, nil
}

type osProcess struct {
	name string
	cmd  *exec.Cmd
	done chan error
}

func (p *osProcess) Done() <-chan error {
	return p.done
}

func (p *osProcess) Stop(ctx context.Context) error {
	if p.cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(p.cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		log.Printf("进程 %s 未在超时内退出，发送 SIGKILL", p.name)
		if pgid, err := syscall.Getpgid(p.cmd.Process.Pid); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = p.cmd.Process.Kill()
		}
		<-p.done
		return ctx.Err()
	}
}
```

- [ ] **Step 4: Run process tests and verify pass**

Run:

```bash
go test ./internal/process
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/process/command.go internal/process/command_test.go
git commit -m "feat: add process runner"
```

---

### Task 6: Add Bandwidth Limiter

**Files:**
- Create: `internal/bandwidth/limiter.go`
- Create: `internal/bandwidth/limiter_test.go`

- [ ] **Step 1: Write failing bandwidth tests**

Create `internal/bandwidth/limiter_test.go`:

```go
package bandwidth

import (
	"context"
	"reflect"
	"testing"
)

type fakeCommandRunner struct {
	calls [][]string
}

func (f *fakeCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil
}

func TestApplyBuildsTCRules(t *testing.T) {
	fake := &fakeCommandRunner{}
	limiter := Limiter{Runner: fake, Interface: "eth0", UploadLimit: "10mbit"}
	if err := limiter.Apply(context.Background(), 4567); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := [][]string{
		{"tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "htb", "default", "30"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:30", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"tc", "class", "replace", "dev", "eth0", "parent", "1:", "classid", "1:10", "htb", "rate", "10mbit", "ceil", "10mbit"},
		{"tc", "filter", "replace", "dev", "eth0", "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", "4567", "0xffff", "flowid", "1:10"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestClearDeletesRootQdisc(t *testing.T) {
	fake := &fakeCommandRunner{}
	limiter := Limiter{Runner: fake, Interface: "eth0"}
	if err := limiter.Clear(context.Background()); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	want := [][]string{{"tc", "qdisc", "del", "dev", "eth0", "root"}}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}
```

- [ ] **Step 2: Run bandwidth tests and verify failure**

Run:

```bash
go test ./internal/bandwidth
```

Expected: FAIL because `Limiter` is undefined.

- [ ] **Step 3: Implement bandwidth limiter**

Create `internal/bandwidth/limiter.go`:

```go
package bandwidth

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("执行 %s %v 失败: %w: %s", name, args, err, string(out))
	}
	return nil
}

type Limiter struct {
	Runner      CommandRunner
	Interface   string
	UploadLimit string
}

func (l Limiter) Apply(ctx context.Context, port int) error {
	runner := l.runner()
	commands := [][]string{
		{"qdisc", "replace", "dev", l.Interface, "root", "handle", "1:", "htb", "default", "30"},
		{"class", "replace", "dev", l.Interface, "parent", "1:", "classid", "1:30", "htb", "rate", "10000mbit", "ceil", "10000mbit"},
		{"class", "replace", "dev", l.Interface, "parent", "1:", "classid", "1:10", "htb", "rate", l.UploadLimit, "ceil", l.UploadLimit},
		{"filter", "replace", "dev", l.Interface, "protocol", "ip", "parent", "1:0", "prio", "1", "u32", "match", "ip", "sport", strconv.Itoa(port), "0xffff", "flowid", "1:10"},
	}
	for _, args := range commands {
		if err := runner.Run(ctx, "tc", args...); err != nil {
			return fmt.Errorf("配置上传限速失败: %w", err)
		}
	}
	return nil
}

func (l Limiter) Clear(ctx context.Context) error {
	if err := l.runner().Run(ctx, "tc", "qdisc", "del", "dev", l.Interface, "root"); err != nil {
		return fmt.Errorf("清理上传限速规则失败: %w", err)
	}
	return nil
}

func (l Limiter) runner() CommandRunner {
	if l.Runner != nil {
		return l.Runner
	}
	return ExecRunner{}
}
```

- [ ] **Step 4: Run bandwidth tests and verify pass**

Run:

```bash
go test ./internal/bandwidth
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bandwidth/limiter.go internal/bandwidth/limiter_test.go
git commit -m "feat: add tc bandwidth limiter"
```

---

### Task 7: Add Supervisor State Machine

**Files:**
- Create: `internal/supervisor/supervisor.go`
- Create: `internal/supervisor/supervisor_test.go`

- [ ] **Step 1: Write failing supervisor tests**

Create `internal/supervisor/supervisor_test.go`:

```go
package supervisor

import (
	"context"
	"testing"

	"github.com/ngnlAYY/hath-with-natter/internal/natmap"
)

type fakeHath struct {
	running bool
	starts  []int
	stops   int
}

func (f *fakeHath) Start(ctx context.Context, port int) error {
	f.running = true
	f.starts = append(f.starts, port)
	return nil
}

func (f *fakeHath) Stop(ctx context.Context) error {
	if f.running {
		f.stops++
	}
	f.running = false
	return nil
}

func (f *fakeHath) Running() bool { return f.running }

type fakeUpdater struct {
	ports []int
}

func (f *fakeUpdater) UpdatePort(ctx context.Context, port int) error {
	f.ports = append(f.ports, port)
	return nil
}

func TestHandleFirstMappingUpdatesAndStartsHath(t *testing.T) {
	hath := &fakeHath{}
	updater := &fakeUpdater{}
	coord := Coordinator{Hath: hath, Updater: updater}
	mapping := natmap.Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, PrivatePort: 4567, Protocol: "TCP"}

	if err := coord.HandleMapping(context.Background(), mapping); err != nil {
		t.Fatalf("HandleMapping() error = %v", err)
	}
	if len(updater.ports) != 1 || updater.ports[0] != 45678 {
		t.Fatalf("updated ports = %#v", updater.ports)
	}
	if len(hath.starts) != 1 || hath.starts[0] != 4567 {
		t.Fatalf("hath starts = %#v", hath.starts)
	}
}

func TestHandleSameMappingDoesNotRestartRunningHath(t *testing.T) {
	hath := &fakeHath{running: true}
	updater := &fakeUpdater{}
	mapping := natmap.Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, PrivatePort: 4567, Protocol: "TCP"}
	coord := Coordinator{Hath: hath, Updater: updater, CurrentMapping: mapping}

	if err := coord.HandleMapping(context.Background(), mapping); err != nil {
		t.Fatalf("HandleMapping() error = %v", err)
	}
	if len(updater.ports) != 0 || len(hath.starts) != 0 || hath.stops != 0 {
		t.Fatalf("unexpected actions: ports=%#v starts=%#v stops=%d", updater.ports, hath.starts, hath.stops)
	}
}

func TestHandleChangedMappingRestartsHath(t *testing.T) {
	hath := &fakeHath{running: true}
	updater := &fakeUpdater{}
	oldMapping := natmap.Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, PrivatePort: 4567, Protocol: "TCP"}
	newMapping := natmap.Mapping{PublicAddress: "203.0.113.10", PublicPort: 45679, PrivatePort: 4567, Protocol: "TCP"}
	coord := Coordinator{Hath: hath, Updater: updater, CurrentMapping: oldMapping}

	if err := coord.HandleMapping(context.Background(), newMapping); err != nil {
		t.Fatalf("HandleMapping() error = %v", err)
	}
	if hath.stops != 1 {
		t.Fatalf("stops = %d, want 1", hath.stops)
	}
	if len(updater.ports) != 1 || updater.ports[0] != 45679 {
		t.Fatalf("updated ports = %#v", updater.ports)
	}
	if len(hath.starts) != 1 || hath.starts[0] != 4567 {
		t.Fatalf("starts = %#v", hath.starts)
	}
}
```

- [ ] **Step 2: Run supervisor tests and verify failure**

Run:

```bash
go test ./internal/supervisor
```

Expected: FAIL because `Coordinator` is undefined.

- [ ] **Step 3: Implement supervisor coordinator**

Create `internal/supervisor/supervisor.go`:

```go
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
			log.Printf("natmap 映射未变化: %s:%d", mapping.PublicAddress, mapping.PublicPort)
			return nil
		}
		log.Printf("natmap 映射未变化，但 hath-rust 未运行，准备启动")
		if err := c.Hath.Start(ctx, mapping.PrivatePort); err != nil {
			return fmt.Errorf("启动 hath-rust 失败: %w", err)
		}
		return nil
	}

	if c.Hath.Running() {
		if err := c.Hath.Stop(ctx); err != nil {
			return fmt.Errorf("停止 hath-rust 失败: %w", err)
		}
	}
	if err := c.Updater.UpdatePort(ctx, mapping.PublicPort); err != nil {
		return fmt.Errorf("更新 Hentai@Home 端口失败: %w", err)
	}
	if err := c.Hath.Start(ctx, mapping.PrivatePort); err != nil {
		return fmt.Errorf("启动 hath-rust 失败: %w", err)
	}
	c.CurrentMapping = mapping
	return nil
}
```

- [ ] **Step 4: Run supervisor tests and verify pass**

Run:

```bash
go test ./internal/supervisor
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/supervisor/supervisor.go internal/supervisor/supervisor_test.go
git commit -m "feat: add supervisor state machine"
```

---

### Task 8: Add Notify Socket and CLI Entrypoint

**Files:**
- Create: `internal/natmap/socket.go`
- Modify: `internal/natmap/runner_test.go`
- Create: `cmd/hath-natmap/main.go`
- Create: `scripts/natmap-notify.sh`

- [ ] **Step 1: Add socket tests**

Append to `internal/natmap/runner_test.go`:

```go
func TestNotifySocketRoundTrip(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "notify.sock")
	listener, events, err := ListenNotify(socketPath)
	if err != nil {
		t.Fatalf("ListenNotify() error = %v", err)
	}
	defer listener.Close()

	args := []string{"203.0.113.10", "45678", "ip4p", "4567", "TCP", "192.168.1.2"}
	if err := SendNotify(socketPath, args); err != nil {
		t.Fatalf("SendNotify() error = %v", err)
	}

	select {
	case mapping := <-events:
		if mapping.PublicPort != 45678 || mapping.PrivatePort != 4567 {
			t.Fatalf("mapping = %+v", mapping)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive mapping")
	}
}
```

Also add imports to `internal/natmap/runner_test.go`:

```go
import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)
```

- [ ] **Step 2: Run natmap tests and verify failure**

Run:

```bash
go test ./internal/natmap
```

Expected: FAIL because `ListenNotify` and `SendNotify` are undefined.

- [ ] **Step 3: Implement notify socket**

Create `internal/natmap/socket.go`:

```go
package natmap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
)

const DefaultNotifySocket = "/run/hath-natmap/notify.sock"

type Listener struct {
	listener net.Listener
}

func (l *Listener) Close() error {
	return l.listener.Close()
}

func ListenNotify(socketPath string) (*Listener, <-chan Mapping, error) {
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("监听 natmap notify socket 失败: %w", err)
	}
	events := make(chan Mapping, 16)
	go func() {
		defer close(events)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleNotifyConn(conn, events)
		}
	}()
	return &Listener{listener: listener}, events, nil
}

func SendNotify(socketPath string, args []string) error {
	mapping, err := ParseNotifyArgs(args)
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("连接 natmap notify socket 失败: %w", err)
	}
	defer conn.Close()
	return json.NewEncoder(conn).Encode(mapping)
}

func handleNotifyConn(conn net.Conn, events chan<- Mapping) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var mapping Mapping
		if err := json.Unmarshal(scanner.Bytes(), &mapping); err == nil {
			events <- mapping
		}
	}
}
```

- [ ] **Step 4: Run natmap tests and verify pass**

Run:

```bash
go test ./internal/natmap
```

Expected: PASS.

- [ ] **Step 5: Add CLI entrypoint**

Create `cmd/hath-natmap/main.go` with a minimal runnable shell. Full orchestration wiring comes in Task 9.

```go
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
	if value := os.Getenv("HATH_NATMAP_NOTIFY_SOCKET"); value != "" {
		return value
	}
	return natmap.DefaultNotifySocket
}
```

- [ ] **Step 6: Add notify shell bridge**

Create `scripts/natmap-notify.sh`:

```sh
#!/bin/sh
set -eu

exec /usr/local/bin/hath-natmap notify "$@"
```

Then make it executable:

```bash
chmod +x scripts/natmap-notify.sh
```

- [ ] **Step 7: Run all tests and build CLI**

Run:

```bash
go test ./...
go build ./cmd/hath-natmap
```

Expected: both commands PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/hath-natmap/main.go internal/natmap/socket.go internal/natmap/runner_test.go scripts/natmap-notify.sh
git commit -m "feat: add natmap notify socket"
```

---

### Task 9: Wire Runtime Orchestration

**Files:**
- Modify: `internal/natmap/runner.go`
- Modify: `internal/hath/client.go`
- Modify: `internal/supervisor/supervisor.go`
- Modify: `cmd/hath-natmap/main.go`

- [ ] **Step 1: Add process-backed controllers**

Append to `internal/natmap/runner.go`:

```go
import (
	"context"

	"github.com/ngnlAYY/hath-with-natter/internal/process"
)

type ProcessRunner struct {
	Config RunnerConfig
	Runner process.Runner
	proc   process.Process
}

func (r *ProcessRunner) Start(ctx context.Context) error {
	proc, err := r.Runner.Start(ctx, process.Spec{Name: "natmap", Path: r.Config.BinaryPath, Args: r.Config.Args()})
	if err != nil {
		return err
	}
	r.proc = proc
	return nil
}

func (r *ProcessRunner) Stop(ctx context.Context) error {
	if r.proc == nil {
		return nil
	}
	return r.proc.Stop(ctx)
}

func (r *ProcessRunner) Done() <-chan error {
	if r.proc == nil {
		ch := make(chan error)
		close(ch)
		return ch
	}
	return r.proc.Done()
}
```

If Go reports duplicate import blocks, merge all imports in `internal/natmap/runner.go` into a single import block.

- [ ] **Step 2: Add hath process controller**

Append to `internal/hath/client.go`:

```go
import (
	"context"
	"time"

	"github.com/ngnlAYY/hath-with-natter/internal/process"
)

type Controller struct {
	Config Config
	Runner process.Runner
	proc   process.Process
}

func (c *Controller) Start(ctx context.Context, port int) error {
	if err := c.Config.WriteClientLogin(); err != nil {
		return err
	}
	proc, err := c.Runner.Start(ctx, process.Spec{Name: "hath-rust", Path: c.Config.BinaryPath, Args: c.Config.Args(port)})
	if err != nil {
		return err
	}
	c.proc = proc
	return nil
}

func (c *Controller) Stop(ctx context.Context) error {
	if c.proc == nil {
		return nil
	}
	stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := c.proc.Stop(stopCtx)
	c.proc = nil
	return err
}

func (c *Controller) Running() bool {
	if c.proc == nil {
		return false
	}
	select {
	case <-c.proc.Done():
		c.proc = nil
		return false
	default:
		return true
	}
}
```

If Go reports duplicate import blocks, merge all imports in `internal/hath/client.go` into a single import block.

- [ ] **Step 3: Extend supervisor with run loop**

Append to `internal/supervisor/supervisor.go`:

```go
type NatmapProcess interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Done() <-chan error
}

type Runtime struct {
	Natmap      NatmapProcess
	Hath        HathController
	Updater     PortUpdater
	Events      <-chan natmap.Mapping
	RetryDelay  time.Duration
	RestartDelay time.Duration
}

func (r Runtime) Run(ctx context.Context) error {
	coord := Coordinator{Hath: r.Hath, Updater: r.Updater}
	defer func() {
		_ = r.Hath.Stop(context.Background())
		_ = r.Natmap.Stop(context.Background())
	}()
	for ctx.Err() == nil {
		if err := r.Natmap.Start(ctx); err != nil {
			if !sleep(ctx, r.retryDelay()) {
				return nil
			}
			continue
		}
		if err := r.runUntilRestart(ctx, &coord); err != nil {
			log.Printf("运行期错误，准备自动恢复: %v", err)
		}
		_ = r.Hath.Stop(ctx)
		_ = r.Natmap.Stop(ctx)
		if !sleep(ctx, r.retryDelay()) {
			return nil
		}
	}
	return nil
}

func (r Runtime) runUntilRestart(ctx context.Context, coord *Coordinator) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case mapping, ok := <-r.Events:
			if !ok {
				return fmt.Errorf("natmap notify 通道已关闭")
			}
			if err := coord.HandleMapping(ctx, mapping); err != nil {
				return err
			}
		case err := <-r.Natmap.Done():
			return fmt.Errorf("natmap 进程退出: %w", err)
		}
	}
}

func (r Runtime) retryDelay() time.Duration {
	if r.RetryDelay > 0 {
		return r.RetryDelay
	}
	return 5 * time.Second
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
```

- [ ] **Step 4: Wire main runtime**

Replace the non-notify branch in `cmd/hath-natmap/main.go` after config load with:

```go
	if err := os.MkdirAll("/run/hath-natmap", 0o755); err != nil {
		log.Fatalf("创建运行目录失败: %v", err)
	}
	listener, events, err := natmap.ListenNotify(notifySocketPath())
	if err != nil {
		log.Fatalf("启动 natmap notify socket 失败: %v", err)
	}
	defer listener.Close()

	runner := process.OSRunner{}
	natmapRunner := &natmap.ProcessRunner{
		Config: natmap.RunnerConfig{
			BinaryPath:          cfg.Natmap.BinaryPath,
			BindPort:            cfg.Network.BindPort,
			StunServer:          cfg.Natmap.StunServer,
			HTTPKeepaliveServer: cfg.Natmap.HTTPKeepaliveServer,
			KeepaliveInterval:   cfg.Natmap.KeepaliveInterval.Duration,
			NotifyScript:        cfg.Natmap.NotifyScript,
		},
		Runner: runner,
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
	updater := ehentai.Client{HTTPClient: updaterHTTPClient, MemberID: cfg.EHentai.MemberID, PassHash: cfg.EHentai.PassHash, ClientID: cfg.EHentai.ClientID}
	runtime := supervisor.Runtime{
		Natmap:       natmapRunner,
		Hath:         hathController,
		Updater:      updater,
		Events:       events,
		RetryDelay:   cfg.Runtime.Retry.InitialDelay.Duration,
		RestartDelay: cfg.Runtime.RestartDelay.Duration,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runtime.Run(ctx); err != nil {
		log.Fatalf("运行失败: %v", err)
	}
```

Also update imports in `cmd/hath-natmap/main.go` to include:

```go
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
```

- [ ] **Step 5: Run build and fix import errors**

Run:

```bash
gofmt -w cmd internal
go test ./...
go build ./cmd/hath-natmap
```

Expected: PASS. If Go rejects multiple import blocks caused by appending snippets, consolidate imports in the modified file without changing behavior.

- [ ] **Step 6: Commit**

```bash
git add cmd/hath-natmap/main.go internal/natmap/runner.go internal/hath/client.go internal/supervisor/supervisor.go
git commit -m "feat: wire orchestrator runtime"
```

---

### Task 10: Add Bandwidth Limiter to Runtime

**Files:**
- Modify: `cmd/hath-natmap/main.go`
- Modify: `internal/bandwidth/limiter.go`

- [ ] **Step 1: Add limiter config check to main**

In `cmd/hath-natmap/main.go`, after `cfg, err := config.Load(*configPath)` succeeds and before `natmap.ListenNotify`, add:

```go
	if cfg.Bandwidth.Enabled {
		limiter := bandwidth.Limiter{Interface: cfg.Bandwidth.Interface, UploadLimit: cfg.Bandwidth.UploadLimit}
		if err := limiter.Apply(context.Background(), cfg.Network.BindPort); err != nil {
			log.Fatalf("配置上传限速失败: %v", err)
		}
		defer func() {
			if err := limiter.Clear(context.Background()); err != nil {
				log.Printf("清理上传限速规则失败: %v", err)
			}
		}()
	}
```

Add import:

```go
"github.com/ngnlAYY/hath-with-natter/internal/bandwidth"
```

- [ ] **Step 2: Run tests and build**

Run:

```bash
gofmt -w cmd internal
go test ./...
go build ./cmd/hath-natmap
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/hath-natmap/main.go internal/bandwidth/limiter.go
git commit -m "feat: apply tc bandwidth limit at startup"
```

---

### Task 11: Add Docker Packaging and Example Config

**Files:**
- Create: `configs/config.example.yaml`
- Replace: `Dockerfile`
- Modify: `.github/workflows/docker-image.yml`

- [ ] **Step 1: Write new example config**

Create `configs/config.example.yaml`:

```yaml
# e-hentai 账号 Cookie 与 Hentai@Home 客户端信息。
ehentai:
  member_id: "123456"
  pass_hash: "example-pass-hash"
  client_id: "12345"
  client_key: "example-client-key"

# natmap 和 hath-rust 共享这个固定本地端口。
network:
  bind_port: 4567
  external_update_timeout: 60s

natmap:
  binary_path: /usr/local/bin/natmap
  stun_server: stun.nextcloud.com:3478
  http_keepalive_server: www.baidu.com:80
  keepalive_interval: 15s
  notify_script: /usr/local/bin/natmap-notify.sh

hath:
  binary_path: /usr/local/bin/hath-rust
  data_dir: /data/hath
  log_level: info
  force_background_scan: true
  rpc_server_ip: ""

proxy:
  enabled: true
  url: http://127.0.0.1:8080
  use_for_hath_downloads: false

bandwidth:
  enabled: true
  upload_limit: 10mbit
  interface: eth0

runtime:
  shutdown_timeout: 30s
  restart_delay: 5s
  retry:
    initial_delay: 5s
    max_delay: 5m
```

- [ ] **Step 2: Replace Dockerfile**

Replace `Dockerfile` with:

```dockerfile
FROM golang:1.23-alpine AS go-builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hath-natmap ./cmd/hath-natmap

FROM alpine:3.20 AS binary-downloader

ARG TARGETARCH
ARG TARGETVARIANT
ARG NATMAP_VERSION=20260214
ARG HATH_RUST_VERSION=v1.17.0

RUN apk add --no-cache ca-certificates wget
RUN set -eux; \
    case "${TARGETARCH}/${TARGETVARIANT}" in \
      "amd64/") \
        NATMAP_ASSET="natmap-linux-x86_64"; \
        HATH_ASSET="hath-rust-x86_64-unknown-linux-musl"; \
        ;; \
      "arm64/") \
        NATMAP_ASSET="natmap-linux-arm64"; \
        HATH_ASSET="hath-rust-aarch64-unknown-linux-musl"; \
        ;; \
      "arm/v7") \
        NATMAP_ASSET="natmap-linux-arm32v7hf"; \
        HATH_ASSET="hath-rust-armv7-unknown-linux-musleabihf"; \
        ;; \
      *) \
        echo "Unsupported platform: ${TARGETARCH}/${TARGETVARIANT}" >&2; \
        exit 1; \
        ;; \
    esac; \
    wget -O /out-natmap "https://github.com/heiher/natmap/releases/download/${NATMAP_VERSION}/${NATMAP_ASSET}"; \
    wget -O /out-hath-rust "https://github.com/james58899/hath-rust/releases/download/${HATH_RUST_VERSION}/${HATH_ASSET}"; \
    chmod 0755 /out-natmap /out-hath-rust

FROM alpine:3.20

RUN apk add --no-cache ca-certificates iproute2 tzdata
WORKDIR /app

COPY --from=go-builder /out/hath-natmap /usr/local/bin/hath-natmap
COPY --from=binary-downloader /out-natmap /usr/local/bin/natmap
COPY --from=binary-downloader /out-hath-rust /usr/local/bin/hath-rust
COPY scripts/natmap-notify.sh /usr/local/bin/natmap-notify.sh
COPY configs/config.example.yaml /config/config.example.yaml

RUN chmod 0755 /usr/local/bin/hath-natmap /usr/local/bin/natmap /usr/local/bin/hath-rust /usr/local/bin/natmap-notify.sh \
    && mkdir -p /config /data/hath /run/hath-natmap

ENTRYPOINT ["/usr/local/bin/hath-natmap"]
CMD ["--config", "/config/config.yaml"]
```

- [ ] **Step 3: Update Docker workflow platforms**

Modify `.github/workflows/docker-image.yml` line with platforms to:

```yaml
          platforms: linux/amd64,linux/arm64,linux/arm/v7
```

Keep the existing DockerHub tag unless the user asks to rename the image.

- [ ] **Step 4: Build Docker image locally for amd64**

Run:

```bash
docker build --platform linux/amd64 -t hath-with-natmap:local .
```

Expected: image builds successfully. If network rate limits GitHub downloads, retry once after confirming the URL in the Dockerfile matches release asset names.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile .github/workflows/docker-image.yml configs/config.example.yaml
git commit -m "feat: add Docker packaging"
```

---

### Task 12: Replace Documentation

**Files:**
- Rename: `README.md` -> `README.legacy.md`
- Create: `README.md`
- Create: `docs/configuration.md`
- Create: `docs/docker.md`
- Create: `docs/bandwidth-limit.md`
- Create: `docs/troubleshooting.md`

- [ ] **Step 1: Preserve old README for one release cycle**

Run:

```bash
git mv README.md README.legacy.md
```

Expected: old README is preserved as `README.legacy.md`.

- [ ] **Step 2: Write new README**

Create `README.md`:

```markdown
# hath-with-natmap

`hath-with-natmap` 是一个 Docker-only 编排器，用于在全锥型 NAT 网络中运行 `hath-rust`。它使用 `natmap` 获取公网映射端口，自动更新 Hentai@Home 设置页，并在需要时启动或重启 `hath-rust`。

## 特性

- 使用 `natmap` bind 模式获取公网映射。
- `natmap` 与 `hath-rust` 共享固定本地端口。
- 公网映射未变化时不重复重启 `hath-rust`。
- 自动更新 Hentai@Home 端口。
- 可选自动配置 `tc` 上传限速。
- 中文日志、中文错误信息和中文文档。

## 快速开始

复制配置文件：

```bash
cp configs/config.example.yaml config.yaml
```

编辑 `config.yaml`，填入 e-hentai Cookie、Hentai@Home client 信息、端口和代理配置。

构建镜像：

```bash
docker build -t hath-with-natmap:local .
```

运行容器：

```bash
docker run --rm \
  --name hath-with-natmap \
  --net host \
  --cap-add NET_ADMIN \
  -v "$PWD/config.yaml:/config/config.yaml:ro" \
  -v "$PWD/hath:/data/hath" \
  hath-with-natmap:local
```

如果未启用上传限速，可以移除 `--cap-add NET_ADMIN`。

## 文档

- [配置说明](docs/configuration.md)
- [Docker 运行](docs/docker.md)
- [上传限速](docs/bandwidth-limit.md)
- [排障指南](docs/troubleshooting.md)

## 参考项目

- [heiher/natmap](https://github.com/heiher/natmap)
- [james58899/hath-rust](https://github.com/james58899/hath-rust)
```

- [ ] **Step 3: Write configuration docs**

Create `docs/configuration.md`:

```markdown
# 配置说明

配置文件默认路径是 `/config/config.yaml`。容器启动时会先校验配置，配置错误会直接终止启动。

## ehentai

```yaml
ehentai:
  member_id: "123456"
  pass_hash: "example-pass-hash"
  client_id: "12345"
  client_key: "example-client-key"
```

- `member_id`：e-hentai Cookie `ipb_member_id`。
- `pass_hash`：e-hentai Cookie `ipb_pass_hash`。
- `client_id`：Hentai@Home client ID。
- `client_key`：Hentai@Home client key。

## network

```yaml
network:
  bind_port: 4567
  external_update_timeout: 60s
```

`bind_port` 是 `natmap` 和 `hath-rust` 共享的固定本地端口。

## natmap

```yaml
natmap:
  binary_path: /usr/local/bin/natmap
  stun_server: stun.nextcloud.com:3478
  http_keepalive_server: www.baidu.com:80
  keepalive_interval: 15s
  notify_script: /usr/local/bin/natmap-notify.sh
```

默认镜像已经包含 `natmap` 和 notify 脚本，通常不需要修改路径。

## hath

```yaml
hath:
  binary_path: /usr/local/bin/hath-rust
  data_dir: /data/hath
  log_level: info
  force_background_scan: true
  rpc_server_ip: ""
```

`data_dir` 会保存 hath-rust 的 cache、data、download、log 和 tmp 子目录。

## proxy

```yaml
proxy:
  enabled: true
  url: http://127.0.0.1:8080
  use_for_hath_downloads: false
```

`enabled` 控制更新 Hentai@Home 设置页是否走代理。`use_for_hath_downloads` 控制 `hath-rust` 下载缓存是否走代理。

## bandwidth

```yaml
bandwidth:
  enabled: true
  upload_limit: 10mbit
  interface: eth0
```

启用后会通过 `tc` 对 `bind_port` 的出站流量配置上传限速。

## runtime

```yaml
runtime:
  shutdown_timeout: 30s
  restart_delay: 5s
  retry:
    initial_delay: 5s
    max_delay: 5m
```

这些字段控制停止超时、hath-rust 重启延迟和可重试错误的退避区间。
```

- [ ] **Step 4: Write Docker docs**

Create `docs/docker.md`:

```markdown
# Docker 运行

本项目只支持 Docker 运行。推荐使用 host network，因为 `natmap` 和 `hath-rust` 需要共享本地端口并暴露公网映射。

## 构建

```bash
docker build -t hath-with-natmap:local .
```

## 运行

```bash
docker run --rm \
  --name hath-with-natmap \
  --net host \
  --cap-add NET_ADMIN \
  -v "$PWD/config.yaml:/config/config.yaml:ro" \
  -v "$PWD/hath:/data/hath" \
  hath-with-natmap:local
```

## 权限

- `--net host`：让 `natmap` 和 `hath-rust` 使用宿主机网络栈。
- `--cap-add NET_ADMIN`：仅在启用 `bandwidth.enabled` 时需要。
- `/config/config.yaml`：只读挂载配置。
- `/data/hath`：持久化 hath-rust 数据。

## 支持平台

Dockerfile 显式支持：

- `linux/amd64`
- `linux/arm64`
- `linux/arm/v7`
```

- [ ] **Step 5: Write bandwidth docs**

Create `docs/bandwidth-limit.md`:

```markdown
# 上传限速

上传限速通过 Linux `tc` 实现。启用后，容器启动早期会为 `network.bind_port` 对应的出站流量配置 HTB 规则。

## 启用方式

```yaml
bandwidth:
  enabled: true
  upload_limit: 10mbit
  interface: eth0
```

运行容器时必须添加：

```bash
--cap-add NET_ADMIN
```

## 网卡选择

host network 下，`interface` 应配置为宿主机实际出口网卡。可以在宿主机执行：

```bash
ip route get 1.1.1.1
```

输出中的 `dev` 字段通常就是出口网卡。

## 验证规则

```bash
tc qdisc show dev eth0
tc class show dev eth0
tc filter show dev eth0
```

将 `eth0` 替换为配置中的网卡名。

## 注意事项

当前规则会在目标网卡上设置 root qdisc。不要在已经手工维护复杂 `tc` 规则的网卡上直接启用此功能。
```

- [ ] **Step 6: Write troubleshooting docs**

Create `docs/troubleshooting.md`:

```markdown
# 排障指南

## Hentai@Home 端口一直无法更新

如果日志显示端口字段被锁定，通常表示 `hath-rust` 仍在线或 Hentai@Home 设置页暂时不允许修改端口。编排器会保持 `hath-rust` 停止并重试更新。

## natmap 没有公网映射

检查：

- 网络是否为全锥型 NAT。
- `natmap.stun_server` 是否可访问。
- `natmap.http_keepalive_server` 是否可访问。
- 容器是否使用 `--net host`。

## hath-rust 启动失败

检查：

- `/data/hath` 是否可写。
- `ehentai.client_id` 和 `ehentai.client_key` 是否正确。
- `network.bind_port` 是否被其他进程占用。

## 上传限速未生效

检查：

- 容器是否添加 `--cap-add NET_ADMIN`。
- 镜像是否包含 `tc`。
- `bandwidth.interface` 是否为实际出口网卡。
- `tc filter show dev <interface>` 是否存在匹配 `bind_port` 的规则。

## 代理无法连接

检查：

- `proxy.enabled` 是否符合预期。
- `proxy.url` 是否从容器网络视角可访问。
- host network 下 `127.0.0.1` 指向宿主机网络命名空间。
```

- [ ] **Step 7: Run markdown-free build checks**

Run:

```bash
go test ./...
go build ./cmd/hath-natmap
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add README.md README.legacy.md docs/configuration.md docs/docker.md docs/bandwidth-limit.md docs/troubleshooting.md
git commit -m "docs: replace project documentation"
```

---

### Task 13: Remove Legacy Python/Natter Files

**Files:**
- Delete: `main.py`
- Delete: `natter.py`
- Delete: `requirements.txt`
- Delete: `config.yaml.example`

- [ ] **Step 1: Remove legacy files**

Run:

```bash
git rm main.py natter.py requirements.txt config.yaml.example
```

Expected: files are staged for deletion.

- [ ] **Step 2: Run full Go verification**

Run:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
go build ./cmd/hath-natmap
```

Expected: all commands PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd internal go.mod go.sum
git commit -m "refactor: remove legacy Python implementation"
```

---

### Task 14: Final Docker and Repository Verification

**Files:**
- Modify only if verification reveals a concrete issue in files already touched by previous tasks.

- [ ] **Step 1: Check git status**

Run:

```bash
git status --short
```

Expected: clean working tree before final verification. If files are modified by formatting, inspect and commit them with a focused message.

- [ ] **Step 2: Run full local verification**

Run:

```bash
go test ./...
go vet ./...
go build ./cmd/hath-natmap
docker build --platform linux/amd64 -t hath-with-natmap:local .
```

Expected: all commands PASS.

- [ ] **Step 3: Run image smoke check**

Run:

```bash
docker run --rm hath-with-natmap:local --config /config/config.example.yaml
```

Expected: without `--cap-add NET_ADMIN`, container exits with a clear Chinese `tc`/permission error because the example config enables bandwidth limiting. The process must not panic.

- [ ] **Step 4: Inspect final diff against original branch point**

Run:

```bash
git diff --stat HEAD~10..HEAD
```

Expected: diff shows Go source, Docker, docs, config, and removal of Python files.

- [ ] **Step 5: Final commit if needed**

If Step 2 or Step 3 required fixes, inspect the exact changed files first:

```bash
git status --short
git diff
```

Then stage only the files shown by `git status --short` that belong to the verification fix and commit:

```bash
git commit -m "fix: complete natmap refactor verification"
```

If no files changed, do not create an empty commit.

---

## Self-Review

- Spec coverage: config, natmap bind mode, notify parsing, Hentai@Home update, hath-rust lifecycle, tc upload limiting, Docker packaging, Chinese docs/logs/errors, Google style, and tests all map to tasks above.
- Placeholder scan: this plan avoids unresolved markers and includes concrete paths, commands, snippets, and expected outputs.
- Type consistency: `natmap.Mapping`, `hath.Config`, `ehentai.Client`, `bandwidth.Limiter`, `process.Runner`, and `supervisor.Coordinator` names are used consistently across tasks.
