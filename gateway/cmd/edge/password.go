package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"lowpath-masque-edge/internal/access"
)

const passwordSessionTTL = 12 * time.Hour

type passwordLogin struct {
	accounts string
	devices  string
	routes   *routes
	slots    chan struct{}
	mu       sync.Mutex
	attempts map[string]loginAttempts
}

type loginAttempts struct {
	until time.Time
	count int
}

func (rt *routes) configurePasswords(accounts, devices string) error {
	if accounts == "" {
		return nil
	}
	if _, err := os.ReadDir(accounts); err != nil {
		return err
	}
	if err := os.MkdirAll(devices, 0700); err != nil {
		return err
	}
	rt.passwords = &passwordLogin{accounts: accounts, devices: devices, routes: rt,
		slots: make(chan struct{}, 2), attempts: make(map[string]loginAttempts)}
	return nil
}

func (g *passwordLogin) mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/login/password", g.login)
	mux.HandleFunc("POST /api/v1/login/logout", g.logout)
}

func (g *passwordLogin) allowAttempt(source string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	const maxSources = 4096
	if len(g.attempts) >= maxSources {
		for addr, old := range g.attempts {
			if !now.Before(old.until) {
				delete(g.attempts, addr)
			}
		}
	}
	a, seen := g.attempts[source]
	if !seen && len(g.attempts) >= maxSources {
		return false
	}
	if !now.Before(a.until) {
		a = loginAttempts{until: now.Add(5 * time.Minute)}
	}
	if a.count >= 10 {
		return false
	}
	a.count++
	g.attempts[source] = a
	return true
}

func (g *passwordLogin) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.TLS == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	name := access.NormalizeAccount(input.Username)
	password := []byte(input.Password)
	defer clear(password)
	if !access.ValidAccount(name) || !access.ValidPassword(password) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		tooManyAttempts(w)
		return
	}
	source, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !g.allowAttempt(source, time.Now()) {
		tooManyAttempts(w)
		return
	}
	account, ok := access.CheckPassword(g.accounts, name, password)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var authorized []string
	for _, node := range g.routes.nodes {
		if slices.Contains(account.Nodes, node.id) {
			authorized = append(authorized, node.id)
		}
	}
	if len(authorized) == 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	credential, err := access.IssueDevice(g.devices, access.Device{
		Label: name, Nodes: authorized, ExpiresAt: time.Now().UTC().Add(passwordSessionTTL), AccountRevision: account.Revision,
	}, 0600)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"credential": credential, "name": name})
}

func tooManyAttempts(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	w.WriteHeader(http.StatusTooManyRequests)
}

func (g *passwordLogin) accountFor(grant access.Grant) (access.Account, bool) {
	account, err := access.ReadAccount(g.accounts, grant.Label)
	return account, err == nil && grant.AccountRevision != "" && account.Revision == grant.AccountRevision
}

func (g *passwordLogin) authorize(header string, now time.Time) (access.Grant, bool) {
	grant, ok := access.Authenticate(g.devices, header, now)
	if !ok {
		return access.Grant{}, false
	}
	account, ok := g.accountFor(grant)
	if !ok {
		return access.Grant{}, false
	}
	grant.Nodes = slices.DeleteFunc(grant.Nodes, func(node string) bool { return !slices.Contains(account.Nodes, node) })
	return grant, true
}

func (g *passwordLogin) stillValid(grant access.Grant, node string, now time.Time) bool {
	if !grant.StillValid(g.devices, node, now) {
		return false
	}
	account, ok := g.accountFor(grant)
	return ok && slices.Contains(account.Nodes, node)
}

func (g *passwordLogin) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	grant, ok := access.Authenticate(g.devices, r.Header.Get("Authorization"), time.Now())
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if err := os.Remove(access.Path(g.devices, grant.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
