package managed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPasswordLoginKeepsDeviceCredentialInBackend(t *testing.T) {
	const credential = "aaaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/login/password":
			var input struct{ Username, Password string }
			if json.NewDecoder(r.Body).Decode(&input) != nil || input.Username != "player" || input.Password != "test-password-fixture" {
				w.WriteHeader(401)
				return
			}
			respond(w, 200, map[string]string{"credential": credential, "name": "player"})
		case "/api/v1/nodes":
			if r.Header.Get("Authorization") != "Bearer "+credential {
				w.WriteHeader(401)
				return
			}
			respond(w, 200, Directory{Template: "https://" + r.Host + "/api/v1/udp/{target_host}/{target_port}/",
				Expires: time.Now().Add(time.Hour), Nodes: []Node{{ID: "tokyo", Name: "东京", Status: "ready", Target: "tokyo.relay.invalid:443"}}})
		case "/api/v1/login/logout":
			if r.Header.Get("Authorization") != "Bearer "+credential {
				t.Error("logout did not revoke the gateway credential")
			}
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected gateway path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	u, _ := url.Parse(remote.URL)
	network := &Network{origin: u, client: remote.Client()}
	stopped := false
	login := &Login{Network: network, StopBoost: func() { stopped = true }}
	mux := http.NewServeMux()
	login.Mount(mux)
	call := func(method, path, body string, status int) string {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != status || strings.Contains(w.Body.String(), credential) {
			t.Fatalf("%s %s returned HTTP %d, or exposed the device credential", method, path, w.Code)
		}
		return w.Body.String()
	}
	call("POST", "/api/account/github", "{}", 404)
	call("POST", "/api/account/password", `{"username":"player","password":"wrong"}`, 403)
	if result := call("POST", "/api/account/password", `{"username":"player","password":"test-password-fixture"}`, 200); !strings.Contains(result, `"signed_in":true`) || len(network.Paths("test")) != 1 {
		t.Fatal("successful login did not unlock the granted node")
	}
	if result := call("POST", "/api/account/logout", "{}", 200); !strings.Contains(result, `"signed_in":false`) || !stopped || len(network.Paths("test")) != 0 {
		t.Fatal("logout did not clear the authorized session")
	}
}

type memoryCredential struct{ value string }

func (s *memoryCredential) Load() (string, error) { return s.value, nil }
func (s *memoryCredential) Save(value string) error {
	s.value = value
	return nil
}
func (s *memoryCredential) Delete() error {
	s.value = ""
	return nil
}

func TestRememberLoginRestoresAfterRestartAndPreservesTemporaryFailures(t *testing.T) {
	accessKey := strings.Repeat("a", 24) + "." + strings.Repeat("b", 64)
	restoredKey := strings.Repeat("c", 24) + "." + strings.Repeat("d", 64)
	refreshKey := strings.Repeat("e", 24) + "." + strings.Repeat("f", 64)
	var unavailable, revoked atomic.Bool
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/v1/login/password":
			var input struct{ Remember bool }
			if json.NewDecoder(r.Body).Decode(&input) != nil || !input.Remember {
				w.WriteHeader(400)
				return
			}
			respond(w, 200, loginGrant{Credential: accessKey, Name: "player", RememberToken: refreshKey})
		case "/api/v1/login/refresh":
			if auth != "Bearer "+refreshKey || revoked.Load() {
				w.WriteHeader(401)
				return
			}
			if unavailable.Load() {
				w.WriteHeader(503)
				return
			}
			respond(w, 200, loginGrant{Credential: restoredKey, Name: "player"})
		case "/api/v1/nodes":
			if auth != "Bearer "+accessKey && auth != "Bearer "+restoredKey {
				w.WriteHeader(401)
				return
			}
			respond(w, 200, Directory{Template: "https://" + r.Host + "/api/v1/udp/{target_host}/{target_port}/",
				Expires: time.Now().Add(time.Hour), Nodes: []Node{{ID: "tokyo", Name: "东京", Status: "ready", Target: "tokyo.relay.invalid:443"}}})
		case "/api/v1/login/logout":
			if auth != "Bearer "+refreshKey {
				t.Error("logout must revoke the remembered login")
				w.WriteHeader(400)
				return
			}
			revoked.Store(true)
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	stored := &memoryCredential{}
	launch := func() *http.ServeMux {
		u, err := url.Parse(remote.URL)
		if err != nil {
			t.Fatal(err)
		}
		mux := http.NewServeMux()
		login := &Login{Network: &Network{origin: u, client: remote.Client()}, Store: stored}
		login.Mount(mux)
		return mux
	}
	type accountStatus struct {
		SignedIn       bool `json:"signed_in"`
		RestorePending bool `json:"restore_pending"`
	}
	call := func(mux *http.ServeMux, method, path, payload string) accountStatus {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(payload)))
		if w.Code != 200 {
			t.Fatalf("%s %s returned HTTP %d", method, path, w.Code)
		}
		if body := w.Body.String(); strings.Contains(body, refreshKey) || strings.Contains(body, accessKey) || strings.Contains(body, restoredKey) {
			t.Fatal("local UI received a credential")
		}
		var state accountStatus
		if err := json.NewDecoder(w.Body).Decode(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	first := launch()
	if state := call(first, "POST", "/api/account/password", `{"username":"player","password":"test-password-fixture","remember":true}`); !state.SignedIn || stored.value != refreshKey {
		t.Fatal("password login did not remember the refresh credential")
	}
	unavailable.Store(true)
	second := launch()
	if state := call(second, "GET", "/api/account", ""); state.SignedIn || !state.RestorePending || stored.value != refreshKey {
		t.Fatal("temporary gateway outage cleared the saved login or left an invalid signed-in state")
	}
	unavailable.Store(false)
	if state := call(second, "GET", "/api/account", ""); !state.SignedIn || state.RestorePending {
		t.Fatal("fresh client did not restore the session")
	}
	if state := call(second, "POST", "/api/account/logout", "{}"); state.SignedIn || !revoked.Load() || stored.value != "" {
		t.Fatal("explicit logout did not clear local and gateway state")
	}
	if state := call(launch(), "GET", "/api/account", ""); state.SignedIn || state.RestorePending {
		t.Fatal("fresh client remained signed in after logout")
	}
	stored.value = refreshKey // simulate a stale Keychain item after gateway revocation
	if state := call(launch(), "GET", "/api/account", ""); state.SignedIn || state.RestorePending || stored.value != "" {
		t.Fatal("revoked credential was not discarded")
	}
}
