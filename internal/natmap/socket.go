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
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	allowedMu sync.Mutex
	allowed   map[int]string
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
		allowed:  make(map[int]string),
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

	token := os.Getenv(NotifyTokenEnv)
	if token == "" {
		return fmt.Errorf("natmap notify token 未配置")
	}
	if err := json.NewEncoder(conn).Encode(notifyEnvelope{Token: token, Mapping: mapping}); err != nil {
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
		if !listener.peerAllowed(conn) {
			log.Printf("natmap notify 鉴权失败，已丢弃事件")
			_ = conn.Close()
			continue
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

func (l *Listener) AllowPID(pid int) {
	if pid <= 0 {
		return
	}
	startTime, err := processStartTime(pid)
	if err != nil {
		return
	}
	l.allowedMu.Lock()
	defer l.allowedMu.Unlock()
	l.allowed[pid] = startTime
}

func (l *Listener) RevokePID(pid int) {
	if pid <= 0 {
		return
	}
	l.allowedMu.Lock()
	defer l.allowedMu.Unlock()
	delete(l.allowed, pid)
}

func (l *Listener) peerAllowed(conn net.Conn) bool {
	l.allowedMu.Lock()
	defer l.allowedMu.Unlock()
	if l.token == "" {
		return true
	}
	pid, err := peerPID(conn)
	if err != nil {
		return false
	}
	return l.pidAllowed(pid)
}

func (l *Listener) pidAllowed(pid int) bool {
	for pid > 0 {
		if startTime, ok := l.allowed[pid]; ok {
			currentStartTime, err := processStartTime(pid)
			return err == nil && currentStartTime == startTime
		}
		parent, err := parentPID(pid)
		if err != nil || parent == pid {
			return false
		}
		pid = parent
	}
	return false
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

func peerPID(conn net.Conn) (int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("natmap notify 连接不是 UnixConn")
	}
	file, err := unixConn.File()
	if err != nil {
		return 0, fmt.Errorf("获取 natmap notify 连接文件失败: %w", err)
	}
	defer file.Close()
	cred, err := syscall.GetsockoptUcred(int(file.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return 0, fmt.Errorf("读取 natmap notify peer credential 失败: %w", err)
	}
	return int(cred.Pid), nil
}

func parentPID(pid int) (int, error) {
	fields, err := processStatFields(pid)
	if err != nil {
		return 0, err
	}
	if len(fields) < 2 {
		return 0, fmt.Errorf("解析进程父 PID 失败")
	}
	return strconv.Atoi(fields[1])
}

func processStartTime(pid int) (string, error) {
	fields, err := processStatFields(pid)
	if err != nil {
		return "", err
	}
	if len(fields) < 20 {
		return "", fmt.Errorf("解析进程启动时间失败")
	}
	return fields[19], nil
}

func processStatFields(pid int) ([]string, error) {
	content, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return nil, err
	}
	end := strings.LastIndexByte(string(content), ')')
	if end < 0 || end+2 >= len(content) {
		return nil, fmt.Errorf("解析进程 stat 失败")
	}
	return strings.Fields(string(content[end+2:])), nil
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
