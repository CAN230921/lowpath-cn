package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"

	masque "github.com/quic-go/masque-go"
	"github.com/yosida95/uritemplate/v3"

	"lowpath-masque-edge/internal/access"
)

const routeTemplate = "/api/v1/udp/{target_host}/{target_port}/"

type nodeSpec struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Upstream string `json:"upstream,omitempty"`
	RelayKey string `json:"relay_key,omitempty"`
}

type node struct {
	id, name, target string
	upstream         netip.AddrPort
	token            uint64
	healthy          atomic.Bool
}

type routes struct {
	oauth    *githubLogin
	nodes    []*node
	byTarget map[string]*node
	devices  string
	sessions chan struct{}
	mu       sync.Mutex
	active   map[string]int
}

func loadRoutes(path, devices string) (*routes, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(devices); err != nil || !info.IsDir() {
		return nil, errors.New("device registry directory is unavailable")
	}
	var configs []nodeSpec
	if err := json.Unmarshal(raw, &configs); err != nil {
		return nil, fmt.Errorf("read route config: %w", err)
	}
	return newRoutes(configs, devices)
}

func newRoutes(configs []nodeSpec, devices string) (*routes, error) {
	rt := &routes{devices: devices, byTarget: make(map[string]*node), sessions: make(chan struct{}, 32), active: make(map[string]int)}
	used := make(map[netip.AddrPort]bool)
	for _, spec := range configs {
		if !access.ValidNode(spec.ID) || spec.Name == "" {
			return nil, errors.New("invalid node ID or name")
		}
		n := &node{id: spec.ID, name: spec.Name, target: spec.ID + ".relay.invalid:443"}
		if rt.byTarget[n.target] != nil {
			return nil, fmt.Errorf("duplicate node %s", spec.ID)
		}
		if spec.Upstream != "" || spec.RelayKey != "" {
			addr, err := netip.ParseAddrPort(spec.Upstream)
			if err != nil || addr.Addr() != netip.AddrFrom4([4]byte{127, 0, 0, 1}) || addr.Port() < 1024 ||
				used[addr] || spec.RelayKey == "" {
				return nil, fmt.Errorf("invalid or duplicate loopback upstream for %s", spec.ID)
			}
			used[addr] = true
			n.upstream = addr
			sum := sha256.Sum256([]byte("lowpath-relay-v1:" + spec.RelayKey))
			n.token = max(1, binary.BigEndian.Uint64(sum[:8]))
		}
		rt.nodes = append(rt.nodes, n)
		rt.byTarget[n.target] = n
	}
	if len(rt.nodes) == 0 {
		return nil, errors.New("empty node list")
	}
	return rt, nil
}

func (rt *routes) monitor(ctx context.Context) {
	for _, n := range rt.nodes {
		if !n.upstream.IsValid() {
			continue
		}
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				n.healthy.Store(n.ping())
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
}

func (n *node) ping() bool {
	conn, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(n.upstream))
	if err != nil {
		return false
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return false
	}
	var probe [27]byte
	probe[0], probe[1] = lowpathMagic, lowpathProbe
	binary.BigEndian.PutUint64(probe[2:10], n.token)
	rand.Read(probe[10:26])
	if _, err := conn.Write(probe[:]); err != nil {
		return false
	}
	var reply [1500]byte
	size, err := conn.Read(reply[:])
	return err == nil && size >= len(probe) && reply[0] == lowpathMagic && reply[1] == lowpathProbeReply &&
		[25]byte(reply[2:27]) == [25]byte(probe[2:27])
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="lowpath"`)
	w.WriteHeader(http.StatusUnauthorized)
}

func (rt *routes) mount(mux *http.ServeMux, host string) {
	if rt.oauth != nil {
		rt.oauth.mount(mux)
	}
	templateURL := "https://" + host + routeTemplate
	template := uritemplate.MustNew(templateURL)
	mux.HandleFunc("GET /api/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		grant, ok := rt.authenticate(r.Header.Get("Authorization"), time.Now())
		if !ok {
			unauthorized(w)
			return
		}
		type publicNode struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
			Target string `json:"target,omitempty"`
		}
		reply := struct {
			Template  string       `json:"connect_udp_template"`
			ExpiresAt time.Time    `json:"expires_at"`
			Nodes     []publicNode `json:"nodes"`
		}{Template: templateURL, ExpiresAt: grant.ExpiresAt, Nodes: []publicNode{}}
		for _, n := range rt.nodes {
			if !grant.Allows(n.id) {
				continue
			}
			entry := publicNode{ID: n.id, Name: n.name, Status: "unconfigured"}
			if n.upstream.IsValid() {
				entry.Status = "unreachable"
			}
			if n.healthy.Load() {
				entry.Status, entry.Target = "ready", n.target
			}
			reply.Nodes = append(reply.Nodes, entry)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reply)
	})
	mux.HandleFunc("CONNECT /api/v1/udp/", func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 3 || r.TLS == nil {
			http.NotFound(w, r)
			return
		}
		grant, ok := rt.authenticate(r.Header.Get("Authorization"), time.Now())
		if !ok {
			unauthorized(w)
			return
		}
		req, err := masque.ParseProxyRequest(r, template)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n := rt.byTarget[req.Target]
		if n == nil || !grant.Allows(n.id) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if !n.healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !rt.reserve(grant.ID) {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		defer rt.release(grant.ID)
		if !rt.stillValid(grant, n.id, time.Now()) {
			unauthorized(w)
			return
		}
		rt.tunnel(w, r, n, grant)
	})
}

func (rt *routes) authenticate(header string, now time.Time) (access.Grant, bool) {
	grant, ok := access.Authenticate(rt.devices, header, now)
	if !ok && rt.oauth != nil {
		return access.Authenticate(rt.oauth.dir, header, now)
	}
	return grant, ok
}

func (rt *routes) stillValid(grant access.Grant, node string, now time.Time) bool {
	return grant.StillValid(rt.devices, node, now) || (rt.oauth != nil && grant.StillValid(rt.oauth.dir, node, now))
}

func (rt *routes) reserve(id string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.active[id] >= 4 {
		return false
	}
	select {
	case rt.sessions <- struct{}{}:
		rt.active[id]++
		return true
	default:
		return false
	}
}

func (rt *routes) release(id string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.active[id]--
	if rt.active[id] == 0 {
		delete(rt.active, id)
	}
	<-rt.sessions
}
