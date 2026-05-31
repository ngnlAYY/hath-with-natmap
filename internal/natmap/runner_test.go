package natmap

import (
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
