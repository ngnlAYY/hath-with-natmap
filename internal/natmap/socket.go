package natmap

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
)

const DefaultNotifySocket = "/run/hath-natmap/notify.sock"
const NotifyTokenEnv = "HATH_NATMAP_NOTIFY_TOKEN"

type Listener struct {
	listener  net.Listener
	done      chan struct{}
	token     string
	closeOnce sync.Once
	closeErr  error
	connsMu   sync.Mutex
	conns     map[net.Conn]struct{}
}

type notifyEnvelope struct {
	Token   string  `json:"token"`
	Mapping Mapping `json:"mapping"`
}

func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		close(l.done)
		l.connsMu.Lock()
		for conn := range l.conns {
			_ = conn.Close()
		}
		l.connsMu.Unlock()
		l.closeErr = l.listener.Close()
	})
	return l.closeErr
}

func GenerateNotifyToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 natmap notify token 失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func ListenNotify(socketPath string) (*Listener, <-chan Mapping, error) {
	return ListenNotifyWithToken(socketPath, os.Getenv(NotifyTokenEnv))
}

func ListenNotifyWithToken(socketPath string, token string) (*Listener, <-chan Mapping, error) {
	if err := removeExistingNotifySocket(socketPath); err != nil {
		return nil, nil, err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("监听 natmap notify socket 失败: %w", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		return nil, nil, fmt.Errorf("设置 natmap notify socket 权限失败: %w", err)
	}

	events := make(chan Mapping, 16)
	notifyListener := &Listener{
		listener: listener,
		done:     make(chan struct{}),
		token:    token,
		conns:    make(map[net.Conn]struct{}),
	}
	go acceptNotifyLoop(notifyListener, events)

	return notifyListener, events, nil
}

func removeExistingNotifySocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("检查 natmap notify socket 路径失败: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("natmap notify socket 路径已存在但不是 Unix socket: %s", socketPath)
	}
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("删除已有 natmap notify socket 失败: %w", err)
	}
	return nil
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

	encoder := json.NewEncoder(conn)
	if token := os.Getenv(NotifyTokenEnv); token != "" {
		if err := encoder.Encode(notifyEnvelope{Token: token, Mapping: mapping}); err != nil {
			return fmt.Errorf("发送 natmap notify 事件失败: %w", err)
		}
		return nil
	}

	if err := encoder.Encode(mapping); err != nil {
		return fmt.Errorf("发送 natmap notify 事件失败: %w", err)
	}
	return nil
}

func acceptNotifyLoop(listener *Listener, events chan<- Mapping) {
	var handlers sync.WaitGroup
	defer func() {
		handlers.Wait()
		close(events)
	}()

	for {
		conn, err := listener.listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("接收 natmap notify 连接失败: %v", err)
			}
			return
		}
		listener.trackConn(conn)
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			defer listener.untrackConn(conn)
			handleNotifyConn(conn, events, listener.done, listener.token)
		}()
	}
}

func (l *Listener) trackConn(conn net.Conn) {
	l.connsMu.Lock()
	defer l.connsMu.Unlock()
	select {
	case <-l.done:
		_ = conn.Close()
	default:
		l.conns[conn] = struct{}{}
	}
}

func (l *Listener) untrackConn(conn net.Conn) {
	l.connsMu.Lock()
	defer l.connsMu.Unlock()
	delete(l.conns, conn)
}

func handleNotifyConn(conn net.Conn, events chan<- Mapping, done <-chan struct{}, token string) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		mapping, ok := decodeNotifyMapping(scanner.Bytes(), token)
		if !ok {
			continue
		}
		select {
		case events <- mapping:
		case <-done:
			return
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case <-done:
			return
		default:
			log.Printf("读取 natmap notify 事件失败: %v", err)
		}
	}
}

func decodeNotifyMapping(payload []byte, token string) (Mapping, bool) {
	if token == "" {
		var mapping Mapping
		if err := json.Unmarshal(payload, &mapping); err != nil {
			log.Printf("解析 natmap notify 事件失败: %v", err)
			return Mapping{}, false
		}
		return mapping, true
	}

	var envelope notifyEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		log.Printf("natmap notify 鉴权失败，已丢弃事件")
		return Mapping{}, false
	}
	if envelope.Token != token {
		log.Printf("natmap notify 鉴权失败，已丢弃事件")
		return Mapping{}, false
	}
	return envelope.Mapping, true
}
