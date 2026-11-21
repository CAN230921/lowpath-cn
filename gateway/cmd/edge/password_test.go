package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lowpath-masque-edge/internal/access"
)

func TestPasswordLoginRequiresOwnerAccount(t *testing.T) {
	accounts := t.TempDir()
	rt, err := newRoutes([]nodeSpec{{ID: "tokyo", Name: "东京"}, {ID: "seoul", Name: "首尔"}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.configurePasswords(accounts, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	rt.mount(mux, "gateway.example")
	call := func(method, path, token, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://gateway.example"+path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s returned HTTP %d, want %d", method, path, w.Code, want)
		}
		return w
	}
	const password = "eight-7!"
	const endpoint = "/api/v1/login/password"
	const body = `{"username":"player","password":"` + password + `"}`
	call("POST", "/api/v1/login/register", "", body, 404)
	call("POST", "/api/v1/login/github/start", "", "", 404)
	call("POST", endpoint, "", body, 401)
	if err := access.CreateAccount(accounts, "Player", []byte(password), []string{"tokyo"}); err != nil {
		t.Fatal(err)
	}
	if err := access.CreateAccount(accounts, "player", []byte("replacement-test-only"), []string{"seoul"}); !os.IsExist(err) {
		t.Fatalf("duplicate account create: %v", err)
	}
	record, err := os.ReadFile(filepath.Join(accounts, "player.json"))
	if err != nil || strings.Contains(string(record), password) || !strings.Contains(string(record), "argon2id_v1") {
		t.Fatal("account record must contain only a hashed password")
	}
	call("POST", endpoint, "", `{"username":"player","password":"wrong-password-test"}`, 401)
	login := func() string {
		t.Helper()
		w := call("POST", endpoint, "", body, 200)
		var result map[string]string
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result["name"] != "player" || result["credential"] == "" {
			t.Fatal("password login did not issue a device credential")
		}
		return result["credential"]
	}
	credential := login()
	w := call("GET", "/api/v1/nodes", credential, "", 200)
	if !strings.Contains(w.Body.String(), `"id":"tokyo"`) || strings.Contains(w.Body.String(), `"id":"seoul"`) {
		t.Fatal("account's node grant was not enforced")
	}
	call("POST", "/api/v1/login/logout", credential, "", 204)
	call("GET", "/api/v1/nodes", credential, "", 401)
	credential = login()
	grant, ok := rt.authenticate("Bearer "+credential, time.Now())
	if !ok || !rt.stillValid(grant, "tokyo", time.Now()) {
		t.Fatal("newly issued grant is invalid")
	}
	if err := os.Remove(filepath.Join(accounts, "player.json")); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/v1/nodes", credential, "", 401)
	if rt.stillValid(grant, "tokyo", time.Now()) {
		t.Fatal("deleted account can continue using an existing tunnel")
	}
}

func TestPasswordAttemptsAreLimitedBySource(t *testing.T) {
	g := &passwordLogin{attempts: make(map[string]loginAttempts)}
	now := time.Now()
	for i := 0; i < 10; i++ {
		if !g.allowAttempt("192.0.2.1", now) {
			t.Fatalf("attempt %d rejected", i+1)
		}
	}
	if g.allowAttempt("192.0.2.1", now) || !g.allowAttempt("192.0.2.2", now) || !g.allowAttempt("192.0.2.1", now.Add(5*time.Minute)) {
		t.Fatal("login limit did not isolate sources or reset after its window")
	}
}

func TestRememberedLoginRefreshAndRevocation(t *testing.T) {
	accounts := t.TempDir()
	const name = "remember-test"
	const password = "remember-test-only"
	if err := access.CreateAccount(accounts, name, []byte(password), []string{"tokyo"}); err != nil {
		t.Fatal(err)
	}
	rt, err := newRoutes([]nodeSpec{{ID: "tokyo", Name: "东京"}, {ID: "seoul", Name: "首尔"}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.configurePasswords(accounts, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	rt.mount(mux, "gateway.example")
	call := func(method, path, key, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://gateway.example"+path, strings.NewReader(body))
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: HTTP %d, want %d", method, path, w.Code, want)
		}
		return w
	}
	readGrant := func(w *httptest.ResponseRecorder) loginResponse {
		t.Helper()
		var g loginResponse
		if err := json.NewDecoder(w.Body).Decode(&g); err != nil || g.Name != name || g.Credential == "" {
			t.Fatal("missing login grant")
		}
		return g
	}
	login := func() loginResponse {
		t.Helper()
		return readGrant(call("POST", "/api/v1/login/password", "", `{"username":"`+name+`","password":"`+password+`","remember":true}`, 200))
	}
	old := login()
	if old.RememberToken == "" || old.RememberToken == old.Credential {
		t.Fatal("expected a separate refresh credential")
	}
	call("GET", "/api/v1/nodes", old.RememberToken, "", 401)
	call("POST", "/api/v1/login/refresh", old.Credential, "", 401)
	refreshed := readGrant(call("POST", "/api/v1/login/refresh", old.RememberToken, "", 200))
	if refreshed.RememberToken != "" || refreshed.Credential == old.Credential {
		t.Fatal("refresh should issue only a new access credential")
	}
	directory := call("GET", "/api/v1/nodes", refreshed.Credential, "", 200)
	if strings.Contains(directory.Body.String(), `"id":"seoul"`) || !strings.Contains(directory.Body.String(), `"id":"tokyo"`) {
		t.Fatal("node permissions changed at refresh")
	}
	grant, ok := rt.authenticate("Bearer "+refreshed.Credential, time.Now())
	if !ok || grant.LoginID == "" || !rt.stillValid(grant, "tokyo", time.Now()) {
		t.Fatal("refreshed grant did not authorize a tunnel")
	}
	call("POST", "/api/v1/login/logout", old.RememberToken, "", 204)
	call("POST", "/api/v1/login/refresh", old.RememberToken, "", 401)
	call("GET", "/api/v1/nodes", old.Credential, "", 401)
	call("GET", "/api/v1/nodes", refreshed.Credential, "", 401)
	if rt.stillValid(grant, "tokyo", time.Now()) {
		t.Fatal("logout did not revoke the existing tunnel's grant")
	}
	old = login()
	call("POST", "/api/v1/login/logout", old.Credential, "", 204)
	call("POST", "/api/v1/login/refresh", old.RememberToken, "", 401)
	account, err := access.ReadAccount(accounts, name)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := access.IssueDevice(rt.passwords.logins, access.Device{
		Label: name, AccountRevision: account.Revision, ExpiresAt: time.Now().Add(-time.Minute),
	}, 0600)
	if err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/v1/login/refresh", expired, "", 401)
}
