package detect

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestReadNettop(t *testing.T) {
	sample := ",bytes_out,bytes_in,\n" +
		"Some.Game.184,80,40,\n" +
		"udp4 10.0.0.2:2222<->8.8.4.4:27015,128,256,\n" +
		"udp4 *:2223<->*:*,10,20,\n" +
		"tcp6 2001:db8::1.8844<->2001:db8::2.9000,45,0,\n" +
		",bytes_in,bytes_out,\n" +
		"tcp4 10.0.0.2:2222<->8.8.4.4:27015,100,100,\n"
	var observed []FlowEvent
	readNettop(strings.NewReader(sample), func(e FlowEvent) { observed = append(observed, e) })
	if len(observed) != 3 {
		t.Fatalf("want three byte deltas, got %+v", observed)
	}
	want := []string{"8.8.4.4:27015", "8.8.4.4:27015", "[2001:db8::2]:9000"}
	for i, event := range observed {
		if event.PID != 184 || event.Remote.String() != want[i] || event.Outbound != (i != 1) {
			t.Errorf("event %d = %+v", i, event)
		}
	}
	if observed[0].Bytes != 128 || observed[1].Bytes != 256 || observed[2].Bytes != 45 {
		t.Errorf("byte deltas = %+v", observed)
	}
}

func TestNettopObservesConnectedUDP(t *testing.T) {
	proxy, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	dest := proxy.LocalAddr().(*net.UDPAddr)
	client, err := net.DialUDP("udp4", nil, dest)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	hit := make(chan struct{}, 1)
	watch, err := NewKernelNetwork().Watch(func(e FlowEvent) {
		if e.PID == uint32(os.Getpid()) && e.Proto == "udp" && e.Outbound && e.Remote == dest.AddrPort() {
			select {
			case hit <- struct{}{}:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(6 * time.Second)
	for {
		select {
		case <-tick.C:
			if _, err := client.Write([]byte("lowpath UDP observation")); err != nil {
				t.Fatal(err)
			}
		case <-hit:
			return
		case <-watch.Done():
			t.Fatal("nettop exited before it reported the socket")
		case <-deadline:
			t.Fatal("nettop did not report the connected UDP socket")
		}
	}
}
