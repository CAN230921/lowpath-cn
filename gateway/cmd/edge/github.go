package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"lowpath-masque-edge/internal/access"
)

const loginLifetime = 5 * time.Minute

type githubConfig struct {
	ClientID string             `json:"client_id"`
	Secret   string             `json:"client_secret"`
	Accounts map[int64][]string `json:"accounts"`
}

type githubLogin struct {
	config   githubConfig
	callback string
	dir      string
	client   *http.Client
	mu       sync.Mutex
	flows    map[string]*githubFlow
}

type githubFlow struct {
	Ticket   string
	Verifier string
	Expires  time.Time
	Source   string
	Claimed  bool
	Finished bool
	Name     string
	Nodes    []string
}

func (rt *routes) configureGitHub(host, credential, dir string) error {
	raw, err := os.ReadFile(credential)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var config githubConfig
	if err := json.Unmarshal(raw, &config); err != nil || config.ClientID == "" || config.Secret == "" || len(config.Accounts) == 0 {
		return errors.New("invalid GitHub OAuth configuration")
	}
	valid := make(map[string]bool)
	for _, node := range rt.nodes {
		valid[node.id] = true
	}
	for id, nodes := range config.Accounts {
		if id <= 0 || len(nodes) == 0 || len(nodes) > 4 {
			return errors.New("each permitted GitHub account needs one to four nodes")
		}
		for _, node := range nodes {
			if !valid[node] {
				return errors.New("invalid GitHub OAuth node grant")
			}
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	rt.oauth = &githubLogin{config: config, dir: dir, flows: make(map[string]*githubFlow),
		callback: "https://" + host + "/api/v1/login/github/callback",
		client:   &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	return nil
}

func randomURLToken() string {
	var b [32]byte
	rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (g *githubLogin) mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/login/github/start", g.start)
	mux.HandleFunc("GET /api/v1/login/github/callback", g.complete)
	mux.HandleFunc("POST /api/v1/login/github/poll", g.poll)
	mux.HandleFunc("POST /api/v1/login/logout", g.logout)
}

func loginHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func (g *githubLogin) start(w http.ResponseWriter, r *http.Request) {
	loginHeaders(w)
	source, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	count := 0
	for state, flow := range g.flows {
		if now.After(flow.Expires) {
			delete(g.flows, state)
		} else if flow.Source == source {
			count++
		}
	}
	if len(g.flows) >= 256 || count >= 8 {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	state := randomURLToken()
	flow := &githubFlow{Ticket: randomURLToken(), Verifier: randomURLToken(), Expires: now.Add(loginLifetime), Source: source}
	g.flows[state] = flow
	challenge := sha256.Sum256([]byte(flow.Verifier))
	u := url.URL{Scheme: "https", Host: "github.com", Path: "/login/oauth/authorize", RawQuery: url.Values{
		"client_id": {g.config.ClientID}, "redirect_uri": {g.callback}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
	}.Encode()}
	loginJSON(w, map[string]string{"authorization_url": u.String(), "ticket": flow.Ticket})
}

func (g *githubLogin) complete(w http.ResponseWriter, r *http.Request) {
	loginHeaders(w)
	state := r.URL.Query().Get("state")
	g.mu.Lock()
	flow := g.flows[state]
	if flow == nil || flow.Claimed || time.Now().After(flow.Expires) {
		g.mu.Unlock()
		http.Error(w, "登录链接已失效，请回到客户端重新登录。", http.StatusGone)
		return
	}
	flow.Claimed = true
	g.mu.Unlock()

	name, nodes := g.identity(r.Context(), r.URL.Query().Get("code"), flow.Verifier)
	g.mu.Lock()
	flow.Name, flow.Nodes, flow.Finished = name, nodes, true
	g.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if len(nodes) == 0 {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "登录未通过。请回到客户端查看状态。")
		return
	}
	_, _ = io.WriteString(w, "GitHub 授权完成。请回到加速器客户端。")
}

func (g *githubLogin) identity(ctx context.Context, code, verifier string) (string, []string) {
	if code == "" || len(code) > 256 {
		return "", nil
	}
	values := url.Values{"client_id": {g.config.ClientID}, "client_secret": {g.config.Secret},
		"code": {code}, "code_verifier": {verifier}, "redirect_uri": {g.callback}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", nil
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var token struct {
		Value string `json:"access_token"`
	}
	if !g.github(req, &token) || token.Value == "" {
		return "", nil
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", nil
	}
	req.Header.Set("Authorization", "Bearer "+token.Value)
	var identity struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if !g.github(req, &identity) || identity.Login == "" {
		return "", nil
	}
	return identity.Login, g.config.Accounts[identity.ID]
}

func (g *githubLogin) github(req *http.Request, output any) bool {
	req.Header.Set("Accept", "application/json")
	res, err := g.client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode == 200 && json.NewDecoder(io.LimitReader(res.Body, 32<<10)).Decode(output) == nil
}

func (g *githubLogin) poll(w http.ResponseWriter, r *http.Request) {
	loginHeaders(w)
	var input struct {
		Ticket string `json:"ticket"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || len(input.Ticket) != 43 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for state, flow := range g.flows {
		if flow.Ticket != input.Ticket {
			continue
		}
		if time.Now().After(flow.Expires) {
			delete(g.flows, state)
			break
		}
		if !flow.Finished {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if len(flow.Nodes) == 0 {
			delete(g.flows, state)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		value, err := g.issue(flow)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		delete(g.flows, state)
		loginJSON(w, map[string]string{"credential": value, "name": flow.Name})
		return
	}
	w.WriteHeader(http.StatusGone)
}

func (g *githubLogin) issue(flow *githubFlow) (string, error) {
	var idBytes [12]byte
	var secretBytes [32]byte
	rand.Read(idBytes[:])
	rand.Read(secretBytes[:])
	id, secret := hex.EncodeToString(idBytes[:]), hex.EncodeToString(secretBytes[:])
	digest := sha256.Sum256([]byte(secret))
	declaration := access.Device{Label: flow.Name, Digest: hex.EncodeToString(digest[:]), Nodes: flow.Nodes, ExpiresAt: time.Now().UTC().Add(12 * time.Hour)}
	file, err := os.CreateTemp(g.dir, ".oauth-device-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(declaration)
	if err = errors.Join(err, file.Close()); err != nil {
		return "", err
	}
	if err := os.Link(file.Name(), access.Path(g.dir, id)); err != nil {
		return "", err
	}
	return id + "." + secret, nil
}

func (g *githubLogin) logout(w http.ResponseWriter, r *http.Request) {
	loginHeaders(w)
	grant, ok := access.Authenticate(g.dir, r.Header.Get("Authorization"), time.Now())
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if err := os.Remove(access.Path(g.dir, grant.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func loginJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
