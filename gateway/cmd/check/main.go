package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"
	"time"

	masque "github.com/quic-go/masque-go"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"
)

const (
	relayTarget = "127.0.0.1:42180"
	stunCookie  = 0x2112a442
	headerBytes = 33
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	host := flag.String("host", "", "HTTPS hostname")
	ip := flag.String("ip", "", "optional origin IPv4 while keeping TLS hostname validation")
	device := flag.String("interface", "", "optional macOS network interface for path diagnostics")
	target := flag.String("stun", "", "IPv4 STUN server and port")
	exit := flag.String("exit", "", "expected exit IP")
	node := flag.String("node", "", "regional node ID, such as tokyo or hk-local")
	count := flag.Int("count", 24, "number of UDP requests")
	webOnly := flag.Bool("web-only", false, "only verify the unauthenticated HTTP/3 page")
	flag.Parse()
	if *host == "" || *count < 1 || *count > 100 {
		return errors.New("host is required and count must be between 1 and 100")
	}
	destination := net.JoinHostPort(*host, "443")
	if *ip != "" {
		destination = net.JoinHostPort(*ip, "443")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tlsConf := &tls.Config{ServerName: *host, NextProtos: []string{http3.NextProtoH3}, MinVersion: tls.VersionTLS13}
	conf := &quic.Config{EnableDatagrams: true, HandshakeIdleTimeout: 5 * time.Second}
	var conn *quic.Conn
	var err error
	if *device == "" {
		conn, err = quic.DialAddr(ctx, destination, tlsConf, conf)
	} else {
		if *ip == "" {
			return errors.New("specify -ip to test a particular network interface")
		}
		peer, resolveErr := net.ResolveUDPAddr("udp4", destination)
		if resolveErr != nil {
			return resolveErr
		}
		sock, bindErr := deviceSocket(ctx, *device)
		if bindErr != nil {
			return bindErr
		}
		defer sock.Close()
		transport := &quic.Transport{Conn: sock}
		defer transport.Close()
		conn, err = transport.Dial(ctx, peer, tlsConf, conf)
	}
	if err != nil {
		return fmt.Errorf("HTTP/3 handshake: %w", err)
	}
	defer conn.CloseWithError(0, "")
	tr := &http3.Transport{EnableDatagrams: true}
	defer tr.Close()
	h3 := tr.NewClientConn(conn)
	if err := checkGame(ctx, h3, *host); err != nil {
		return err
	}
	fmt.Println("game: verified public certificate, HTTP/3 homepage, script, daily challenge and trail events")
	if *webOnly {
		return nil
	}
	dst, err := netip.ParseAddrPort(*target)
	if err != nil || !dst.Addr().Is4() || *exit == "" {
		return errors.New("an IPv4 STUN server and expected exit are required")
	}
	scan := bufio.NewScanner(os.Stdin)
	if !scan.Scan() {
		return errors.New("missing MASQUE credential on stdin")
	}
	auth := "Bearer " + strings.TrimSpace(scan.Text())
	if *node != "" {
		return checkNode(ctx, h3, *host, *node, auth, dst, *count, *exit)
	}
	if !scan.Scan() {
		return errors.New("missing Lowpath relay key on stdin")
	}
	key, ok := strings.CutPrefix(strings.TrimSpace(scan.Text()), "LOWPATH_RELAY_KEY=")
	if !ok || key == "" {
		return errors.New("invalid Lowpath relay key")
	}
	sum := sha256.Sum256([]byte("lowpath-relay-v1:" + key))
	token := binary.BigEndian.Uint64(sum[:8])
	if token == 0 {
		token = 1
	}

	tmpl := uritemplate.MustNew("https://" + *host + "/udp/{target_host}/{target_port}/")
	client := masque.NewClientConn(h3)
	for _, test := range []struct {
		address, auth string
		status        int
	}{
		{relayTarget, "", http.StatusUnauthorized},
		{"127.0.0.1:9", auth, http.StatusForbidden},
	} {
		tunnel, resp, err := dial(ctx, client, tmpl, test.address, test.auth)
		if tunnel != nil {
			tunnel.Close()
		}
		if err == nil || resp == nil || resp.StatusCode != test.status {
			return fmt.Errorf("denial test failed for expected HTTP %d", test.status)
		}
	}
	fmt.Println("relay: rejected an unauthenticated request and an unapproved destination")
	tunnel, _, err := dial(ctx, client, tmpl, relayTarget, auth)
	if err != nil {
		return err
	}
	defer tunnel.Close()
	return probe(tunnel, dst, token, *count, *exit)
}

func checkGame(ctx context.Context, h3 *http3.ClientConn, host string) error {
	pages := []struct{ path, marker string }{
		{"/", `<canvas class="river" id="river">`},
		{"/game.js", `function setupLandscape()`},
		{"/api/game/daily", `"seed":`},
		{"/api/game/event?from=3&to=4", `"event":"bell"`},
	}
	for _, page := range pages {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+page.path, nil)
		if err != nil {
			return err
		}
		resp, err := h3.RoundTrip(req)
		if err != nil {
			return fmt.Errorf("HTTP/3 GET %s: %w", page.path, err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || resp.ProtoMajor != 3 || !bytes.Contains(body, []byte(page.marker)) {
			return fmt.Errorf("unexpected game response for %s: %s %d", page.path, resp.Proto, resp.StatusCode)
		}
	}
	return nil
}

func checkNode(ctx context.Context, h3 *http3.ClientConn, host, node, auth string, dst netip.AddrPort, count int, exit string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/api/v1/nodes", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	resp, err := h3.RoundTrip(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node directory status %d", resp.StatusCode)
	}
	var directory struct {
		Template string `json:"connect_udp_template"`
		Nodes    []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Target string `json:"target"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&directory); err != nil {
		return err
	}
	if directory.Template != "https://"+host+"/api/v1/udp/{target_host}/{target_port}/" {
		return errors.New("unexpected CONNECT-UDP template")
	}
	tmpl := uritemplate.MustNew(directory.Template)
	client := masque.NewClientConn(h3)
	for _, entry := range directory.Nodes {
		fmt.Printf("node %s: %s\n", entry.ID, entry.Status)
	}
	for _, entry := range directory.Nodes {
		if entry.ID != node {
			continue
		}
		if entry.Status != "ready" {
			return fmt.Errorf("node %s is %s", node, entry.Status)
		}
		for _, test := range []struct {
			target, auth string
			code         int
		}{
			{entry.Target, "", http.StatusUnauthorized},
			{"127.0.0.1:9", auth, http.StatusForbidden},
		} {
			conn, response, err := dial(ctx, client, tmpl, test.target, test.auth)
			if conn != nil {
				conn.Close()
			}
			if err == nil || response == nil || response.StatusCode != test.code {
				return fmt.Errorf("expected rejection %d", test.code)
			}
		}
		tunnel, _, err := dial(ctx, client, tmpl, entry.Target, auth)
		if err != nil {
			return err
		}
		defer tunnel.Close()
		return probe(tunnel, dst, 0, count, exit)
	}
	return fmt.Errorf("device is not authorized for node %s", node)
}

func dial(ctx context.Context, client *masque.ClientConn, tmpl *uritemplate.Template, target, auth string) (*masque.Conn, *http.Response, error) {
	req, err := masque.NewRequest(ctx, tmpl, target)
	if err != nil {
		return nil, nil, err
	}
	if auth != "" {
		req.Header().Set("Authorization", auth)
	}
	return client.Dial(req)
}

func probe(tunnel *masque.Conn, dst netip.AddrPort, token uint64, count int, expected string) error {
	var flow [8]byte
	if _, err := rand.Read(flow[:]); err != nil {
		return err
	}
	start := time.Now()
	samples := make([]time.Duration, 0, count)
	var mismatched int
	for seq := 1; seq <= count; seq++ {
		request := makeRequest(dst, token, flow, uint32(seq), uint32(time.Since(start).Milliseconds()))
		sent := time.Now()
		if _, err := tunnel.WriteTo(request, nil); err != nil {
			return err
		}
		tunnel.SetReadDeadline(sent.Add(time.Second))
		for {
			var buf [2048]byte
			n, _, err := tunnel.ReadFrom(buf[:])
			if err != nil {
				if e, isNet := err.(net.Error); isNet && e.Timeout() {
					break
				}
				return err
			}
			msg := buf[:n]
			if len(msg) < headerBytes+20 || !bytes.Equal(msg[:18], request[:18]) ||
				!bytes.Equal(msg[26:headerBytes], request[26:headerBytes]) ||
				!bytes.Equal(msg[headerBytes+4:headerBytes+20], request[headerBytes+4:headerBytes+20]) {
				continue
			}
			mapped, ok := stunMapped(msg[headerBytes:])
			if !ok {
				continue
			}
			if mapped.String() != expected {
				mismatched++
				break
			}
			samples = append(samples, time.Since(sent))
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("valid exit replies: %d/%d; unexpected exit: %d\n", len(samples), count, mismatched)
	if len(samples) > 0 {
		slices.Sort(samples)
		var sum time.Duration
		for _, t := range samples {
			sum += t
		}
		fmt.Printf("RTT mean %s, p95 %s\n", (sum / time.Duration(len(samples))).Round(time.Microsecond),
			samples[(len(samples)*95+99)/100-1].Round(time.Microsecond))
	}
	if len(samples) != count {
		return errors.New("one or more UDP requests did not return from the expected exit")
	}
	return nil
}

func makeRequest(dst netip.AddrPort, token uint64, flow [8]byte, seq, elapsed uint32) []byte {
	packet := make([]byte, headerBytes+20)
	packet[0], packet[1] = 0xa7, 1
	binary.BigEndian.PutUint64(packet[2:10], token)
	copy(packet[10:18], flow[:])
	binary.BigEndian.PutUint32(packet[18:22], seq)
	binary.BigEndian.PutUint32(packet[22:26], elapsed)
	addr := dst.Addr().As4()
	packet[26] = 4
	copy(packet[27:31], addr[:])
	binary.BigEndian.PutUint16(packet[31:33], dst.Port())
	binary.BigEndian.PutUint16(packet[33:35], 1)
	binary.BigEndian.PutUint32(packet[37:41], stunCookie)
	rand.Read(packet[41:53])
	return packet
}

func stunMapped(msg []byte) (netip.Addr, bool) {
	if binary.BigEndian.Uint16(msg[:2]) != 0x0101 {
		return netip.Addr{}, false
	}
	end := 20 + int(binary.BigEndian.Uint16(msg[2:4]))
	if end > len(msg) {
		return netip.Addr{}, false
	}
	for off := 20; off+4 <= end; {
		kind := binary.BigEndian.Uint16(msg[off:])
		size := int(binary.BigEndian.Uint16(msg[off+2:]))
		if off+4+size > end {
			break
		}
		value := msg[off+4 : off+4+size]
		if (kind == 1 || kind == 0x20) && len(value) == 8 && value[1] == 1 {
			address := binary.BigEndian.Uint32(value[4:])
			if kind == 0x20 {
				address ^= stunCookie
			}
			return netip.AddrFrom4([4]byte{byte(address >> 24), byte(address >> 16), byte(address >> 8), byte(address)}), true
		}
		off += 4 + (size+3)&^3
	}
	return netip.Addr{}, false
}
