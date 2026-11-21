package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"lowpath-masque-edge/internal/access"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGitHubLoginIssuesSingleRevocableDevice(t *testing.T) {
	profile := `{"id":42,"login":"player"}`
	g := &githubLogin{dir: t.TempDir(), callback: "https://example.org/api/v1/login/github/callback", flows: make(map[string]*githubFlow),
		config: githubConfig{ClientID: "client", Secret: "test-secret", Accounts: map[int64][]string{42: {"tokyo"}}},
	}
	g.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := profile
		if req.URL.Host == "github.com" {
			if err := req.ParseForm(); err != nil || req.Form.Get("code_verifier") == "" || req.Form.Get("redirect_uri") != g.callback {
				t.Fatal("token exchange did not use the stored PKCE verifier and callback")
			}
			body = `{"access_token":"provider-test-only"}`
		} else if req.URL.Host != "api.github.com" || req.Header.Get("Authorization") != "Bearer provider-test-only" {
			t.Fatal("profile request used incorrect endpoint or authorization")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	mux := http.NewServeMux()
	g.mount(mux)
	call := func(method, path, body, token string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, want %d", method, strings.Split(path, "?")[0], w.Code, want)
		}
		return w
	}
	start := func() (state, ticket string) {
		t.Helper()
		w := call("POST", "/api/v1/login/github/start", "", "", 200)
		var started map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(started["authorization_url"])
		if err != nil || u.Host != "github.com" || u.Query().Get("code_challenge_method") != "S256" {
			t.Fatal("expected GitHub authorization URL with PKCE")
		}
		return u.Query().Get("state"), `{"ticket":"` + started["ticket"] + `"}`
	}
	state, ticket := start()
	call("POST", "/api/v1/login/github/poll", ticket, "", 202)
	callback := "/api/v1/login/github/callback?code=test&state=" + state
	call("GET", callback, "", "", 200)
	call("GET", callback, "", "", 410)
	w := call("POST", "/api/v1/login/github/poll", ticket, "", 200)
	var device map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &device); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), "provider-test-only") {
		t.Fatal("provider token reached the desktop")
	}
	value := device["credential"]
	grant, ok := access.Authenticate(g.dir, "Bearer "+value, time.Now())
	if !ok || !grant.Allows("tokyo") || grant.Allows("seoul") {
		t.Fatal("incorrect node grant")
	}
	call("POST", "/api/v1/login/github/poll", ticket, "", 410)
	call("POST", "/api/v1/login/logout", "", value, 204)
	if _, ok := access.Authenticate(g.dir, "Bearer "+value, time.Now()); ok {
		t.Fatal("signed-out device can still authenticate")
	}
	profile = `{"id":99,"login":"outsider"}`
	state, ticket = start()
	call("GET", "/api/v1/login/github/callback?code=test&state="+state, "", "", 403)
	call("POST", "/api/v1/login/github/poll", ticket, "", 403)
}
