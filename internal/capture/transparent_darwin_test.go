//go:build cgo

package capture

import (
	"bufio"
	"encoding/json"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestMacProxyBridge(t *testing.T) {
	host, provider := net.Pipe()
	defer host.Close()
	defer provider.Close()
	provider.SetDeadline(time.Now().Add(3 * time.Second))
	local := netip.MustParseAddrPort("192.168.1.2:50000")
	remote := netip.MustParseAddrPort("192.0.2.21:27015")
	excluded := netip.MustParseAddrPort("192.0.2.30:443")
	seen := make(chan []byte, 2)
	session := &proxySession{conn: host, done: make(chan struct{}), routes: map[proxyRoute]proxyFlow{}}
	go session.read(bufio.NewScanner(host), Config{
		Exclude: []netip.AddrPort{excluded},
		OnPacket: func(src, dst netip.AddrPort, payload []byte) bool {
			if src != local || dst != remote {
				t.Errorf("unexpected packet %s -> %s", src, dst)
			}
			seen <- payload
			return string(payload) != "pass"
		},
	})
	defer session.Close()

	send, receive := json.NewEncoder(provider), json.NewDecoder(provider)
	for _, packet := range []proxyMessage{
		{Kind: "packet", Flow: 6, PID: 1234, Local: local, Remote: excluded, Data: []byte("excluded")},
		{Kind: "packet", Flow: 7, PID: 1234, Local: local, Remote: remote, Data: []byte("tunnel")},
	} {
		if err := send.Encode(packet); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case payload := <-seen:
		if string(payload) != "tunnel" {
			t.Fatalf("expected the eligible packet, got %q", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not hand a packet to the engine")
	}

	injected := make(chan error, 1)
	go func() { injected <- session.Inject(local, remote, []byte("return")) }()
	var reply proxyMessage
	if err := receive.Decode(&reply); err != nil || reply.Kind != "reply" || reply.Flow != 7 || string(reply.Data) != "return" {
		t.Fatalf("bridge reply = %+v, %v", reply, err)
	}
	if err := <-injected; err != nil {
		t.Fatal(err)
	}

	if err := send.Encode(proxyMessage{Kind: "packet", Flow: 7, PID: 1234, Local: local, Remote: remote, Data: []byte("pass")}); err != nil {
		t.Fatal(err)
	}
	if err := receive.Decode(&reply); err != nil || reply.Kind != "direct" || string(reply.Data) != "pass" {
		t.Fatalf("bridge fallback = %+v, %v", reply, err)
	}
	if stats := session.Stats(); stats.Captured != 2 || stats.Passed != 1 || stats.Injected != 1 {
		t.Fatalf("unexpected bridge counters: %+v", stats)
	}
}
