package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"lowpath-masque-edge/internal/access"
)

const (
	passwordSessionTTL = 12 * time.Hour
	rememberedLoginTTL = 30 * 24 * time.Hour
)

type passwordLogin struct {
	accounts string
	devices  string
	logins   string
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
	logins := filepath.Join(devices, "remembered")
	if err := os.MkdirAll(logins, 0700); err != nil {
		return err
	}
	rt.passwords = &passwordLogin{accounts: accounts, devices: devices, logins: logins, routes: rt,
		slots: make(chan struct{}, 2), attempts: make(map[string]loginAttempts)}
	return nil
}

func (g *passwordLogin) mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/login/password", g.login)
	mux.HandleFunc("POST /api/v1/login/refresh", g.refresh)
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
		Remember bool   `json:"remember"`
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
	authorized := g.authorizedNodes(account)
	if len(authorized) == 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	now := time.Now().UTC()
	var loginID, rememberToken string
	expires := now.Add(passwordSessionTTL)
	if input.Remember {
		rememberToken, err = access.IssueDevice(g.logins, access.Device{
			Label: name, ExpiresAt: now.Add(rememberedLoginTTL), AccountRevision: account.Revision,
		}, 0600)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		loginID, _, _ = strings.Cut(rememberToken, ".")
	}
	credential, err := g.issueSession(name, account, authorized, expires, loginID)
	if err != nil {
		if loginID != "" {
			_ = os.Remove(access.Path(g.logins, loginID))
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	writeLogin(w, loginResponse{Credential: credential, Name: name, RememberToken: rememberToken})
}

func (g *passwordLogin) authorizedNodes(account access.Account) []string {
	var authorized []string
	for _, node := range g.routes.nodes {
		if slices.Contains(account.Nodes, node.id) {
			authorized = append(authorized, node.id)
		}
	}
	return authorized
}

func (g *passwordLogin) issueSession(name string, account access.Account, nodes []string, expires time.Time, loginID string) (string, error) {
	return access.IssueDevice(g.devices, access.Device{
		Label: name, Nodes: nodes, ExpiresAt: expires, AccountRevision: account.Revision, LoginID: loginID,
	}, 0600)
}

type loginResponse struct {
	Credential    string `json:"credential"`
	Name          string `json:"name"`
	RememberToken string `json:"remember_token,omitempty"`
}

func writeLogin(w http.ResponseWriter, result loginResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (g *passwordLogin) refresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.TLS == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	now := time.Now().UTC()
	login, ok := access.Authenticate(g.logins, r.Header.Get("Authorization"), now)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	account, ok := g.accountFor(login)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	// Reuse the per-source bound, but charge only valid remembered logins so
	// an arbitrary token cannot force password checks to be rate limited.
	source, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !g.allowAttempt("refresh:"+source, now) {
		tooManyAttempts(w)
		return
	}
	authorized := g.authorizedNodes(account)
	if len(authorized) == 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	expires := now.Add(passwordSessionTTL)
	if login.ExpiresAt.Before(expires) {
		expires = login.ExpiresAt
	}
	credential, err := g.issueSession(login.Label, account, authorized, expires, login.ID)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	writeLogin(w, loginResponse{Credential: credential, Name: login.Label})
}

func tooManyAttempts(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	w.WriteHeader(http.StatusTooManyRequests)
}

func (g *passwordLogin) accountFor(grant access.Grant) (access.Account, bool) {
	account, err := access.ReadAccount(g.accounts, grant.Label)
	return account, err == nil && grant.AccountRevision != "" && account.Revision == grant.AccountRevision
}

func (g *passwordLogin) loginValid(grant access.Grant, now time.Time) bool {
	if grant.LoginID == "" {
		return true
	}
	login, err := access.ReadDevice(g.logins, grant.LoginID)
	return err == nil && now.Before(login.ExpiresAt) && login.Label == grant.Label && login.AccountRevision == grant.AccountRevision
}

func (g *passwordLogin) authorize(header string, now time.Time) (access.Grant, bool) {
	grant, ok := access.Authenticate(g.devices, header, now)
	if !ok || !g.loginValid(grant, now) {
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
	if !grant.StillValid(g.devices, node, now) || !g.loginValid(grant, now) {
		return false
	}
	account, ok := g.accountFor(grant)
	return ok && slices.Contains(account.Nodes, node)
}

func (g *passwordLogin) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	header, now := r.Header.Get("Authorization"), time.Now()
	if login, ok := access.Authenticate(g.logins, header, now); ok {
		removeLogin(w, g.logins, login.ID)
		return
	}
	grant, ok := access.Authenticate(g.devices, header, now)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if grant.LoginID != "" {
		removeLogin(w, g.logins, grant.LoginID)
		return
	}
	removeLogin(w, g.devices, grant.ID)
}

func removeLogin(w http.ResponseWriter, dir, id string) {
	if !access.ValidDevice(id) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if err := os.Remove(access.Path(dir, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
