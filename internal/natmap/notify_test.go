package natmap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"testing"
)

func TestParseNotifyArgsParsesTCPMapping(t *testing.T) {
	mapping, err := ParseNotifyArgs([]string{
		"203.0.113.10",
		"45678",
		"192.0.2.20",
		"16000",
		"TCP",
		"10.0.0.2",
	})
	if err != nil {
		t.Fatalf("ParseNotifyArgs returned error: %v", err)
	}

	expected := Mapping{
		PublicAddress:  "203.0.113.10",
		PublicPort:     45678,
		IP4P:           "192.0.2.20",
		PrivatePort:    16000,
		Protocol:       "TCP",
		PrivateAddress: "10.0.0.2",
	}
	if mapping != expected {
		t.Fatalf("ParseNotifyArgs() = %#v, want %#v", mapping, expected)
	}
}

func TestParseNotifyArgsNormalizesTCPProtocol(t *testing.T) {
	mapping, err := ParseNotifyArgs([]string{
		"203.0.113.10",
		"45678",
		"192.0.2.20",
		"16000",
		"Tcp",
		"10.0.0.2",
	})
	if err != nil {
		t.Fatalf("ParseNotifyArgs returned error: %v", err)
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
			mapping, err := ParseNotifyArgs([]string{
				"203.0.113.10",
				tt.publicPort,
				"192.0.2.20",
				tt.privatePort,
				"TCP",
				"10.0.0.2",
			})
			if err != nil {
				t.Fatalf("ParseNotifyArgs returned error: %v", err)
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
	_, err := ParseNotifyArgs([]string{"203.0.113.10", "45678"})
	if err == nil {
		t.Fatal("ParseNotifyArgs returned nil error for wrong argument count")
	}
}

func TestParseNotifyArgsRejectsNonTCPProtocol(t *testing.T) {
	_, err := ParseNotifyArgs([]string{
		"203.0.113.10",
		"45678",
		"192.0.2.20",
		"16000",
		"udp",
		"10.0.0.2",
	})
	if err == nil {
		t.Fatal("ParseNotifyArgs returned nil error for non-TCP protocol")
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
			_, err := ParseNotifyArgs(tt.args)
			if err == nil {
				t.Fatal("ParseNotifyArgs returned nil error for invalid port")
			}
		})
	}
}

func TestHandleNotifyConnLogsInvalidJSONAndContinues(t *testing.T) {
	var logs bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})

	client, server := net.Pipe()
	events := make(chan Mapping, 1)
	done := make(chan struct{})
	go func() {
		handleNotifyConn(server, events)
		close(done)
	}()

	want := Mapping{
		PublicAddress:  "203.0.113.10",
		PublicPort:     45678,
		IP4P:           "192.0.2.20",
		PrivatePort:    16000,
		Protocol:       "TCP",
		PrivateAddress: "10.0.0.2",
	}
	if _, err := fmt.Fprintln(client, "not-json"); err != nil {
		t.Fatalf("write invalid notify payload: %v", err)
	}
	if err := json.NewEncoder(client).Encode(want); err != nil {
		t.Fatalf("write valid notify payload: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close notify client: %v", err)
	}

	<-done

	select {
	case got := <-events:
		if got != want {
			t.Fatalf("handleNotifyConn emitted %#v, want %#v", got, want)
		}
	default:
		t.Fatal("handleNotifyConn did not emit valid mapping after invalid JSON")
	}
	if !strings.Contains(logs.String(), "解析 natmap notify 事件失败") {
		t.Fatalf("log output = %q, want Chinese JSON parse context", logs.String())
	}
}

func TestSamePublicEndpoint(t *testing.T) {
	base := Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, Protocol: "TCP"}
	same := Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, Protocol: "TCP", PrivatePort: 16000}
	differentAddress := Mapping{PublicAddress: "203.0.113.11", PublicPort: 45678, Protocol: "TCP"}
	differentPort := Mapping{PublicAddress: "203.0.113.10", PublicPort: 45679, Protocol: "TCP"}
	differentProtocol := Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, Protocol: "udp"}

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
