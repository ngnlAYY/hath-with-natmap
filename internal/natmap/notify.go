package natmap

import (
	"fmt"
	"strconv"
	"strings"
)

const notifyArgCount = 6

type Mapping struct {
	PublicAddress  string
	PublicPort     int
	IP4P           string
	PrivatePort    int
	Protocol       string
	PrivateAddress string
}

func ParseNotifyArgs(args []string) (Mapping, error) {
	if len(args) != notifyArgCount {
		return Mapping{}, fmt.Errorf("natmap 通知参数数量错误: 需要 %d 个，实际 %d 个", notifyArgCount, len(args))
	}

	publicPort, err := parsePort("公网端口", args[1])
	if err != nil {
		return Mapping{}, err
	}
	privatePort, err := parsePort("本地端口", args[3])
	if err != nil {
		return Mapping{}, err
	}
	protocol := strings.ToUpper(args[4])
	if protocol != "TCP" {
		return Mapping{}, fmt.Errorf("仅支持 TCP 协议，当前协议: %s", args[4])
	}

	return Mapping{
		PublicAddress:  args[0],
		PublicPort:     publicPort,
		IP4P:           args[2],
		PrivatePort:    privatePort,
		Protocol:       protocol,
		PrivateAddress: args[5],
	}, nil
}

func (m Mapping) SamePublicEndpoint(other Mapping) bool {
	return m.PublicAddress == other.PublicAddress &&
		m.PublicPort == other.PublicPort &&
		m.Protocol == other.Protocol
}

func parsePort(name, value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("解析%s失败: %w", name, err)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s必须是 1 到 65535 之间的端口", name)
	}
	return port, nil
}
