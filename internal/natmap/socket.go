package natmap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
)

const DefaultNotifySocket = "/run/hath-natmap/notify.sock"

type Listener struct {
	listener net.Listener
}

func (l *Listener) Close() error {
	return l.listener.Close()
}

func ListenNotify(socketPath string) (*Listener, <-chan Mapping, error) {
	_ = os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("监听 natmap notify socket 失败: %w", err)
	}

	events := make(chan Mapping, 16)
	notifyListener := &Listener{listener: listener}
	go acceptNotifyLoop(listener, events)

	return notifyListener, events, nil
}

func SendNotify(socketPath string, args []string) error {
	mapping, err := ParseNotifyArgs(args)
	if err != nil {
		return err
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("连接 natmap notify socket 失败: %w", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(mapping); err != nil {
		return fmt.Errorf("发送 natmap notify 事件失败: %w", err)
	}
	return nil
}

func acceptNotifyLoop(listener net.Listener, events chan<- Mapping) {
	var handlers sync.WaitGroup
	defer func() {
		handlers.Wait()
		close(events)
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			handleNotifyConn(conn, events)
		}()
	}
}

func handleNotifyConn(conn net.Conn, events chan<- Mapping) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var mapping Mapping
		if err := json.Unmarshal(scanner.Bytes(), &mapping); err != nil {
			continue
		}
		events <- mapping
	}
}
