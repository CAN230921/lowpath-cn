package capture

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

const (
	sysProtoControl = 2
	utunIfName      = 2
)

// HostRoute handles a dedicated test destination. It owns a single /32 and
// delivers only UDP to the selected port. Closing utun removes its routes.
type HostRoute struct{ target netip.AddrPort }

func NewHostRoute(target netip.AddrPort) *HostRoute { return &HostRoute{target: target} }
func (r *HostRoute) Name() string                   { return "utun-test" }
func (r *HostRoute) Recover(Marker) error           { return nil }
func (r *HostRoute) Available() error {
	if os.Geteuid() != 0 {
		return ErrNeedAdmin
	}
	return nil
}

func (r *HostRoute) Start(cfg Config) (Session, error) {
	if err := r.Available(); err != nil {
		return nil, err
	}
	if !r.target.Addr().Is4() || r.target.Port() == 0 || cfg.OnPacket == nil {
		return nil, errors.New("utun requires an IPv4 UDP endpoint and packet callback")
	}
	file, name, err := openUTUN()
	if err != nil {
		return nil, err
	}
	if err := routeCommand("/sbin/ifconfig", name, "inet", "169.254.76.80", "169.254.76.81", "netmask", "255.255.255.255", "mtu", "1100", "up"); err != nil {
		file.Close()
		return nil, err
	}
	if err := routeCommand("/sbin/route", "-n", "add", "-inet", "-host", r.target.Addr().String(), "-interface", name); err != nil {
		file.Close()
		return nil, err
	}
	s := &hostRouteSession{file: file, done: make(chan struct{})}
	if cfg.Log != nil {
		cfg.Log.Info("macOS test route installed", "interface", name, "target", r.target)
	}
	go s.read(r.target, cfg.OnPacket)
	return s, nil
}

func routeCommand(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, out)
	}
	return nil
}

func openUTUN() (_ *os.File, name string, err error) {
	fd, err := unix.Socket(unix.AF_SYSTEM, unix.SOCK_DGRAM, sysProtoControl)
	if err != nil {
		return nil, "", err
	}
	unix.CloseOnExec(fd)
	defer func() {
		if err != nil {
			unix.Close(fd)
		}
	}()
	var info unix.CtlInfo
	copy(info.Name[:], "com.apple.net.utun_control")
	if err := unix.IoctlCtlInfo(fd, &info); err != nil {
		return nil, "", err
	}
	if err := unix.Connect(fd, &unix.SockaddrCtl{ID: info.Id}); err != nil {
		return nil, "", err
	}
	name, err = unix.GetsockoptString(fd, sysProtoControl, utunIfName)
	if err != nil {
		return nil, "", err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, "", err
	}
	return os.NewFile(uintptr(fd), name), name, nil
}

type hostRouteSession struct {
	file                         *os.File
	done                         chan struct{}
	closing                      atomic.Bool
	failure                      atomic.Pointer[error]
	captured, injected, failures atomic.Uint64
}

func (s *hostRouteSession) read(target netip.AddrPort, send func(netip.AddrPort, netip.AddrPort, []byte) bool) {
	defer close(s.done)
	defer s.file.Close()
	buf := make([]byte, 4+65535)
	for {
		n, err := s.file.Read(buf)
		if err != nil {
			if !s.closing.Load() {
				s.failure.Store(&err)
			}
			return
		}
		if n < 4 || binary.BigEndian.Uint32(buf) != unix.AF_INET {
			continue
		}
		local, remote, data, ok := parseUDP4(buf[4:n])
		if ok && remote == target {
			s.captured.Add(1)
			if !send(local, remote, data) {
				s.failures.Add(1)
			}
		}
	}
}

func (s *hostRouteSession) Inject(local, remote netip.AddrPort, data []byte) error {
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], unix.AF_INET)
	packet, err := appendUDP4(head[:], remote, local, 0, data)
	if err == nil {
		_, err = s.file.Write(packet)
	}
	if err != nil {
		s.failures.Add(1)
		return err
	}
	s.injected.Add(1)
	return nil
}

func (s *hostRouteSession) Stats() Stats {
	return Stats{Captured: s.captured.Load(), Injected: s.injected.Load(), Errors: s.failures.Load()}
}

func (s *hostRouteSession) Close() error {
	if !s.closing.Swap(true) {
		_ = s.file.Close()
	}
	<-s.done
	return nil
}

func (s *hostRouteSession) Done() <-chan struct{} { return s.done }
func (s *hostRouteSession) Err() error {
	if err := s.failure.Load(); err != nil {
		return *err
	}
	return nil
}
