package hath

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestArgsOmitsOptionalFlagsWhenDisabled(t *testing.T) {
	cfg := Config{
		DataDir:  "/data/hath",
		LogLevel: "unknown",
		ProxyURL: "http://127.0.0.1:8080",
	}
	got := cfg.Args(8080)
	want := []string{
		"--cache-dir", "/data/hath/cache",
		"--data-dir", "/data/hath/data",
		"--download-dir", "/data/hath/download",
		"--log-dir", "/data/hath/log",
		"--temp-dir", "/data/hath/tmp",
		"--port", "8080",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

func TestArgsOmitsProxyWhenProxyURLEmpty(t *testing.T) {
	cfg := Config{
		DataDir:  "/data/hath",
		UseProxy: true,
		ProxyURL: "",
		LogLevel: "debug",
	}
	got := cfg.Args(8080)
	want := []string{
		"--cache-dir", "/data/hath/cache",
		"--data-dir", "/data/hath/data",
		"--download-dir", "/data/hath/download",
		"--log-dir", "/data/hath/log",
		"--temp-dir", "/data/hath/tmp",
		"--port", "8080",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

func TestArgsQuietFlagMapping(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		want     []string
	}{
		{name: "debug", logLevel: "debug"},
		{name: "info", logLevel: "info", want: []string{"-q"}},
		{name: "warn", logLevel: "warn", want: []string{"-qq"}},
		{name: "error", logLevel: "error", want: []string{"-qqq"}},
		{name: "off", logLevel: "off", want: []string{"-qqqq"}},
		{name: "unknown", logLevel: "trace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{DataDir: "/data/hath", LogLevel: tt.logLevel}
			got := cfg.Args(1)
			quietFlags := quietFlagsFromArgs(got)
			if !reflect.DeepEqual(quietFlags, tt.want) {
				t.Fatalf("quiet flags = %#v, want %#v in args %#v", quietFlags, tt.want, got)
			}
		})
	}
}

func TestWriteClientLogin(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, ClientID: "12345", ClientKey: "secret"}
	if err := cfg.WriteClientLogin(); err != nil {
		t.Fatalf("WriteClientLogin() error = %v", err)
	}
	path := filepath.Join(dir, "data", "client_login")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 client_login 失败: %v", err)
	}
	if string(content) != "12345-secret" {
		t.Fatalf("client_login = %q", string(content))
	}
	dataInfo, err := os.Stat(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("读取 data 目录权限失败: %v", err)
	}
	if got := dataInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("data directory permissions = %v, want %v", got, os.FileMode(0o700))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取 client_login 权限失败: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("client_login permissions = %v, want %v", got, os.FileMode(0o600))
	}
}

func TestWriteClientLoginRejectsEmptyClientID(t *testing.T) {
	cfg := Config{DataDir: t.TempDir(), ClientKey: "secret"}
	if err := cfg.WriteClientLogin(); err == nil || !strings.Contains(err.Error(), "ClientID 不能为空") {
		t.Fatalf("WriteClientLogin() error = %v, want ClientID 不能为空", err)
	}
}

func TestWriteClientLoginRejectsEmptyClientKey(t *testing.T) {
	cfg := Config{DataDir: t.TempDir(), ClientID: "12345"}
	if err := cfg.WriteClientLogin(); err == nil || !strings.Contains(err.Error(), "ClientKey 不能为空") {
		t.Fatalf("WriteClientLogin() error = %v, want ClientKey 不能为空", err)
	}
}

func quietFlagsFromArgs(args []string) []string {
	var flags []string
	for _, arg := range args {
		switch arg {
		case "-q", "-qq", "-qqq", "-qqqq":
			flags = append(flags, arg)
		}
	}
	return flags
}
