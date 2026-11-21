package managed

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"lowpath/internal/diag"
)

func bindInterface(conn syscall.RawConn, index int) error {
	var sockErr error
	if err := conn.Control(func(fd uintptr) {
		sockErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, index)
	}); err != nil {
		return err
	}
	return sockErr
}

func physicalUplinks() ([]net.Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var uplinks []net.Interface
	for _, adapter := range interfaces {
		if !physicalIPv4Adapter(adapter) {
			continue
		}
		addresses, err := adapter.Addrs()
		if err != nil {
			continue
		}
		for _, entry := range addresses {
			prefix, err := netip.ParsePrefix(entry.String())
			if address := prefix.Addr(); err == nil && address.Is4() && address.IsGlobalUnicast() {
				uplinks = append(uplinks, adapter)
				break
			}
		}
	}
	if len(uplinks) == 0 {
		return nil, errors.New("no active macOS physical interface has an IPv4 address")
	}
	if len(uplinks) > 1 {
		if primary, err := diag.NewNetStats().DefaultRoute(); err == nil {
			for i := range uplinks {
				if uplinks[i].Index == primary.Index {
					uplinks[0], uplinks[i] = uplinks[i], uplinks[0]
					break
				}
			}
		}
	}
	return uplinks, nil
}

func physicalIPv4Adapter(adapter net.Interface) bool {
	number, ok := strings.CutPrefix(adapter.Name, "en")
	if !ok || number == "" || adapter.Flags&(net.FlagUp|net.FlagRunning) != net.FlagUp|net.FlagRunning ||
		adapter.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
		return false
	}
	for _, digit := range number {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
