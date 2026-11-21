package managed

import (
	"syscall"

	"golang.org/x/sys/unix"
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
