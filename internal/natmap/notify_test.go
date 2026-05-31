package natmap

import "testing"

func TestParseNotifyArgsParsesTCPMapping(t *testing.T) {
	mapping, err := ParseNotifyArgs([]string{
		"203.0.113.10",
		"45678",
		"192.0.2.20",
		"16000",
		"tcp",
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
		Protocol:       "tcp",
		PrivateAddress: "10.0.0.2",
	}
	if mapping != expected {
		t.Fatalf("ParseNotifyArgs() = %#v, want %#v", mapping, expected)
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
			name: "public port",
			args: []string{"203.0.113.10", "invalid", "192.0.2.20", "16000", "tcp", "10.0.0.2"},
		},
		{
			name: "private port",
			args: []string{"203.0.113.10", "45678", "192.0.2.20", "invalid", "tcp", "10.0.0.2"},
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

func TestSamePublicEndpoint(t *testing.T) {
	base := Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, Protocol: "tcp"}
	same := Mapping{PublicAddress: "203.0.113.10", PublicPort: 45678, Protocol: "tcp", PrivatePort: 16000}
	differentAddress := Mapping{PublicAddress: "203.0.113.11", PublicPort: 45678, Protocol: "tcp"}
	differentPort := Mapping{PublicAddress: "203.0.113.10", PublicPort: 45679, Protocol: "tcp"}
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
