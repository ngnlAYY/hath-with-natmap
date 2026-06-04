package upnp_test

import (
	"context"
	"errors"
	upnp "github.com/ngnlAYY/hath-with-natter/internal/upnp"
	"strings"
	"testing"
)

type fakeWANService struct {
	addCalled         bool
	addArgs           addPortMappingCall
	addErr            error
	addCtxCalled      bool
	addCtx            context.Context
	deleteCalled      bool
	deleteArgs        deletePortMappingCall
	deleteErr         error
	deleteCtxCalled   bool
	deleteCtx         context.Context
	externalIP        string
	externalErr       error
	externalCtxCalled bool
	externalCtx       context.Context
	localAddr         string
	serviceHost       string
}

type addPortMappingCall struct {
	remoteHost     string
	externalPort   uint16
	protocol       string
	internalPort   uint16
	internalClient string
	enabled        bool
	description    string
	leaseDuration  uint32
}

type deletePortMappingCall struct {
	remoteHost   string
	externalPort uint16
	protocol     string
}

type legacyWANService struct {
	addCalled    bool
	addArgs      addPortMappingCall
	addErr       error
	deleteCalled bool
	deleteArgs   deletePortMappingCall
	deleteErr    error
	externalIP   string
	externalErr  error
	localAddr    string
}

func (f *legacyWANService) AddPortMapping(remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseDuration uint32) error {
	f.addCalled = true
	f.addArgs = addPortMappingCall{
		remoteHost:     remoteHost,
		externalPort:   externalPort,
		protocol:       protocol,
		internalPort:   internalPort,
		internalClient: internalClient,
		enabled:        enabled,
		description:    description,
		leaseDuration:  leaseDuration,
	}
	return f.addErr
}

func (f *legacyWANService) DeletePortMapping(remoteHost string, externalPort uint16, protocol string) error {
	f.deleteCalled = true
	f.deleteArgs = deletePortMappingCall{
		remoteHost:   remoteHost,
		externalPort: externalPort,
		protocol:     protocol,
	}
	return f.deleteErr
}

func (f *legacyWANService) GetExternalIPAddress() (string, error) {
	return f.externalIP, f.externalErr
}

func (f *legacyWANService) LocalAddress() string {
	return f.localAddr
}

func (f *fakeWANService) AddPortMapping(remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseDuration uint32) error {
	f.addCalled = true
	f.addArgs = addPortMappingCall{
		remoteHost:     remoteHost,
		externalPort:   externalPort,
		protocol:       protocol,
		internalPort:   internalPort,
		internalClient: internalClient,
		enabled:        enabled,
		description:    description,
		leaseDuration:  leaseDuration,
	}
	return f.addErr
}

func (f *fakeWANService) AddPortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseDuration uint32) error {
	f.addCtxCalled = true
	f.addCtx = ctx
	f.addArgs = addPortMappingCall{
		remoteHost:     remoteHost,
		externalPort:   externalPort,
		protocol:       protocol,
		internalPort:   internalPort,
		internalClient: internalClient,
		enabled:        enabled,
		description:    description,
		leaseDuration:  leaseDuration,
	}
	return f.addErr
}

func (f *fakeWANService) DeletePortMapping(remoteHost string, externalPort uint16, protocol string) error {
	f.deleteCalled = true
	f.deleteArgs = deletePortMappingCall{
		remoteHost:   remoteHost,
		externalPort: externalPort,
		protocol:     protocol,
	}
	return f.deleteErr
}

func (f *fakeWANService) DeletePortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string) error {
	f.deleteCtxCalled = true
	f.deleteCtx = ctx
	f.deleteArgs = deletePortMappingCall{
		remoteHost:   remoteHost,
		externalPort: externalPort,
		protocol:     protocol,
	}
	return f.deleteErr
}

func (f *fakeWANService) GetExternalIPAddress() (string, error) {
	return f.externalIP, f.externalErr
}

func (f *fakeWANService) GetExternalIPAddressCtx(ctx context.Context) (string, error) {
	f.externalCtxCalled = true
	f.externalCtx = ctx
	return f.externalIP, f.externalErr
}

func (f *fakeWANService) LocalAddress() string {
	return f.localAddr
}

func (f *fakeWANService) ServiceHost() string {
	return f.serviceHost
}

func TestClientAddMappingAddsTCPPortMapping(t *testing.T) {
	service := &legacyWANService{
		externalIP: "203.0.113.10",
		localAddr:  "192.168.1.10",
	}
	client := upnp.Client{
		Discover: func(ctx context.Context) (upnp.WANService, error) {
			return service, nil
		},
	}
	cfg := upnp.Config{
		Port:          16000,
		LeaseDuration: 3600,
		Description:   "hath-with-natter",
	}

	mapping, err := client.AddMapping(context.Background(), cfg)
	if err != nil {
		t.Fatalf("AddMapping returned error: %v", err)
	}
	if !service.addCalled {
		t.Fatal("AddPortMapping was not called")
	}
	if service.addArgs.remoteHost != "" {
		t.Fatalf("remoteHost = %q, want empty", service.addArgs.remoteHost)
	}
	if service.addArgs.externalPort != 16000 {
		t.Fatalf("externalPort = %d, want 16000", service.addArgs.externalPort)
	}
	if service.addArgs.protocol != "TCP" {
		t.Fatalf("protocol = %q, want TCP", service.addArgs.protocol)
	}
	if service.addArgs.internalPort != 16000 {
		t.Fatalf("internalPort = %d, want 16000", service.addArgs.internalPort)
	}
	if service.addArgs.internalClient != "192.168.1.10" {
		t.Fatalf("internalClient = %q, want 192.168.1.10", service.addArgs.internalClient)
	}
	if !service.addArgs.enabled {
		t.Fatal("enabled = false, want true")
	}
	if service.addArgs.description != "hath-with-natter" {
		t.Fatalf("description = %q, want hath-with-natter", service.addArgs.description)
	}
	if service.addArgs.leaseDuration != 3600 {
		t.Fatalf("leaseDuration = %d, want 3600", service.addArgs.leaseDuration)
	}

	if mapping.PublicAddress != "203.0.113.10" {
		t.Fatalf("PublicAddress = %q, want 203.0.113.10", mapping.PublicAddress)
	}
	if mapping.PublicPort != 16000 {
		t.Fatalf("PublicPort = %d, want 16000", mapping.PublicPort)
	}
	if mapping.PrivatePort != 16000 {
		t.Fatalf("PrivatePort = %d, want 16000", mapping.PrivatePort)
	}
	if mapping.PrivateAddress != "192.168.1.10" {
		t.Fatalf("PrivateAddress = %q, want 192.168.1.10", mapping.PrivateAddress)
	}
}

func TestClientAddMappingContinuesWhenExternalIPFails(t *testing.T) {
	service := &fakeWANService{
		externalErr: errors.New("external ip failed"),
		localAddr:   "192.168.1.10",
	}
	client := upnp.Client{
		Discover: func(ctx context.Context) (upnp.WANService, error) {
			return service, nil
		},
	}

	mapping, err := client.AddMapping(context.Background(), upnp.Config{Port: 16000})
	if err != nil {
		t.Fatalf("AddMapping returned error: %v", err)
	}
	if mapping.PublicAddress != "" {
		t.Fatalf("PublicAddress = %q, want empty", mapping.PublicAddress)
	}
}

func TestClientAddMappingReturnsDiscoverError(t *testing.T) {
	wantErr := errors.New("discover failed")
	client := upnp.Client{
		Discover: func(ctx context.Context) (upnp.WANService, error) {
			return nil, wantErr
		},
	}

	_, err := client.AddMapping(context.Background(), upnp.Config{Port: 16000})
	if err == nil {
		t.Fatal("AddMapping returned nil error, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("errors.Is(err, wantErr) = false, err = %v", err)
	}
	if !strings.Contains(err.Error(), "发现") {
		t.Fatalf("error = %q, want Chinese discover context", err.Error())
	}
}

func TestClientAddMappingReturnsAddError(t *testing.T) {
	wantErr := errors.New("add failed")
	service := &fakeWANService{
		addErr:    wantErr,
		localAddr: "192.168.1.10",
	}
	client := upnp.Client{
		Discover: func(ctx context.Context) (upnp.WANService, error) {
			return service, nil
		},
	}

	_, err := client.AddMapping(context.Background(), upnp.Config{Port: 16000})
	if err == nil {
		t.Fatal("AddMapping returned nil error, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("errors.Is(err, wantErr) = false, err = %v", err)
	}
	if !strings.Contains(err.Error(), "添加") {
		t.Fatalf("error = %q, want Chinese add context", err.Error())
	}
}

func TestClientAddMappingPrefersContextAwareMethods(t *testing.T) {
	service := &fakeWANService{
		externalIP: "203.0.113.10",
		localAddr:  "192.168.1.10",
	}
	client := upnp.Client{
		Discover: func(ctx context.Context) (upnp.WANService, error) {
			return service, nil
		},
	}
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")

	_, err := client.AddMapping(ctx, upnp.Config{Port: 16000})
	if err != nil {
		t.Fatalf("AddMapping returned error: %v", err)
	}
	if !service.addCtxCalled {
		t.Fatal("AddPortMappingCtx was not called")
	}
	if service.addCalled {
		t.Fatal("AddPortMapping fallback was called unexpectedly")
	}
	if service.addCtx != ctx {
		t.Fatal("AddPortMappingCtx did not receive caller context")
	}
	if !service.externalCtxCalled {
		t.Fatal("GetExternalIPAddressCtx was not called")
	}
	if service.externalCtx != ctx {
		t.Fatal("GetExternalIPAddressCtx did not receive caller context")
	}
}

func TestClientDeleteMappingDeletesTCPPortMapping(t *testing.T) {
	service := &legacyWANService{localAddr: "192.168.1.10"}
	client := upnp.Client{
		Discover: func(ctx context.Context) (upnp.WANService, error) {
			return service, nil
		},
	}

	if err := client.DeleteMapping(context.Background(), upnp.Config{Port: 16000}); err != nil {
		t.Fatalf("DeleteMapping returned error: %v", err)
	}
	if !service.deleteCalled {
		t.Fatal("DeletePortMapping was not called")
	}
	if service.deleteArgs.remoteHost != "" {
		t.Fatalf("remoteHost = %q, want empty", service.deleteArgs.remoteHost)
	}
	if service.deleteArgs.externalPort != 16000 {
		t.Fatalf("externalPort = %d, want 16000", service.deleteArgs.externalPort)
	}
	if service.deleteArgs.protocol != "TCP" {
		t.Fatalf("protocol = %q, want TCP", service.deleteArgs.protocol)
	}
}

func TestClientDeleteMappingPrefersContextAwareMethod(t *testing.T) {
	service := &fakeWANService{localAddr: "192.168.1.10"}
	client := upnp.Client{
		Discover: func(ctx context.Context) (upnp.WANService, error) {
			return service, nil
		},
	}
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")

	if err := client.DeleteMapping(ctx, upnp.Config{Port: 16000}); err != nil {
		t.Fatalf("DeleteMapping returned error: %v", err)
	}
	if !service.deleteCtxCalled {
		t.Fatal("DeletePortMappingCtx was not called")
	}
	if service.deleteCalled {
		t.Fatal("DeletePortMapping fallback was called unexpectedly")
	}
	if service.deleteCtx != ctx {
		t.Fatal("DeletePortMappingCtx did not receive caller context")
	}
}

func TestClientAddMappingRejectsUntrustedLocalAddress(t *testing.T) {
	service := &fakeWANService{localAddr: "203.0.113.10"}
	client := upnp.Client{
		Discover: func(ctx context.Context) (upnp.WANService, error) {
			return service, nil
		},
	}

	_, err := client.AddMapping(context.Background(), upnp.Config{Port: 16000})
	if err == nil || !strings.Contains(err.Error(), "UPnP 本地地址不可信") {
		t.Fatalf("AddMapping error = %v, want untrusted local address error", err)
	}
	if service.addCtxCalled || service.addCalled {
		t.Fatal("AddPortMapping was called for untrusted local address")
	}
}

func TestClientAddMappingRejectsUntrustedServiceHost(t *testing.T) {
	tests := []string{"198.51.100.1", "127.0.0.1", "169.254.169.254"}
	for _, serviceHost := range tests {
		t.Run(serviceHost, func(t *testing.T) {
			service := &fakeWANService{localAddr: "192.168.1.10", serviceHost: serviceHost}
			client := upnp.Client{
				Discover: func(ctx context.Context) (upnp.WANService, error) {
					return service, nil
				},
			}

			_, err := client.AddMapping(context.Background(), upnp.Config{Port: 16000})
			if err == nil || !strings.Contains(err.Error(), "UPnP 服务地址不可信") {
				t.Fatalf("AddMapping error = %v, want untrusted service host error", err)
			}
			if service.addCtxCalled || service.addCalled {
				t.Fatal("AddPortMapping was called for untrusted service host")
			}
		})
	}
}

func TestClientAddMappingAllowsIPv6LinkLocalServiceHost(t *testing.T) {
	service := &fakeWANService{localAddr: "192.168.1.10", serviceHost: "[fe80::1%eth0]:80"}
	client := upnp.Client{
		Discover: func(ctx context.Context) (upnp.WANService, error) {
			return service, nil
		},
	}

	_, err := client.AddMapping(context.Background(), upnp.Config{Port: 16000})
	if err != nil {
		t.Fatalf("AddMapping returned error: %v", err)
	}
	if !service.addCtxCalled {
		t.Fatal("AddPortMappingCtx was not called")
	}
}
