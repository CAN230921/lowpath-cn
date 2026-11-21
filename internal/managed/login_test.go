package managed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
