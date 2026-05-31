package natmap

import (
	"fmt"
	"strconv"
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

	publicPort, err := strconv.Atoi(args[1])
	if err != nil {
		return Mapping{}, fmt.Errorf("解析公网端口失败: %w", err)
	}
	privatePort, err := strconv.Atoi(args[3])
	if err != nil {
		return Mapping{}, fmt.Errorf("解析私有端口失败: %w", err)
	}
	if args[4] != "tcp" {
		return Mapping{}, fmt.Errorf("仅支持 TCP 协议，当前协议: %s", args[4])
	}

	return Mapping{
		PublicAddress:  args[0],
		PublicPort:     publicPort,
		IP4P:           args[2],
		PrivatePort:    privatePort,
		Protocol:       args[4],
		PrivateAddress: args[5],
	}, nil
}

func (m Mapping) SamePublicEndpoint(other Mapping) bool {
	return m.PublicAddress == other.PublicAddress &&
		m.PublicPort == other.PublicPort &&
		m.Protocol == other.Protocol
}
