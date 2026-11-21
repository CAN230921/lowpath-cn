package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	masque "github.com/quic-go/masque-go"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"

	"lowpath-masque-edge/internal/access"
)

func TestRegionalGateway(t *testing.T) {
	dir := t.TempDir()
	nodes, err := newRoutes([]nodeSpec{
		{ID: "hk-local", Name: "Hong Kong", Upstream: echoRelay(t, 0x11), RelayKey: "test-hk"},
		{ID: "tokyo", Name: "Tokyo", Upstream: echoRelay(t, 0x22), RelayKey: "test-tokyo"},
		{ID: "london", Name: "London"},
		{ID: "oregon", Name: "Oregon"},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes.nodes[:2] {
		if !n.ping() {
			t.Fatalf("%s did not answer a Lowpath probe", n.id)
		}
		n.healthy.Store(true)
	}

	socket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	host := socket.LocalAddr().String()
	legacy := &masque.Proxy{}
	defer legacy.Close()
	handler := newHandler(host, sha256.Sum256([]byte("Bearer legacy")), legacy, nodes)
	cert := httptest.NewTLSServer(handler)
	defer cert.Close()
	server := &http3.Server{
		Handler: handler, TLSConfig: &tls.Config{Certificates: cert.TLS.Certificates},
		EnableDatagrams: true,
	}
	defer server.Close()
	go server.Serve(socket)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tlsConf := cert.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	tlsConf.ServerName = "127.0.0.1"
	tlsConf.NextProtos = []string{http3.NextProtoH3}
	conn, err := quic.DialAddr(ctx, host, tlsConf, &quic.Config{EnableDatagrams: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "")
	transport := &http3.Transport{EnableDatagrams: true}
	h3 := transport.NewClientConn(conn)
	defer transport.Close()
	origin := "https://" + host

	valid := writeDevice(t, dir, "1", time.Now().Add(time.Minute), "hk-local", "tokyo", "london")
	expired := writeDevice(t, dir, "2", time.Now().Add(-time.Minute), "hk-local")
	directory := func(auth string, expected int) string {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/api/v1/nodes", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", auth)
		resp, err := h3.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != expected || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("directory status = %d, expected %d", resp.StatusCode, expected)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	directory("", 401)
	directory(expired, 401)
	body := directory(valid, 200)
	for _, want := range []string{"hk-local.relay.invalid", "tokyo.relay.invalid", "unconfigured"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in directory", want)
		}
	}
	for _, avoid := range []string{"oregon", "test-hk", nodes.nodes[0].upstream.String()} {
		if strings.Contains(body, avoid) {
			t.Fatalf("unlisted or private value %s in directory", avoid)
		}
	}

	client := masque.NewClientConn(h3)
	tmpl := uritemplate.MustNew(origin + routeTemplate)
	denied := func(target, auth string, status int) {
		t.Helper()
		req, err := masque.NewRequest(ctx, tmpl, target)
		if err != nil {
			t.Fatal(err)
		}
		req.Header().Set("Authorization", auth)
		conn, resp, err := client.Dial(req)
		if conn != nil {
			conn.Close()
		}
		if err == nil || resp == nil || resp.StatusCode != status {
			t.Fatalf("expected %d, got %v", status, err)
		}
	}
	denied("hk-local.relay.invalid:443", "", 401)
	denied("hk-local.relay.invalid:443", "Bearer legacy", 401)
	denied("oregon.relay.invalid:443", valid, 403)
	denied("127.0.0.1:9", valid, 403)
	denied("london.relay.invalid:443", valid, 503)

	open := func(id string) *masque.Conn {
		t.Helper()
		req, err := masque.NewRequest(ctx, tmpl, id+".relay.invalid:443")
		if err != nil {
			t.Fatal(err)
		}
		req.Header().Set("Authorization", valid)
		tunnel, _, err := client.Dial(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { tunnel.Close() })
		return tunnel
	}
	hk, tokyo := open("hk-local"), open("tokyo")
	var packet [lowpathHeader + 1]byte
	packet[0], packet[1], packet[10] = lowpathMagic, lowpathData, 0x44
	for _, pair := range []struct {
		tunnel *masque.Conn
		marker byte
	}{{hk, 0x11}, {tokyo, 0x22}} {
		if _, err := pair.tunnel.WriteTo(packet[:], nil); err != nil {
			t.Fatal(err)
		}
		reply := make([]byte, 256)
		pair.tunnel.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := pair.tunnel.ReadFrom(reply)
		if err != nil || n != len(packet) || !bytes.Equal(reply[:n-1], packet[:n-1]) || reply[n-1] != pair.marker {
			t.Fatalf("wrong relay round trip: %x %v", reply[:n], err)
		}
	}
	open("tokyo")
	open("hk-local")
	denied("hk-local.relay.invalid:443", valid, 429)
	if err := os.Remove(access.Path(dir, strings.Repeat("1", 24))); err != nil {
		t.Fatal(err)
	}
	directory(valid, 401)
	denied("hk-local.relay.invalid:443", valid, 401)
	hk.SetReadDeadline(time.Now().Add(4 * time.Second))
	if _, _, err := hk.ReadFrom(make([]byte, 256)); err == nil {
		t.Fatal("revoked tunnel did not close")
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("revoked tunnel stayed open until client deadline")
	}
}

func writeDevice(t *testing.T, dir, id string, until time.Time, nodes ...string) string {
	t.Helper()
	id = strings.Repeat(id, 24)
	secret := strings.Repeat("a", 64)
	digest := sha256.Sum256([]byte(secret))
	raw, err := json.Marshal(access.Device{Digest: hex.EncodeToString(digest[:]), Nodes: nodes, ExpiresAt: until})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(access.Path(dir, id), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return "Bearer " + id + "." + secret
}

func echoRelay(t *testing.T, marker byte) string {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		var buf [1024]byte
		for {
			n, addr, err := conn.ReadFromUDPAddrPort(buf[:])
			if err != nil {
				return
			}
			if n < lowpathHeader || binary.BigEndian.Uint64(buf[2:10]) == 0 {
				continue
			}
			if buf[1] == lowpathProbe {
				buf[1] = lowpathProbeReply
			} else {
				buf[n-1] = marker
			}
			conn.WriteToUDPAddrPort(buf[:n], addr)
		}
	}()
	return conn.LocalAddr().String()
}
