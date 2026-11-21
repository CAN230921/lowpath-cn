package managed

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	masque "github.com/quic-go/masque-go"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"
	"lowpath/internal/app"
	"lowpath/internal/engine"
)

const dialTimeout = 6 * time.Second

type Node struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Target string `json:"target"`
}

type Directory struct {
	Template string    `json:"connect_udp_template"`
	Expires  time.Time `json:"expires_at"`
	Nodes    []Node    `json:"nodes"`
}

type Network struct {
	origin  *url.URL
	peer    string
	control func(string, string, syscall.RawConn) error
	client  *http.Client
	gameID  string
	target  netip.AddrPort

	mu        sync.RWMutex
	key       string
	name      string
	directory Directory
}

func New(origin, ip, uplink, gameID string, target netip.AddrPort) (*Network, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("gateway must be an HTTPS origin")
	}
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = "443"
	}
	if ip != "" {
		addr, err := netip.ParseAddr(ip)
		if err != nil || !addr.Is4() {
			return nil, errors.New("gateway-ip must be IPv4")
		}
		host = ip
	}
	var ctrl func(string, string, syscall.RawConn) error
	if uplink != "" {
		iface, err := net.InterfaceByName(uplink)
		if err != nil {
			return nil, err
		}
		ctrl = func(_, _ string, socket syscall.RawConn) error { return bindInterface(socket, iface.Index) }
	}
	n := &Network{origin: u, peer: net.JoinHostPort(host, port), control: ctrl, gameID: gameID, target: target}
	dialer := &net.Dialer{Control: ctrl, Timeout: dialTimeout}
	n.client = &http.Client{
		Timeout:       dialTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13},
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "tcp4", n.peer)
			},
		},
	}
	return n, nil
}

func (n *Network) Demo() bool { return false }

func (n *Network) Endpoint(gameID string) (netip.AddrPort, netip.AddrPort, bool) {
	return n.target, n.target, gameID == n.gameID && n.target.IsValid()
}

func (n *Network) Request(ctx context.Context, method, path, key string, body io.Reader, result any) (int, error) {
	u := *n.origin
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return 0, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := n.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 || result == nil {
		return res.StatusCode, nil
	}
	return res.StatusCode, json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(result)
}

func (n *Network) Login(ctx context.Context, key, name string) error {
	var directory Directory
	code, err := n.Request(ctx, http.MethodGet, "/api/v1/nodes", key, nil, &directory)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("gateway rejected device credential (HTTP %d)", code)
	}
	expected := strings.TrimSuffix(n.origin.String(), "/") + "/api/v1/udp/{target_host}/{target_port}/"
	if directory.Template != expected || !time.Now().Before(directory.Expires) {
		return errors.New("invalid gateway directory")
	}
	n.mu.Lock()
	n.key, n.name, n.directory = key, name, directory
	n.mu.Unlock()
	return nil
}

func (n *Network) Account() (name string, expiry time.Time) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.key != "" && time.Now().Before(n.directory.Expires) {
		return n.name, n.directory.Expires
	}
	return "", time.Time{}
}

func (n *Network) Logout() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	key := n.key
	n.key, n.name, n.directory = "", "", Directory{}
	return key
}

func (n *Network) Paths(string) []engine.PathSpec {
	n.mu.RLock()
	defer n.mu.RUnlock()
	var paths []engine.PathSpec
	if !time.Now().Before(n.directory.Expires) {
		return paths
	}
	for _, node := range n.directory.Nodes {
		if node.Status != "ready" || node.Target == "" {
			continue
		}
		key, template := n.key, n.directory.Template
		paths = append(paths, engine.PathSpec{ID: node.ID, Label: node.Name, Region: node.Name, Continent: continent(node.ID), Egress: node.ID,
			Dial: func(ctx context.Context) (net.Conn, error) { return n.open(ctx, key, template, node.Target) },
		})
	}
	return paths
}

func (n *Network) ManagedRelays() []app.RelayView {
	paths := n.Paths("")
	views := make([]app.RelayView, 0, len(paths))
	for _, path := range paths {
		views = append(views, app.RelayView{ID: path.ID, Label: path.Label, Region: path.Region, Continent: path.Continent,
			Addr: n.origin.Host, HasKey: true, Builtin: true})
	}
	return views
}

func continent(node string) string {
	switch node {
	case "london":
		return "Europe"
	case "oregon":
		return "North America"
	default:
		return "Asia"
	}
}

func (n *Network) open(ctx context.Context, key, template, target string) (_ net.Conn, err error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	peer, err := net.ResolveUDPAddr("udp4", n.peer)
	if err != nil {
		return nil, err
	}
	config := net.ListenConfig{Control: n.control}
	socket, err := config.ListenPacket(ctx, "udp4", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	transport := &quic.Transport{Conn: socket}
	defer func() {
		if err != nil {
			transport.Close()
			socket.Close()
		}
	}()
	conn, err := transport.Dial(ctx, peer, &tls.Config{ServerName: n.origin.Hostname(), NextProtos: []string{http3.NextProtoH3}, MinVersion: tls.VersionTLS13},
		&quic.Config{EnableDatagrams: true, HandshakeIdleTimeout: dialTimeout, KeepAlivePeriod: 20 * time.Second})
	if err != nil {
		return nil, err
	}
	h3 := &http3.Transport{EnableDatagrams: true}
	req, err := masque.NewRequest(ctx, uritemplate.MustNew(template), target)
	if err != nil {
		return nil, err
	}
	req.Header().Set("Authorization", "Bearer "+key)
	udp, _, err := masque.NewClientConn(h3.NewClientConn(conn)).Dial(req)
	if err != nil {
		return nil, err
	}
	return &tunnel{Conn: udp, closeTransport: func() {
		conn.CloseWithError(0, "")
		h3.Close()
		transport.Close()
		socket.Close()
	}}, nil
}

type tunnel struct {
	*masque.Conn
	closeTransport func()
	once           sync.Once
}

func (t *tunnel) Read(p []byte) (int, error) {
	n, _, err := t.Conn.ReadFrom(p)
	if err != nil {
		return n, errors.Join(net.ErrClosed, err)
	}
	return n, nil
}

func (t *tunnel) Write(p []byte) (int, error) { return t.Conn.WriteTo(p, nil) }

func (t *tunnel) Close() error {
	t.once.Do(func() {
		t.Conn.Close()
		t.closeTransport()
	})
	return nil
}
