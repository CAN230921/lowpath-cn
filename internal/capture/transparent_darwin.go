//go:build cgo

package capture

/*
#cgo LDFLAGS: -framework Foundation -framework NetworkExtension -framework SystemExtensions
#include <stdlib.h>
int lowpath_proxy_embedded(void);
void lowpath_proxy_control(const char *json);
*/
import "C"

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const MacProxyControlArg = "-internal-mac-proxy"
const proxyStartTimeout = 45 * time.Second

var ErrMacProxyUnavailable = errors.New("当前构建未启用 macOS 网络扩展，游戏路由模式需要带 Apple 授权的构建")

type TransparentProxy struct{}

func (TransparentProxy) Name() string { return "network-extension" }

func (TransparentProxy) Available() error {
	if C.lowpath_proxy_embedded() == 0 {
		return ErrMacProxyUnavailable
	}
	return nil
}

// The bridge is the provider's lease. Once the parent exits, the connection
// closes and the provider stops accepting flows and disconnects.
func (TransparentProxy) Recover(Marker) error { return nil }

type proxyConfig struct {
	Port      int      `json:"port"`
	Secret    string   `json:"secret"`
	Processes []string `json:"processes"`
	PIDs      []uint32 `json:"pids"`
	Exclude   []string `json:"exclude"`
	Parent    int      `json:"parent"`
	UID       int      `json:"uid"`
}

type proxyMessage struct {
	Kind   string         `json:"kind"`
	Token  string         `json:"token,omitempty"`
	Flow   uint64         `json:"flow,omitempty"`
	PID    uint32         `json:"pid,omitempty"`
	Local  netip.AddrPort `json:"local,omitempty"`
	Remote netip.AddrPort `json:"remote,omitempty"`
	Data   []byte         `json:"data,omitempty"`
}

func RunMacProxyController() error {
	input := bufio.NewReaderSize(os.Stdin, 64<<10)
	text, err := input.ReadSlice('\n')
	if err != nil || !json.Valid(text) {
		return errors.New("invalid macOS proxy configuration")
	}
	// The native controller owns the main run loop. A parent crash also
	// terminates it, while the provider independently watches its bridge.
	go func() {
		input.ReadByte()
		os.Exit(0)
	}()
	config := C.CString(string(text))
	defer C.free(unsafe.Pointer(config))
	C.lowpath_proxy_control(config)
	return errors.New("macOS network extension stopped")
}

func (p TransparentProxy) Start(cfg Config) (Session, error) {
	if err := p.Available(); err != nil {
		return nil, err
	}
	if cfg.OnPacket == nil || len(cfg.Processes)+len(cfg.PIDs) == 0 {
		return nil, errors.New("macOS proxy needs selected game processes")
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	control := proxyConfig{
		Port: listener.Addr().(*net.TCPAddr).Port, Secret: hex.EncodeToString(secret),
		Processes: slices.Clone(cfg.Processes), PIDs: slices.Clone(cfg.PIDs),
		Exclude: []string{}, Parent: os.Getpid(), UID: os.Geteuid(),
	}
	for _, a := range cfg.Exclude {
		control.Exclude = append(control.Exclude, a.String())
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, MacProxyControlArg)
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	started := false
	defer func() {
		if !started {
			input.Close()
		}
	}()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	waited := make(chan error, 1)
	go func() {
		waited <- cmd.Wait()
		listener.Close()
	}()
	if err := json.NewEncoder(input).Encode(control); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(proxyStartTimeout)
	listener.SetDeadline(deadline)
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-waited:
				return nil, errors.New("macOS 拒绝启动网络扩展，请检查签名与系统设置中的授权")
			default:
				return nil, fmt.Errorf("等待 macOS 网络扩展授权: %w", err)
			}
		}
		conn.SetReadDeadline(time.Now().Add(time.Second))
		scan := bufio.NewScanner(conn)
		var hello proxyMessage
		if scan.Scan() && json.Unmarshal(scan.Bytes(), &hello) == nil && hello.Kind == "hello" && subtle.ConstantTimeCompare([]byte(hello.Token), []byte(proxyProof("provider", control.Secret))) == 1 {
			// Wait for the provider's rule installation to succeed before
			// presenting the capture as active.
			conn.SetDeadline(deadline)
			if err := json.NewEncoder(conn).Encode(proxyMessage{Kind: "accept", Token: proxyProof("client", control.Secret)}); err != nil {
				conn.Close()
				return nil, err
			}
			var ready proxyMessage
			if !scan.Scan() || json.Unmarshal(scan.Bytes(), &ready) != nil || ready.Kind != "ready" {
				conn.Close()
				return nil, errors.New("macOS 网络扩展未能注册 UDP 规则")
			}
			conn.SetDeadline(time.Time{})
			s := &proxySession{conn: conn, done: make(chan struct{}), routes: map[proxyRoute]proxyFlow{}}
			go s.read(scan, cfg)
			go s.watch(cfg.StallLimit)
			// Keep the controller's stdin open for the duration of the
			// session. Transfer ownership so a normal Start can return.
			go func() {
				<-s.done
				input.Close()
			}()
			started = true
			return s, nil
		}
		conn.Close()
	}
}

func proxyProof(role, secret string) string {
	sum := sha256.Sum256([]byte(role + ":" + secret))
	return hex.EncodeToString(sum[:])
}

type proxyRoute struct{ local, remote netip.AddrPort }

type proxyFlow struct {
	id  uint64
	pid uint32
}

type proxySession struct {
	conn      net.Conn
	done      chan struct{}
	closeOnce sync.Once
	worker    workerState
	failure   atomic.Pointer[error]
	captured  atomic.Uint64
	passed    atomic.Uint64
	injected  atomic.Uint64
	errors    atomic.Uint64
	mu        sync.Mutex
	routes    map[proxyRoute]proxyFlow
	writeLock sync.Mutex
}

func (s *proxySession) read(scan *bufio.Scanner, cfg Config) {
	defer s.stop(errors.New("macOS 网络扩展连接已结束"))
	for scan.Scan() {
		var packet proxyMessage
		if json.Unmarshal(scan.Bytes(), &packet) != nil {
			continue
		}
		if packet.Kind == "closed" {
			s.mu.Lock()
			for key, flow := range s.routes {
				if flow.id == packet.Flow {
					delete(s.routes, key)
				}
			}
			s.mu.Unlock()
			continue
		}
		if packet.Kind != "packet" || packet.Flow == 0 || !packet.Local.Addr().Is4() ||
			packet.Local.Port() == 0 || packet.Remote.Port() == 0 || packet.Remote.Port() == 53 ||
			!reachable(packet.Remote.Addr()) || packet.Remote.Addr().IsUnspecified() ||
			slices.ContainsFunc(cfg.Exclude, func(e netip.AddrPort) bool {
				return e.Addr().Unmap() == packet.Remote.Addr() && (e.Port() == 0 || e.Port() == packet.Remote.Port())
			}) {
			continue
		}
		key := proxyRoute{packet.Local, packet.Remote}
		s.mu.Lock()
		if len(s.routes) >= 4096 && s.routes[key].id == 0 {
			s.mu.Unlock()
			continue
		}
		s.routes[key] = proxyFlow{packet.Flow, packet.PID}
		s.mu.Unlock()
		s.captured.Add(1)
		s.worker.enter()
		handled := cfg.OnPacket(packet.Local, packet.Remote, packet.Data)
		if !handled {
			s.passed.Add(1)
			packet.Kind = "direct"
			if err := s.write(packet); err != nil {
				s.worker.leave()
				break
			}
		}
		s.worker.leave()
	}
}

func (s *proxySession) watch(limit time.Duration) {
	watch := newStallWatch(limit, []*workerState{&s.worker}, time.Now())
	ticker := time.NewTicker(watch.every)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			if elapsed, stuck := watch.check(now); stuck {
				s.stop(fmt.Errorf("macOS UDP 处理停滞 %s，已停止接管", elapsed.Round(time.Millisecond)))
				return
			}
		case <-s.done:
			return
		}
	}
}

func (s *proxySession) write(message proxyMessage) error {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	s.conn.SetWriteDeadline(time.Now().Add(time.Second))
	if err := json.NewEncoder(s.conn).Encode(message); err != nil {
		s.errors.Add(1)
		s.stop(err)
		return err
	}
	return nil
}

func (s *proxySession) Inject(local, remote netip.AddrPort, payload []byte) error {
	s.mu.Lock()
	flow := s.routes[proxyRoute{local, remote}]
	s.mu.Unlock()
	if flow.id == 0 {
		return ErrClosed
	}
	if err := s.write(proxyMessage{Kind: "reply", Flow: flow.id, Remote: remote, Data: payload}); err != nil {
		return err
	}
	s.injected.Add(1)
	return nil
}

func (s *proxySession) Stats() Stats {
	s.mu.Lock()
	var pids []uint32
	for _, flow := range s.routes {
		if flow.pid > 0 && !slices.Contains(pids, flow.pid) {
			pids = append(pids, flow.pid)
		}
	}
	s.mu.Unlock()
	slices.Sort(pids)
	stats := Stats{Captured: s.captured.Load(), Passed: s.passed.Load(), Injected: s.injected.Load(), Errors: s.errors.Load(), GamePIDs: pids}
	if err := s.Err(); err != nil {
		stats.LastError = err.Error()
	}
	return stats
}

func (s *proxySession) Done() <-chan struct{} { return s.done }

func (s *proxySession) Close() error {
	s.stop(nil)
	return nil
}

func (s *proxySession) stop(reason error) {
	s.closeOnce.Do(func() {
		if reason != nil {
			s.failure.Store(&reason)
		}
		s.conn.Close()
		close(s.done)
	})
}

func (s *proxySession) Err() error {
	if err := s.failure.Load(); err != nil {
		return *err
	}
	return nil
}
