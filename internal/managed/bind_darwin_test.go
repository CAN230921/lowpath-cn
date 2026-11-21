package managed

import (
	"context"
	"net"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPhysicalIPv4AdapterExcludesTunnelAndInactiveDevices(t *testing.T) {
	ready := net.FlagUp | net.FlagRunning
	for _, tc := range []struct {
		name  string
		flags net.Flags
		want  bool
	}{
		{"en0", ready, true},
		{"en9", ready, true},
		{"en9", net.FlagUp, false},
		{"en0", ready | net.FlagPointToPoint, false},
		{"utun6", ready | net.FlagPointToPoint, false},
		{"lo0", ready | net.FlagLoopback, false},
		{"awdl0", ready, false},
		{"bridge0", ready, false},
	} {
		if got := physicalIPv4Adapter(net.Interface{Name: tc.name, Flags: tc.flags}); got != tc.want {
			t.Errorf("%s (%v): got %v, want %v", tc.name, tc.flags, got, tc.want)
		}
	}
}

func TestMacGatewaySocketControlBindsTCPAndUDP(t *testing.T) {
	adapter, err := net.InterfaceByName("lo0")
	if err != nil {
		t.Fatal(err)
	}
	check := func(conn syscall.Conn) {
		t.Helper()
		raw, err := conn.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		var index int
		var readErr error
		err = raw.Control(func(fd uintptr) {
			index, readErr = unix.GetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF)
		})
		if err != nil || readErr != nil || index != adapter.Index {
			t.Fatalf("bound interface = %d, want %d: %v %v", index, adapter.Index, err, readErr)
		}
	}
	ctx := context.Background()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	dialer := net.Dialer{Control: socketControl(adapter.Index)}
	tcp, err := dialer.DialContext(ctx, "tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	check(tcp.(*net.TCPConn))
	config := net.ListenConfig{Control: socketControl(adapter.Index)}
	udp, err := config.ListenPacket(ctx, "udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	check(udp.(*net.UDPConn))
}
