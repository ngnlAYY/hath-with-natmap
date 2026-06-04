package natmap_test

import (
	natmap "github.com/ngnlAYY/hath-with-natter/internal/natmap"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNotifyArgsParsesTCPMapping(t *testing.T) {
	mapping, err := natmap.ParseNotifyArgs([]string{
		"203.0.113.10",
		"45678",
		"192.0.2.20",
		"16000",
		"TCP",
		"10.0.0.2",
	})
	if err != nil {
		t.Fatalf("natmap.ParseNotifyArgs returned error: %v", err)
	}

	expected := natmap.Mapping{
		PublicAddress:  "203.0.113.10",
		PublicPort:     45678,
		IP4P:           "192.0.2.20",
		PrivatePort:    16000,
		Protocol:       "TCP",
		PrivateAddress: "10.0.0.2",
	}
	if mapping != expected {
		t.Fatalf("natmap.ParseNotifyArgs() = %#v, want %#v", mapping, expected)
	}
}

func TestParseNotifyArgsNormalizesTCPProtocol(t *testing.T) {
	mapping, err := natmap.ParseNotifyArgs([]string{
		"203.0.113.10",
		"45678",
		"192.0.2.20",
		"16000",
		"Tcp",
		"10.0.0.2",
	})
	if err != nil {
		t.Fatalf("natmap.ParseNotifyArgs returned error: %v", err)
	}
	if mapping.Protocol != "TCP" {
		t.Fatalf("Protocol = %q, want TCP", mapping.Protocol)
	}
}

func TestParseNotifyArgsAcceptsPortBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		publicPort  string
		privatePort string
		wantPublic  int
		wantPrivate int
	}{
		{
			name:        "minimum ports",
			publicPort:  "1",
			privatePort: "1",
			wantPublic:  1,
			wantPrivate: 1,
		},
		{
			name:        "maximum ports",
			publicPort:  "65535",
			privatePort: "65535",
			wantPublic:  65535,
			wantPrivate: 65535,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapping, err := natmap.ParseNotifyArgs([]string{
				"203.0.113.10",
				tt.publicPort,
				"192.0.2.20",
				tt.privatePort,
				"TCP",
				"10.0.0.2",
			})
			if err != nil {
				t.Fatalf("natmap.ParseNotifyArgs returned error: %v", err)
			}
			if mapping.PublicPort != tt.wantPublic {
				t.Fatalf("PublicPort = %d, want %d", mapping.PublicPort, tt.wantPublic)
			}
			if mapping.PrivatePort != tt.wantPrivate {
				t.Fatalf("PrivatePort = %d, want %d", mapping.PrivatePort, tt.wantPrivate)
			}
		})
	}
}

func TestParseNotifyArgsRejectsWrongArgumentCount(t *testing.T) {
	_, err := natmap.ParseNotifyArgs([]string{"203.0.113.10", "45678"})
	if err == nil {
		t.Fatal("natmap.ParseNotifyArgs returned nil error for wrong argument count")
	}
}

func TestParseNotifyArgsRejectsNonTCPProtocol(t *testing.T) {
	_, err := natmap.ParseNotifyArgs([]string{
		"203.0.113.10",
		"45678",
		"192.0.2.20",
		"16000",
		"udp",
		"10.0.0.2",
	})
	if err == nil {
		t.Fatal("natmap.ParseNotifyArgs returned nil error for non-TCP protocol")
	}
}

func TestParseNotifyArgsRejectsInvalidPorts(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "public port text",
			args: []string{"203.0.113.10", "invalid", "192.0.2.20", "16000", "TCP", "10.0.0.2"},
		},
		{
			name: "private port text",
			args: []string{"203.0.113.10", "45678", "192.0.2.20", "invalid", "TCP", "10.0.0.2"},
		},
		{
			name: "zero public port",
			args: []string{"203.0.113.10", "0", "192.0.2.20", "16000", "TCP", "10.0.0.2"},
		},
		{
			name: "negative public port",
			args: []string{"203.0.113.10", "-1", "192.0.2.20", "16000", "TCP", "10.0.0.2"},
		},
		{
			name: "too large public port",
			args: []string{"203.0.113.10", "65536", "192.0.2.20", "16000", "TCP", "10.0.0.2"},
		},
		{
			name: "zero private port",
			args: []string{"203.0.113.10", "45678", "192.0.2.20", "0", "TCP", "10.0.0.2"},
		},
		{
			name: "negative private port",
			args: []string{"203.0.113.10", "45678", "192.0.2.20", "-1", "TCP", "10.0.0.2"},
		},
		{
			name: "too large private port",
			args: []string{"203.0.113.10", "45678", "192.0.2.20", "65536", "TCP", "10.0.0.2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := natmap.ParseNotifyArgs(tt.args)
			if err == nil {
				t.Fatal("natmap.ParseNotifyArgs returned nil error for invalid port")
			}
		})
	}
}

func TestListenNotifyRejectsExistingRegularFile(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	content := []byte("do not delete")
	if err := os.WriteFile(socketPath, content, 0o644); err != nil {
		t.Fatalf("create existing regular file: %v", err)
	}

	listener, events, err := natmap.ListenNotify(socketPath)
	if err == nil {
		if listener != nil {
			_ = listener.Close()
		}
		t.Fatal("natmap.ListenNotify returned nil error for existing regular file")
	}
	if listener != nil {
		t.Fatalf("natmap.ListenNotify returned listener %#v for existing regular file", listener)
	}
	if events != nil {
		t.Fatalf("natmap.ListenNotify returned events channel for existing regular file")
	}
	got, readErr := os.ReadFile(socketPath)
	if readErr != nil {
		t.Fatalf("existing regular file was removed or became unreadable: %v", readErr)
	}
	if string(got) != string(content) {
		t.Fatalf("existing regular file content = %q, want %q", got, content)
	}
	if !strings.Contains(err.Error(), "不是 Unix socket") {
		t.Fatalf("natmap.ListenNotify error = %q, want non-socket context", err.Error())
	}
}

func TestListenNotifyRestrictsSocketPermissions(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "notify.sock")

	listener, _, err := natmap.ListenNotify(socketPath)
	if err != nil {
		t.Fatalf("natmap.ListenNotify returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	info, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatalf("stat notify socket: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("notify socket permissions = %v, want 0600", info.Mode().Perm())
	}
}

func TestSamePublicEndpoint(t *testing.T) {
	base := natmap.Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, Protocol: "TCP"}
	same := natmap.Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, Protocol: "TCP", PrivatePort: 16000}
	differentAddress := natmap.Mapping{PublicAddress: "203.0.113.11", PublicPort: 45678, Protocol: "TCP"}
	differentPort := natmap.Mapping{PublicAddress: "203.0.113.10", PublicPort: 45679, Protocol: "TCP"}
	differentProtocol := natmap.Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, Protocol: "udp"}

	if !base.SamePublicEndpoint(same) {
		t.Fatal("SamePublicEndpoint returned false for matching public endpoint")
	}
	if base.SamePublicEndpoint(differentAddress) {
		t.Fatal("SamePublicEndpoint returned true for different public address")
	}
	if base.SamePublicEndpoint(differentPort) {
		t.Fatal("SamePublicEndpoint returned true for different public port")
	}
	if base.SamePublicEndpoint(differentProtocol) {
		t.Fatal("SamePublicEndpoint returned true for different protocol")
	}
}
