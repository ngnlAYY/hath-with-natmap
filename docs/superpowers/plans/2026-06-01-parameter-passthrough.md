# Parameter Whitelist Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add YAML-driven whitelist configuration for safe `natmap` and `hath-rust` CLI parameters without allowing arbitrary argument passthrough.

**Architecture:** Extend the existing `config.Config` schema with structured whitelist fields under `natmap` and `hath`, validate them at config load time, and copy them into `natmap.RunnerConfig` / `hath.Config` in `cmd/hath-natmap/main.go`. Argument builders remain the only place that converts config into argv, preserving existing supervisor ownership of fixed bind port, notify, directories, proxy source, and process lifecycle.

**Tech Stack:** Go 1.23, standard `testing`, `gopkg.in/yaml.v3`, existing process runner abstractions, Chinese docs/logging conventions.

---

## File Map

- Modify `internal/config/config.go`: add whitelist config fields and validation helpers.
- Modify `internal/config/config_test.go`: test parsing and validation for the new YAML fields.
- Modify `internal/natmap/runner.go`: map safe natmap whitelist fields to argv.
- Modify `internal/natmap/runner_test.go`: test default args stay unchanged and optional whitelist args are appended.
- Modify `internal/hath/client.go`: map safe hath-rust whitelist fields to argv.
- Modify `internal/hath/client_test.go`: test default args stay unchanged and optional whitelist args are appended.
- Modify `cmd/hath-natmap/main.go`: wire config fields into the two runtime configs.
- Modify `configs/config.example.yaml`: document safe defaults in example config.
- Modify `docs/configuration.md`: document new fields, upstream flag mapping, and non-configurable parameters.
- Modify `docs/troubleshooting.md`: add troubleshooting guidance for unsupported or invalid upstream parameters.

## Implementation Notes

- Do not add `extra_args` or any raw argv passthrough.
- Do not expose `natmap -d`, `-t`, `-p`, `-C`, or `-T`.
- Do not allow overriding `natmap -b`, `-e`, `hath-rust --port`, hath directory args, or proxy source.
- Keep all command execution through argv slices; never build shell strings.
- Avoid logging full argv because proxy URLs can contain credentials.

---

### Task 1: Extend Config Schema and Validation

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write failing config load test for whitelist fields**

Add this test after `TestLoadValidConfig` in `internal/config/config_test.go`:

```go
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

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
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
```

- [ ] **Step 2: Write failing validation tests**

Add these tests after `TestValidateRejectsInvalidBandwidthInterface` in `internal/config/config_test.go`:

```go
func TestValidateRejectsInvalidNatmapAddressFamily(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "notify_script: /usr/local/bin/natmap-notify.sh", "notify_script: /usr/local/bin/natmap-notify.sh\n  address_family: ipx", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "natmap.address_family") {
		t.Fatalf("Load() error = %v, want natmap.address_family validation error", err)
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

			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "natmap.interface") {
				t.Fatalf("Load() error = %v, want natmap.interface validation error", err)
			}
		})
	}
}

func TestValidateRejectsInvalidNatmapFWMark(t *testing.T) {
	tests := []string{"0x", "0xzz", "1;rm", "mark"}
	for _, fwmark := range tests {
		t.Run(fwmark, func(t *testing.T) {
			dir := t.TempDir()
			cfgText := strings.Replace(validConfigYAML(t, dir), "notify_script: /usr/local/bin/natmap-notify.sh", "notify_script: /usr/local/bin/natmap-notify.sh\n  fwmark: '"+fwmark+"'", 1)
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
				t.Fatalf("写入配置失败: %v", err)
			}

			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "natmap.fwmark") {
				t.Fatalf("Load() error = %v, want natmap.fwmark validation error", err)
			}
		})
	}
}

func TestValidateRejectsInvalidNatmapUDPCheckCycle(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), "notify_script: /usr/local/bin/natmap-notify.sh", "notify_script: /usr/local/bin/natmap-notify.sh\n  udp_check_cycle: 0", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "natmap.udp_check_cycle") {
		t.Fatalf("Load() error = %v, want natmap.udp_check_cycle validation error", err)
	}
}

func TestValidateRejectsNegativeHathMaxConnection(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(validConfigYAML(t, dir), `rpc_server_ip: ""`, "rpc_server_ip: \"\"\n  max_connection: -1", 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "hath.max_connection") {
		t.Fatalf("Load() error = %v, want hath.max_connection validation error", err)
	}
}
```

- [ ] **Step 3: Run config tests and verify RED**

Run:

```bash
go test ./internal/config
```

Expected: FAIL with compile errors like `cfg.Natmap.AddressFamily undefined` and `cfg.Hath.DisableLogging undefined`.

- [ ] **Step 4: Add config fields and validators**

Modify `internal/config/config.go` imports to include `strconv`:

```go
import (
	"fmt"
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
```

Replace the `var` block with:

```go
var (
	bandwidthInterfacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,14}$`)
	bandwidthLimitPattern     = regexp.MustCompile(`^[1-9][0-9]*(bit|kbit|mbit|gbit)$`)
	natmapInterfacePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)
)
```

Replace `NatmapConfig` with:

```go
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
```

Replace `HathConfig` with:

```go
type HathConfig struct {
	BinaryPath             string `yaml:"binary_path"`
	DataDir                string `yaml:"data_dir"`
	LogLevel               string `yaml:"log_level"`
	ForceBackgroundScan    bool   `yaml:"force_background_scan"`
	RPCServerIP            string `yaml:"rpc_server_ip"`
	DisableLogging         bool   `yaml:"disable_logging"`
	FlushLog               bool   `yaml:"flush_log"`
	MaxConnection          int    `yaml:"max_connection"`
	DisableIPOriginCheck   bool   `yaml:"disable_ip_origin_check"`
	DisableFloodControl    bool   `yaml:"disable_flood_control"`
	EnableMetrics          bool   `yaml:"enable_metrics"`
	DisableServerHeader    bool   `yaml:"disable_server_header"`
	EnableH3               bool   `yaml:"enable_h3"`
}
```

In `Validate()`, after `natmap.notify_script` validation, add:

```go
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
		return fmt.Errorf("natmap.udp_check_cycle 必须大于 0")
	}
```

In `Validate()`, after `validateLogLevel`, add:

```go
	if c.Hath.MaxConnection < 0 {
		return fmt.Errorf("hath.max_connection 必须大于等于 0")
	}
```

Add these helper functions near the existing validation helpers:

```go
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
		return fmt.Errorf("natmap.fwmark 必须是十进制、八进制或 0x 十六进制无符号整数: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run config tests and verify GREEN**

Run:

```bash
go test ./internal/config
```

Expected: PASS.

- [ ] **Step 6: Commit Task 1**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add parameter whitelist config"
```

---

### Task 2: Add natmap Whitelist Args

**Files:**
- Modify: `internal/natmap/runner.go`
- Test: `internal/natmap/runner_test.go`

- [ ] **Step 1: Write failing natmap argument tests**

Add this test after `TestRunnerConfigArgsBuildsBindModeArguments` in `internal/natmap/runner_test.go`:

```go
func TestRunnerConfigArgsBuildsWhitelistArguments(t *testing.T) {
	cfg := RunnerConfig{
		BindPort:            16000,
		StunServer:          "stun.example.com:3478",
		HTTPKeepaliveServer: "keepalive.example.com:80",
		KeepaliveInterval:   30 * time.Second,
		NotifyScript:        "/usr/local/bin/natmap-notify",
		AddressFamily:       "ipv6",
		UDPMode:             true,
		Interface:           "eth0",
		FWMark:              "0x1",
		UDPCheckCycle:       12,
	}

	got := cfg.Args()
	want := []string{
		"-6",
		"-u",
		"-b", "16000",
		"-s", "stun.example.com:3478",
		"-h", "keepalive.example.com:80",
		"-k", "30",
		"-e", "/usr/local/bin/natmap-notify",
		"-i", "eth0",
		"-f", "0x1",
		"-c", "12",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}
```

- [ ] **Step 2: Run natmap tests and verify RED**

Run:

```bash
go test ./internal/natmap -run 'TestRunnerConfigArgs'
```

Expected: FAIL with compile errors for missing `RunnerConfig` fields.

- [ ] **Step 3: Add natmap fields and argv mapping**

Modify `RunnerConfig` in `internal/natmap/runner.go`:

```go
type RunnerConfig struct {
	BinaryPath          string
	BindPort            int
	StunServer          string
	HTTPKeepaliveServer string
	KeepaliveInterval   time.Duration
	NotifyScript        string
	NotifyToken         string
	AddressFamily       string
	UDPMode             bool
	Interface           string
	FWMark              string
	UDPCheckCycle       int
}
```

Replace `Args()` with:

```go
func (c RunnerConfig) Args() []string {
	addressFlag := "-4"
	if c.AddressFamily == "ipv6" {
		addressFlag = "-6"
	}

	args := []string{addressFlag}
	if c.UDPMode {
		args = append(args, "-u")
	}
	args = append(args,
		"-b", strconv.Itoa(c.BindPort),
		"-s", c.StunServer,
		"-h", c.HTTPKeepaliveServer,
		"-k", strconv.FormatInt(int64(c.KeepaliveInterval/time.Second), 10),
		"-e", c.NotifyScript,
	)
	if c.Interface != "" {
		args = append(args, "-i", c.Interface)
	}
	if c.FWMark != "" {
		args = append(args, "-f", c.FWMark)
	}
	if c.UDPCheckCycle > 0 {
		args = append(args, "-c", strconv.Itoa(c.UDPCheckCycle))
	}
	return args
}
```

- [ ] **Step 4: Run natmap tests and verify GREEN**

Run:

```bash
go test ./internal/natmap
```

Expected: PASS.

- [ ] **Step 5: Commit Task 2**

```bash
git add internal/natmap/runner.go internal/natmap/runner_test.go
git commit -m "feat: configure natmap whitelist args"
```

---

### Task 3: Add hath-rust Whitelist Args

**Files:**
- Modify: `internal/hath/client.go`
- Test: `internal/hath/client_test.go`

- [ ] **Step 1: Write failing hath argument test**

Add this test after `TestArgsOmitsProxyWhenProxyURLEmpty` in `internal/hath/client_test.go`:

```go
func TestArgsIncludesWhitelistedHathFlags(t *testing.T) {
	cfg := Config{
		DataDir:              "/data/hath",
		DisableLogging:       true,
		FlushLog:             true,
		MaxConnection:        128,
		DisableIPOriginCheck: true,
		DisableFloodControl:  true,
		EnableMetrics:        true,
		DisableServerHeader:  true,
		EnableH3:             true,
	}

	got := cfg.Args(4567)
	want := []string{
		"--cache-dir", "/data/hath/cache",
		"--data-dir", "/data/hath/data",
		"--download-dir", "/data/hath/download",
		"--log-dir", "/data/hath/log",
		"--temp-dir", "/data/hath/tmp",
		"--port", "4567",
		"--disable-logging",
		"--flush-log",
		"--max-connection", "128",
		"--disable-ip-origin-check",
		"--disable-flood-control",
		"--enable-metrics",
		"--disable-server-header",
		"--enable-h3",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}
```

- [ ] **Step 2: Run hath tests and verify RED**

Run:

```bash
go test ./internal/hath -run 'TestArgs'
```

Expected: FAIL with compile errors for missing `Config` fields.

- [ ] **Step 3: Add hath fields and argv mapping**

Modify `Config` in `internal/hath/client.go`:

```go
type Config struct {
	BinaryPath             string
	DataDir                string
	LogLevel               string
	ForceBackgroundScan    bool
	RPCServerIP            string
	ProxyURL               string
	UseProxy               bool
	ClientID               string
	ClientKey              string
	DisableLogging         bool
	FlushLog               bool
	MaxConnection          int
	DisableIPOriginCheck   bool
	DisableFloodControl    bool
	EnableMetrics          bool
	DisableServerHeader    bool
	EnableH3               bool
}
```

In `Args(port int)`, after the existing `RPCServerIP` block and before `return args`, add:

```go
	if c.DisableLogging {
		args = append(args, "--disable-logging")
	}
	if c.FlushLog {
		args = append(args, "--flush-log")
	}
	if c.MaxConnection > 0 {
		args = append(args, "--max-connection", strconv.Itoa(c.MaxConnection))
	}
	if c.DisableIPOriginCheck {
		args = append(args, "--disable-ip-origin-check")
	}
	if c.DisableFloodControl {
		args = append(args, "--disable-flood-control")
	}
	if c.EnableMetrics {
		args = append(args, "--enable-metrics")
	}
	if c.DisableServerHeader {
		args = append(args, "--disable-server-header")
	}
	if c.EnableH3 {
		args = append(args, "--enable-h3")
	}
```

- [ ] **Step 4: Run hath tests and verify GREEN**

Run:

```bash
go test ./internal/hath
```

Expected: PASS.

- [ ] **Step 5: Commit Task 3**

```bash
git add internal/hath/client.go internal/hath/client_test.go
git commit -m "feat: configure hath-rust whitelist args"
```

---

### Task 4: Wire Config Into Runtime Builders

**Files:**
- Modify: `cmd/hath-natmap/main.go`
- Test: `cmd/hath-natmap/main_test.go` if practical; otherwise rely on package-level config/args tests and final full test suite.

- [ ] **Step 1: Wire natmap fields**

In `cmd/hath-natmap/main.go`, extend the `natmap.RunnerConfig` literal inside `run()`:

```go
				AddressFamily:       cfg.Natmap.AddressFamily,
				UDPMode:             cfg.Natmap.UDPMode,
				Interface:           cfg.Natmap.Interface,
				FWMark:              cfg.Natmap.FWMark,
				UDPCheckCycle:       cfg.Natmap.UDPCheckCycle,
```

The full literal should look like:

```go
			Config: natmap.RunnerConfig{
				BinaryPath:          cfg.Natmap.BinaryPath,
				BindPort:            cfg.Network.BindPort,
				StunServer:          cfg.Natmap.StunServer,
				HTTPKeepaliveServer: cfg.Natmap.HTTPKeepaliveServer,
				KeepaliveInterval:   cfg.Natmap.KeepaliveInterval.Duration,
				NotifyScript:        cfg.Natmap.NotifyScript,
				NotifyToken:         notifyToken,
				AddressFamily:       cfg.Natmap.AddressFamily,
				UDPMode:             cfg.Natmap.UDPMode,
				Interface:           cfg.Natmap.Interface,
				FWMark:              cfg.Natmap.FWMark,
				UDPCheckCycle:       cfg.Natmap.UDPCheckCycle,
			},
```

- [ ] **Step 2: Wire hath fields**

In the `hath.Config` literal inside `run()`, add:

```go
				DisableLogging:       cfg.Hath.DisableLogging,
				FlushLog:             cfg.Hath.FlushLog,
				MaxConnection:        cfg.Hath.MaxConnection,
				DisableIPOriginCheck: cfg.Hath.DisableIPOriginCheck,
				DisableFloodControl:  cfg.Hath.DisableFloodControl,
				EnableMetrics:        cfg.Hath.EnableMetrics,
				DisableServerHeader:  cfg.Hath.DisableServerHeader,
				EnableH3:             cfg.Hath.EnableH3,
```

The full literal should look like:

```go
			Config: hath.Config{
				BinaryPath:             cfg.Hath.BinaryPath,
				DataDir:                cfg.Hath.DataDir,
				LogLevel:               cfg.Hath.LogLevel,
				ForceBackgroundScan:    cfg.Hath.ForceBackgroundScan,
				RPCServerIP:            cfg.Hath.RPCServerIP,
				ProxyURL:               cfg.Proxy.URL,
				UseProxy:               cfg.Proxy.UseForHathDownloads,
				ClientID:               cfg.EHentai.ClientID,
				ClientKey:              cfg.EHentai.ClientKey,
				DisableLogging:         cfg.Hath.DisableLogging,
				FlushLog:               cfg.Hath.FlushLog,
				MaxConnection:          cfg.Hath.MaxConnection,
				DisableIPOriginCheck:   cfg.Hath.DisableIPOriginCheck,
				DisableFloodControl:    cfg.Hath.DisableFloodControl,
				EnableMetrics:          cfg.Hath.EnableMetrics,
				DisableServerHeader:    cfg.Hath.DisableServerHeader,
				EnableH3:               cfg.Hath.EnableH3,
			},
```

- [ ] **Step 3: Format and run main package tests**

Run:

```bash
gofmt -w cmd/hath-natmap/main.go internal/config/config.go internal/config/config_test.go internal/natmap/runner.go internal/natmap/runner_test.go internal/hath/client.go internal/hath/client_test.go
go test ./cmd/hath-natmap
```

Expected: PASS.

- [ ] **Step 4: Run all tests to catch wiring compile errors**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit Task 4**

```bash
git add cmd/hath-natmap/main.go
git commit -m "feat: wire whitelist parameter config"
```

---

### Task 5: Update Example Config and Documentation

**Files:**
- Modify: `configs/config.example.yaml`
- Modify: `docs/configuration.md`
- Modify: `docs/troubleshooting.md`

- [ ] **Step 1: Update example config**

In `configs/config.example.yaml`, change the `natmap` section to:

```yaml
natmap:
  binary_path: /usr/local/bin/natmap
  stun_server: stun.nextcloud.com:3478
  http_keepalive_server: www.baidu.com:80
  keepalive_interval: 15s
  notify_script: /usr/local/bin/natmap-notify.sh
  address_family: ipv4
  udp_mode: false
  interface: ""
  fwmark: ""
  udp_check_cycle: 10
```

Change the `hath` section to:

```yaml
hath:
  binary_path: /usr/local/bin/hath-rust
  data_dir: /data/hath
  log_level: info
  force_background_scan: true
  rpc_server_ip: ""
  disable_logging: false
  flush_log: false
  max_connection: 0
  disable_ip_origin_check: false
  disable_flood_control: false
  enable_metrics: false
  disable_server_header: false
  enable_h3: false
```

- [ ] **Step 2: Update natmap docs**

In `docs/configuration.md`, replace the `natmap` YAML block with:

```yaml
natmap:
  binary_path: /usr/local/bin/natmap
  stun_server: stun.nextcloud.com:3478
  http_keepalive_server: www.baidu.com:80
  keepalive_interval: 15s
  notify_script: /usr/local/bin/natmap-notify.sh
  address_family: ipv4
  udp_mode: false
  interface: ""
  fwmark: ""
  udp_check_cycle: 10
```

After the existing three natmap bullet points, add:

```markdown
- `address_family`：传给 `natmap` 的地址族，支持 `ipv4` 和 `ipv6`，默认使用 `ipv4`。
- `udp_mode`：是否传递 `natmap -u`。Hentai@Home 使用 TCP 服务，通常保持 `false`。
- `interface`：可选，传给 `natmap -i` 的网卡名或源 IP。
- `fwmark`：可选，传给 `natmap -f` 的 fwmark，支持十进制、八进制或 `0x` 十六进制。
- `udp_check_cycle`：传给 `natmap -c` 的 UDP STUN 检查周期，必须大于 0。

以下 `natmap` 参数由编排器固定管理，不能通过配置覆盖：`-b`、`-s`、`-h`、`-k`、`-e`。不开放 `-d` 和 forward mode 参数 `-t`、`-p`、`-C`、`-T`。
```

- [ ] **Step 3: Update hath docs**

In `docs/configuration.md`, replace the `hath` YAML block with:

```yaml
hath:
  binary_path: /usr/local/bin/hath-rust
  data_dir: /data/hath
  log_level: info
  force_background_scan: true
  rpc_server_ip: ""
  disable_logging: false
  flush_log: false
  max_connection: 0
  disable_ip_origin_check: false
  disable_flood_control: false
  enable_metrics: false
  disable_server_header: false
  enable_h3: false
```

After the existing hath bullet points, add:

```markdown
- `disable_logging`：传递 `hath-rust --disable-logging`，禁用非错误日志写入文件。
- `flush_log`：传递 `hath-rust --flush-log`，每行日志都刷盘。
- `max_connection`：大于 0 时传递 `hath-rust --max-connection <value>`。
- `disable_ip_origin_check`：传递 `hath-rust --disable-ip-origin-check`，同时会影响 server command IP 检查和 flood control。
- `disable_flood_control`：传递 `hath-rust --disable-flood-control`。
- `enable_metrics`：传递 `hath-rust --enable-metrics`。
- `disable_server_header`：传递 `hath-rust --disable-server-header`。
- `enable_h3`：传递 `hath-rust --enable-h3`，启用实验性 HTTP/3。

`--port`、各数据目录参数、`--proxy`、`--force-background-scan`、`--rpc-server-ip` 和 `-q` 仍由现有结构化配置生成；不支持任意额外参数透传。
```

- [ ] **Step 4: Update troubleshooting docs**

In `docs/troubleshooting.md`, under `## 配置校验失败`, add these bullets after the existing proxy bullet:

```markdown
- `natmap.address_family` 只能是 `ipv4` 或 `ipv6`。
- `natmap.interface` 必须是合法网卡名或 IP，不能包含空格、分号或以 `-` 开头。
- `natmap.fwmark` 必须是十进制、八进制或 `0x` 十六进制无符号整数。
- `natmap.udp_check_cycle` 必须大于 0。
- `hath.max_connection` 不能小于 0。
```

Add a new section before `## Docker build 无法连接 Docker socket`:

```markdown
## 上游参数没有生效

检查：

- 当前镜像固定的 `natmap` 版本是否支持该参数。
- 当前镜像固定的 `hath-rust` 版本是否支持该参数。
- 本项目只开放白名单参数，不支持任意 `extra_args`。
- `natmap` 的固定端口、notify 脚本和 forward mode 参数由编排器控制，不能通过配置覆盖。
- `hath-rust` 的端口、目录和代理参数由编排器控制，不能通过配置覆盖。
```

- [ ] **Step 5: Run docs-adjacent tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit Task 5**

```bash
git add configs/config.example.yaml docs/configuration.md docs/troubleshooting.md
git commit -m "docs: document parameter whitelist config"
```

---

### Task 6: Final Verification and Review

**Files:**
- Review all files changed by Tasks 1-5.

- [ ] **Step 1: Run full Go tests**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run race tests**

```bash
go test -race ./...
```

Expected: PASS.

- [ ] **Step 3: Run vet**

```bash
go vet ./...
```

Expected: PASS.

- [ ] **Step 4: Run build**

```bash
go build ./cmd/hath-natmap
rm -f hath-natmap
```

Expected: build succeeds and local binary is removed.

- [ ] **Step 5: Run Docker build if local Docker daemon is available**

```bash
docker build -f docker/Dockerfile --platform linux/amd64 -t hath-with-natmap:local .
```

Expected: PASS when the current user can access `/var/run/docker.sock`. If it fails with `permission denied while trying to connect to the docker API at unix:///var/run/docker.sock`, report it as an environment permission blocker, not a code failure.

- [ ] **Step 6: Request code review**

Use the Go reviewer agent to review the final implementation. The review must check:

- no raw argv passthrough exists;
- fixed bind port, notify script, hath directories, proxy source, and lifecycle remain controlled by the orchestrator;
- validators reject shell-like unsafe values;
- tests cover config, natmap args, hath args, and docs compile-adjacent behavior.

- [ ] **Step 7: Fix reviewer findings or record PASS**

If reviewer finds issues, fix them with tests and rerun Steps 1-4. If reviewer returns PASS, continue.

- [ ] **Step 8: Commit final fixes if any**

If Step 7 required changes:

```bash
git add <changed-files>
git commit -m "fix: finalize parameter whitelist config"
```

If Step 7 required no changes, do not create an empty commit.

---

## Self-Review Checklist

- Spec coverage: Covered config schema, validation, natmap args, hath args, main wiring, docs, security constraints, and final verification.
- Placeholder scan: No `TBD`, `TODO`, or unspecified implementation steps remain.
- Type consistency: Uses `NatmapConfig.AddressFamily`, `UDPMode`, `Interface`, `FWMark`, `UDPCheckCycle`; `HathConfig.DisableLogging`, `FlushLog`, `MaxConnection`, `DisableIPOriginCheck`, `DisableFloodControl`, `EnableMetrics`, `DisableServerHeader`, `EnableH3`; matching runner/controller fields use the same exported names.
