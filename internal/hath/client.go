package hath

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config describes hath-rust process settings and client credentials.
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

// Args builds the hath-rust command arguments for the configured data paths and port.
func (c Config) Args(port int) []string {
	args := []string{
		"--cache-dir", filepath.Join(c.DataDir, "cache"),
		"--data-dir", filepath.Join(c.DataDir, "data"),
		"--download-dir", filepath.Join(c.DataDir, "download"),
		"--log-dir", filepath.Join(c.DataDir, "log"),
		"--temp-dir", filepath.Join(c.DataDir, "tmp"),
		"--port", strconv.Itoa(port),
	}

	if c.UseProxy && c.ProxyURL != "" {
		args = append(args, "--proxy", c.ProxyURL)
	}
	if c.ForceBackgroundScan {
		args = append(args, "--force-background-scan")
	}
	if quietFlag := quietFlagForLogLevel(c.LogLevel); quietFlag != "" {
		args = append(args, quietFlag)
	}
	if c.RPCServerIP != "" {
		args = append(args, "--rpc-server-ip", c.RPCServerIP)
	}

	return args
}

// WriteClientLogin writes hath-rust's client_login credential file.
func (c Config) WriteClientLogin() error {
	if c.ClientID == "" {
		return fmt.Errorf("ClientID 不能为空")
	}
	if c.ClientKey == "" {
		return fmt.Errorf("ClientKey 不能为空")
	}

	dataDir := filepath.Join(c.DataDir, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("创建 hath 数据目录失败: %w", err)
	}

	loginPath := filepath.Join(dataDir, "client_login")
	content := []byte(c.ClientID + "-" + c.ClientKey)
	if err := os.WriteFile(loginPath, content, 0o600); err != nil {
		return fmt.Errorf("写入 client_login 失败: %w", err)
	}

	return nil
}

func quietFlagForLogLevel(logLevel string) string {
	switch logLevel {
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
