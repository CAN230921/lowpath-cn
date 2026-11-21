package managed

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type Login struct {
	Network     *Network
	OpenBrowser func(string) error
	StopBoost   func()

	mu     sync.Mutex
	ticket string
}

func (l *Login) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account", l.account)
	mux.HandleFunc("POST /api/account/device", l.device)
	mux.HandleFunc("POST /api/account/github", l.github)
	mux.HandleFunc("POST /api/account/poll", l.poll)
	mux.HandleFunc("POST /api/account/logout", l.logout)
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func authError(w http.ResponseWriter, status int, kind, message string) {
	respond(w, status, map[string]string{"error": kind, "message": message})
}

func (l *Login) account(w http.ResponseWriter, _ *http.Request) {
	name, expiry := l.Network.Account()
	respond(w, 200, map[string]any{"signed_in": name != "", "name": name, "expires_at": expiry})
}

func (l *Login) device(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil || len(strings.TrimSpace(input.Credential)) != 89 {
		authError(w, 400, "device_credential", "请输入有效的设备凭据。")
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.Network.Login(r.Context(), strings.TrimSpace(input.Credential), "设备登录"); err != nil {
		authError(w, 403, "device_credential", "设备凭据校验失败。请检查凭据、有效期和网关连接。")
		return
	}
	l.ticket = ""
	l.account(w, r)
}

func (l *Login) github(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var flow struct {
		URL    string `json:"authorization_url"`
		Ticket string `json:"ticket"`
	}
	code, err := l.Network.Request(r.Context(), http.MethodPost, "/api/v1/login/github/start", "", nil, &flow)
	if err != nil || code != 200 {
		authError(w, 503, "oauth_unavailable", "GitHub 登录暂不可用。请稍后重试，或使用设备凭据登录。")
		return
	}
	u, err := url.Parse(flow.URL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.Path != "/login/oauth/authorize" || len(flow.Ticket) != 43 {
		authError(w, 502, "oauth_response", "网关返回了无效的登录地址。")
		return
	}
	if err := l.OpenBrowser(flow.URL); err != nil {
		authError(w, 500, "browser_failed", "无法打开系统浏览器。")
		return
	}
	l.ticket = flow.Ticket
	respond(w, 200, map[string]bool{"pending": true})
}

func (l *Login) poll(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ticket == "" {
		authError(w, 409, "oauth_expired", "登录已结束，请重新发起 GitHub 登录。")
		return
	}
	body, _ := json.Marshal(map[string]string{"ticket": l.ticket})
	var result struct {
		Device string `json:"credential"`
		Name   string `json:"name"`
	}
	code, err := l.Network.Request(r.Context(), http.MethodPost, "/api/v1/login/github/poll", "", bytes.NewReader(body), &result)
	switch {
	case code == 202:
		respond(w, 200, map[string]bool{"pending": true})
	case code == 403 || code == 410:
		l.ticket = ""
		authError(w, code, "oauth_rejected", "登录已过期、取消，或该 GitHub 账号未获授权。")
	case err != nil || code != 200:
		authError(w, 502, "oauth_response", "暂时无法读取登录状态，请重试。")
	default:
		if err := l.Network.Login(r.Context(), result.Device, result.Name); err != nil {
			l.ticket = ""
			authError(w, 502, "device_credential", "已完成 GitHub 授权，但无法验证设备会话。请重新登录。")
			return
		}
		l.ticket = ""
		l.account(w, r)
	}
}

func (l *Login) logout(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ticket = ""
	key := l.Network.Logout()
	if l.StopBoost != nil {
		l.StopBoost()
	}
	if key != "" {
		_, _ = l.Network.Request(r.Context(), http.MethodPost, "/api/v1/login/logout", key, nil, nil)
	}
	l.account(w, r)
}
