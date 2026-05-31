package natmap

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRunnerConfigArgsBuildsBindModeArguments(t *testing.T) {
	cfg := RunnerConfig{
		BindPort:            16000,
		StunServer:          "stun.example.com:3478",
		HTTPKeepaliveServer: "https://keepalive.example.com",
		KeepaliveInterval:   30 * time.Second,
		NotifyScript:        "/usr/local/bin/natmap-notify",
	}

	got := cfg.Args()
	want := []string{
		"-4",
		"-b", "16000",
		"-s", "stun.example.com:3478",
		"-h", "https://keepalive.example.com",
		"-k", "30",
		"-e", "/usr/local/bin/natmap-notify",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

func TestListenNotifyReceivesMappingSentBySendNotify(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	listener, events, err := ListenNotify(socketPath)
	if err != nil {
		t.Fatalf("ListenNotify returned error: %v", err)
	}
	defer listener.Close()

	err = SendNotify(socketPath, []string{
		"203.0.113.10",
		"45678",
		"192.0.2.20",
		"16000",
		"TCP",
		"10.0.0.2",
	})
	if err != nil {
		t.Fatalf("SendNotify returned error: %v", err)
	}

	select {
	case mapping := <-events:
		if mapping.PublicPort != 45678 {
			t.Fatalf("PublicPort = %d, want 45678", mapping.PublicPort)
		}
		if mapping.PrivatePort != 16000 {
			t.Fatalf("PrivatePort = %d, want 16000", mapping.PrivatePort)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notify mapping")
	}
}
