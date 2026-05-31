package natmap

import (
	"strconv"
	"time"
)

type RunnerConfig struct {
	BindPort            int
	StunServer          string
	HTTPKeepaliveServer string
	KeepaliveInterval   time.Duration
	NotifyScript        string
}

func (c RunnerConfig) Args() []string {
	return []string{
		"-4",
		"-b", strconv.Itoa(c.BindPort),
		"-s", c.StunServer,
		"-h", c.HTTPKeepaliveServer,
		"-k", strconv.FormatInt(int64(c.KeepaliveInterval/time.Second), 10),
		"-e", c.NotifyScript,
	}
}
