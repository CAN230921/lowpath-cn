package managed

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

type Login struct {
	Network   *Network
	StopBoost func()

	mu sync.Mutex
}

func (l *Login) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account", l.account)
	mux.HandleFunc("POST /api/account/password", l.password)
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

func (l *Login) password(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil || strings.TrimSpace(input.Username) == "" || input.Password == "" {
		authError(w, 400, "password_input", "请输入账号和密码。")
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if name, _ := l.Network.Account(); name != "" {
		authError(w, 409, "already_signed_in", "请先退出当前账号。")
		return
	}
	body, _ := json.Marshal(input)
	defer clear(body)
	var grant struct {
		Credential string `json:"credential"`
		Name       string `json:"name"`
	}
	code, err := l.Network.Request(r.Context(), http.MethodPost, "/api/v1/login/password", "", bytes.NewReader(body), &grant)
	switch {
	case code == http.StatusUnauthorized:
		authError(w, 403, "credentials_rejected", "账号或密码错误。")
	case code == http.StatusForbidden:
		authError(w, 403, "no_node_access", "账号尚未分配加速节点。")
	case code == http.StatusTooManyRequests:
		authError(w, 429, "login_rate_limit", "登录请求过多，请稍后再试。")
	case err != nil || code != 200 || grant.Name == "" || len(grant.Credential) != 89:
		authError(w, 503, "login_unavailable", "暂时无法连接登录服务，请稍后再试。")
	default:
		if err := l.Network.Login(r.Context(), grant.Credential, grant.Name); err != nil {
			_, _ = l.Network.Request(r.Context(), http.MethodPost, "/api/v1/login/logout", grant.Credential, nil, nil)
			authError(w, 502, "device_credential", "设备会话验证失败，请重新登录。")
			return
		}
		l.account(w, r)
	}
}

func (l *Login) logout(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := l.Network.Logout()
	if l.StopBoost != nil {
		l.StopBoost()
	}
	if key != "" {
		_, _ = l.Network.Request(r.Context(), http.MethodPost, "/api/v1/login/logout", key, nil, nil)
	}
	l.account(w, r)
}
