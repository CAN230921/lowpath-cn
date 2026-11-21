package main

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func deviceSocket(ctx context.Context, name string) (net.PacketConn, error) {
	device, err := net.InterfaceByName(name)
	if err != nil {
		return nil, err
	}
	network := net.ListenConfig{
		Control: func(_, _ string, conn syscall.RawConn) error {
			var socketErr error
			err := conn.Control(func(fd uintptr) {
				socketErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, device.Index)
			})
			if err != nil {
				return err
			}
			return socketErr
		},
	}
	return network.ListenPacket(ctx, "udp4", "0.0.0.0:0")
}
