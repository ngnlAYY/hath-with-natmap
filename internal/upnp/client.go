package upnp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/huin/goupnp"
	internetgateway1 "github.com/huin/goupnp/dcps/internetgateway1"
	internetgateway2 "github.com/huin/goupnp/dcps/internetgateway2"
)

const (
	tcpProtocol              = "TCP"
	defaultHTTPClientTimeout = 30 * time.Second
	defaultHTTPDialTimeout   = 5 * time.Second
	defaultHTTPHeaderTimeout = 10 * time.Second
)

var goupnpHTTPClientMu sync.Mutex

type Config struct {
	Port          int
	LeaseDuration uint32
	Description   string
}

type Mapping struct {
	PublicAddress  string
	PublicPort     int
	PrivatePort    int
	PrivateAddress string
}

type WANService interface {
	AddPortMapping(remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseDuration uint32) error
	DeletePortMapping(remoteHost string, externalPort uint16, protocol string) error
	GetExternalIPAddress() (string, error)
	LocalAddress() string
}

type DiscoverFunc func(ctx context.Context) (WANService, error)

type Client struct {
	Discover   DiscoverFunc
	HTTPClient *http.Client
}

type addPortMappingContextAware interface {
	AddPortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseDuration uint32) error
}

type deletePortMappingContextAware interface {
	DeletePortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string) error
}

type externalIPAddressContextAware interface {
	GetExternalIPAddressCtx(ctx context.Context) (string, error)
}

type serviceHostAware interface {
	ServiceHost() string
}

type goupnpWANClient interface {
	AddPortMapping(remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseDuration uint32) error
	AddPortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseDuration uint32) error
	DeletePortMapping(remoteHost string, externalPort uint16, protocol string) error
	DeletePortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string) error
	GetExternalIPAddress() (string, error)
	GetExternalIPAddressCtx(ctx context.Context) (string, error)
	GetServiceClient() *goupnp.ServiceClient
}

type wanServiceAdapter struct {
	client     goupnpWANClient
	httpClient *http.Client
}

func (a wanServiceAdapter) AddPortMapping(remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseDuration uint32) error {
	return a.withSOAPHTTPClient(func() error {
		return a.client.AddPortMapping(remoteHost, externalPort, protocol, internalPort, internalClient, enabled, description, leaseDuration)
	})
}

func (a wanServiceAdapter) AddPortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseDuration uint32) error {
	return a.withSOAPHTTPClient(func() error {
		return a.client.AddPortMappingCtx(ctx, remoteHost, externalPort, protocol, internalPort, internalClient, enabled, description, leaseDuration)
	})
}

func (a wanServiceAdapter) DeletePortMapping(remoteHost string, externalPort uint16, protocol string) error {
	return a.withSOAPHTTPClient(func() error {
		return a.client.DeletePortMapping(remoteHost, externalPort, protocol)
	})
}

func (a wanServiceAdapter) DeletePortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string) error {
	return a.withSOAPHTTPClient(func() error {
		return a.client.DeletePortMappingCtx(ctx, remoteHost, externalPort, protocol)
	})
}

func (a wanServiceAdapter) GetExternalIPAddress() (string, error) {
	return withSOAPHTTPClient(a.client.GetServiceClient(), a.httpClient, a.client.GetExternalIPAddress)
}

func (a wanServiceAdapter) GetExternalIPAddressCtx(ctx context.Context) (string, error) {
	return withSOAPHTTPClient(a.client.GetServiceClient(), a.httpClient, func() (string, error) {
		return a.client.GetExternalIPAddressCtx(ctx)
	})
}

func (a wanServiceAdapter) withSOAPHTTPClient(fn func() error) error {
	_, err := withSOAPHTTPClient(a.client.GetServiceClient(), a.httpClient, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

func (a wanServiceAdapter) LocalAddress() string {
	serviceClient := a.client.GetServiceClient()
	if serviceClient == nil {
		return ""
	}
	localAddr := serviceClient.LocalAddr()
	if localAddr == nil {
		return ""
	}
	return localAddressHost(localAddr.String())
}

func (a wanServiceAdapter) ServiceHost() string {
	serviceClient := a.client.GetServiceClient()
	if serviceClient == nil {
		return ""
	}
	if serviceClient.Service != nil && serviceClient.Service.ControlURL.Ok {
		return serviceHostFromURL(serviceClient.Service.ControlURL.URL.Host)
	}
	if serviceClient.SOAPClient != nil {
		return serviceHostFromURL(serviceClient.SOAPClient.EndpointURL.Host)
	}
	if serviceClient.Location != nil {
		return serviceHostFromURL(serviceClient.Location.Host)
	}
	return ""
}

func withSOAPHTTPClient[T any](serviceClient *goupnp.ServiceClient, httpClient *http.Client, fn func() (T, error)) (T, error) {
	if serviceClient == nil || serviceClient.SOAPClient == nil || httpClient == nil {
		return fn()
	}
	previous := serviceClient.SOAPClient.HTTPClient
	serviceClient.SOAPClient.HTTPClient = *httpClient
	defer func() {
		serviceClient.SOAPClient.HTTPClient = previous
	}()
	return fn()
}

func (c Client) AddMapping(ctx context.Context, cfg Config) (Mapping, error) {
	port, err := validatePort(cfg.Port)
	if err != nil {
		return Mapping{}, err
	}
	if err := ctx.Err(); err != nil {
		return Mapping{}, err
	}

	service, err := c.discover(ctx)
	if err != nil {
		return Mapping{}, fmt.Errorf("发现 UPnP WAN 服务失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Mapping{}, err
	}
	if err := validateServiceTrustBoundary(service); err != nil {
		return Mapping{}, fmt.Errorf("发现 UPnP WAN 服务失败: %w", err)
	}

	privateAddress := service.LocalAddress()
	if err := addPortMapping(ctx, service, port, privateAddress, cfg); err != nil {
		return Mapping{}, fmt.Errorf("添加 UPnP TCP 端口映射失败: %w", err)
	}

	publicAddress, err := getExternalIPAddress(ctx, service)
	if err != nil {
		publicAddress = ""
	}

	return Mapping{
		PublicAddress:  publicAddress,
		PublicPort:     cfg.Port,
		PrivatePort:    cfg.Port,
		PrivateAddress: privateAddress,
	}, nil
}

func (c Client) DeleteMapping(ctx context.Context, cfg Config) error {
	port, err := validatePort(cfg.Port)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	service, err := c.discover(ctx)
	if err != nil {
		return fmt.Errorf("发现 UPnP WAN 服务失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateServiceTrustBoundary(service); err != nil {
		return fmt.Errorf("发现 UPnP WAN 服务失败: %w", err)
	}

	if err := deletePortMapping(ctx, service, port); err != nil {
		return fmt.Errorf("删除 UPnP TCP 端口映射失败: %w", err)
	}
	return nil
}

func Discover(ctx context.Context) (WANService, error) {
	return discover(ctx, nil)
}

func discover(ctx context.Context, httpClient *http.Client) (WANService, error) {
	discoverers := []func(context.Context, *http.Client) (WANService, error){
		discoverWANIPConnection2,
		discoverWANIPConnectionV2,
		discoverWANPPPConnectionV2,
		discoverWANIPConnectionV1,
		discoverWANPPPConnectionV1,
	}

	var errs []error
	for _, discover := range discoverers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		service, err := discover(ctx, httpClient)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if service != nil {
			return service, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("未发现可用的 UPnP WAN 服务: %w", err)
	}
	return nil, errors.New("未发现可用的 UPnP WAN 服务")
}

func (c Client) discover(ctx context.Context) (WANService, error) {
	if c.Discover != nil {
		return c.Discover(ctx)
	}
	httpClient := c.httpClient()
	return withGoupnpHTTPClient(httpClient, func() (WANService, error) {
		return discover(ctx, httpClient)
	})
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{
		Timeout: defaultHTTPClientTimeout,
		Transport: trustedLANTransport{
			base: &http.Transport{
				Proxy: nil,
				DialContext: (&net.Dialer{
					Timeout: defaultHTTPDialTimeout,
				}).DialContext,
				ResponseHeaderTimeout: defaultHTTPHeaderTimeout,
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return validateTrustedLANURL(req.URL)
		},
	}
}

type trustedLANTransport struct {
	base http.RoundTripper
}

func (t trustedLANTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := validateTrustedLANURL(req.URL); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func validateTrustedLANURL(target *url.URL) error {
	if target == nil {
		return errors.New("UPnP 目标 URL 为空")
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return fmt.Errorf("UPnP 目标 URL 协议不可信: %s", target.Scheme)
	}
	if !isTrustedRemoteLANHost(target.Host) {
		return fmt.Errorf("UPnP 目标地址不可信: %s", target.Host)
	}
	return nil
}

func withGoupnpHTTPClient[T any](client *http.Client, fn func() (T, error)) (T, error) {
	goupnpHTTPClientMu.Lock()
	previous := goupnp.HTTPClientDefault
	goupnp.HTTPClientDefault = client
	defer func() {
		goupnp.HTTPClientDefault = previous
		goupnpHTTPClientMu.Unlock()
	}()
	return fn()
}

func discoverWANIPConnection2(ctx context.Context, httpClient *http.Client) (WANService, error) {
	clients, errs, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx)
	if err != nil {
		return nil, err
	}
	return firstWANIPConnection2(clients, errs, httpClient)
}

func discoverWANIPConnectionV2(ctx context.Context, httpClient *http.Client) (WANService, error) {
	clients, errs, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx)
	if err != nil {
		return nil, err
	}
	return firstWANIPConnection1V2(clients, errs, httpClient)
}

func discoverWANPPPConnectionV2(ctx context.Context, httpClient *http.Client) (WANService, error) {
	clients, errs, err := internetgateway2.NewWANPPPConnection1ClientsCtx(ctx)
	if err != nil {
		return nil, err
	}
	return firstWANPPPConnection1V2(clients, errs, httpClient)
}

func discoverWANIPConnectionV1(ctx context.Context, httpClient *http.Client) (WANService, error) {
	clients, errs, err := internetgateway1.NewWANIPConnection1ClientsCtx(ctx)
	if err != nil {
		return nil, err
	}
	return firstWANIPConnection1V1(clients, errs, httpClient)
}

func discoverWANPPPConnectionV1(ctx context.Context, httpClient *http.Client) (WANService, error) {
	clients, errs, err := internetgateway1.NewWANPPPConnection1ClientsCtx(ctx)
	if err != nil {
		return nil, err
	}
	return firstWANPPPConnection1V1(clients, errs, httpClient)
}

func firstWANIPConnection2(clients []*internetgateway2.WANIPConnection2, errs []error, httpClient *http.Client) (WANService, error) {
	for _, client := range clients {
		if client != nil {
			return wanServiceAdapter{client: client, httpClient: httpClient}, nil
		}
	}
	return nil, joinErrors(errs)
}

func firstWANIPConnection1V2(clients []*internetgateway2.WANIPConnection1, errs []error, httpClient *http.Client) (WANService, error) {
	for _, client := range clients {
		if client != nil {
			return wanServiceAdapter{client: client, httpClient: httpClient}, nil
		}
	}
	return nil, joinErrors(errs)
}

func firstWANPPPConnection1V2(clients []*internetgateway2.WANPPPConnection1, errs []error, httpClient *http.Client) (WANService, error) {
	for _, client := range clients {
		if client != nil {
			return wanServiceAdapter{client: client, httpClient: httpClient}, nil
		}
	}
	return nil, joinErrors(errs)
}

func firstWANIPConnection1V1(clients []*internetgateway1.WANIPConnection1, errs []error, httpClient *http.Client) (WANService, error) {
	for _, client := range clients {
		if client != nil {
			return wanServiceAdapter{client: client, httpClient: httpClient}, nil
		}
	}
	return nil, joinErrors(errs)
}

func firstWANPPPConnection1V1(clients []*internetgateway1.WANPPPConnection1, errs []error, httpClient *http.Client) (WANService, error) {
	for _, client := range clients {
		if client != nil {
			return wanServiceAdapter{client: client, httpClient: httpClient}, nil
		}
	}
	return nil, joinErrors(errs)
}

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func addPortMapping(ctx context.Context, service WANService, port uint16, privateAddress string, cfg Config) error {
	if contextAware, ok := service.(addPortMappingContextAware); ok {
		return contextAware.AddPortMappingCtx(ctx, "", port, tcpProtocol, port, privateAddress, true, cfg.Description, cfg.LeaseDuration)
	}
	return service.AddPortMapping("", port, tcpProtocol, port, privateAddress, true, cfg.Description, cfg.LeaseDuration)
}

func deletePortMapping(ctx context.Context, service WANService, port uint16) error {
	if contextAware, ok := service.(deletePortMappingContextAware); ok {
		return contextAware.DeletePortMappingCtx(ctx, "", port, tcpProtocol)
	}
	return service.DeletePortMapping("", port, tcpProtocol)
}

func getExternalIPAddress(ctx context.Context, service WANService) (string, error) {
	if contextAware, ok := service.(externalIPAddressContextAware); ok {
		return contextAware.GetExternalIPAddressCtx(ctx)
	}
	return service.GetExternalIPAddress()
}

func validatePort(port int) (uint16, error) {
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("端口必须在 1 到 65535 之间: %d", port)
	}
	return uint16(port), nil
}

func validateServiceTrustBoundary(service WANService) error {
	localHost := service.LocalAddress()
	if localHost == "" {
		return errors.New("UPnP 本地地址为空")
	}
	if !isTrustedLANHost(localHost) {
		return fmt.Errorf("UPnP 本地地址不可信: %s", localHost)
	}

	if hostAware, ok := service.(serviceHostAware); ok {
		serviceHost := serviceHostFromURL(hostAware.ServiceHost())
		if serviceHost != "" && !isTrustedRemoteLANHost(serviceHost) {
			return fmt.Errorf("UPnP 服务地址不可信: %s", serviceHost)
		}
	}
	return nil
}

func isTrustedLANHost(host string) bool {
	ip := parseHostIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func isTrustedRemoteLANHost(host string) bool {
	ip := parseHostIP(host)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.To4() == nil && ip.IsLinkLocalUnicast()
}

func parseHostIP(host string) net.IP {
	host = localAddressHost(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}
	if zoneIndex := strings.Index(host, "%"); zoneIndex >= 0 {
		host = host[:zoneIndex]
	}
	return net.ParseIP(host)
}

func serviceHostFromURL(hostport string) string {
	return localAddressHost(hostport)
}

func localAddressHost(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}
