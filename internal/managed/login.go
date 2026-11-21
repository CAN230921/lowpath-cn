package managed

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	credentialLength = 89
	refreshBefore    = 15 * time.Minute
)

type Login struct {
	Network   *Network
	StopBoost func()
	Store     CredentialStore

	mu             sync.Mutex
	storeRead      bool
	remembered     string
	restorePending bool
	notice         string
}

type loginGrant struct {
	Credential    string `json:"credential"`
	Name          string `json:"name"`
	RememberToken string `json:"remember_token"`
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

func (l *Login) account(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore(r.Context())
	l.writeAccount(w)
}

func (l *Login) writeAccount(w http.ResponseWriter) {
	name, expiry := l.Network.Account()
	respond(w, 200, map[string]any{
		"signed_in": name != "", "name": name, "expires_at": expiry,
		"can_remember": l.Store != nil, "restore_pending": l.restorePending, "notice": l.notice,
	})
}

func (l *Login) readStore() {
	if l.storeRead || l.Store == nil {
		return
	}
	l.storeRead = true
	value, err := l.Store.Load()
	if err != nil {
		l.notice = "无法读取钥匙串中的登录凭据，请重新登录。"
		return
	}
	l.remembered = value
}

func (l *Login) restore(ctx context.Context) {
	if _, expiry := l.Network.Account(); time.Until(expiry) > refreshBefore {
		return
	}
	l.readStore()
	if l.remembered == "" {
		return
	}
	var grant loginGrant
	code, err := l.Network.Request(ctx, http.MethodPost, "/api/v1/login/refresh", l.remembered, nil, &grant)
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		l.Network.Logout()
		if l.StopBoost != nil {
			l.StopBoost()
		}
		l.remembered = ""
		l.restorePending = false
		l.notice = "保存的登录已失效，请重新登录。"
		if l.Store != nil {
			_ = l.Store.Delete()
		}
		return
	}
	if err != nil || code != http.StatusOK || grant.Name == "" || len(grant.Credential) != credentialLength ||
		l.Network.Login(ctx, grant.Credential, grant.Name) != nil {
		l.restorePending = true
		return // the refresh credential is still usable after a temporary outage
	}
	l.restorePending = false
}

func (l *Login) password(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
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
	l.readStore()
	input.Remember = input.Remember && l.Store != nil
	body, _ := json.Marshal(input)
	defer clear(body)
	var grant loginGrant
	code, err := l.Network.Request(r.Context(), http.MethodPost, "/api/v1/login/password", "", bytes.NewReader(body), &grant)
	switch {
	case code == http.StatusUnauthorized:
		authError(w, 403, "credentials_rejected", "账号或密码错误。")
	case code == http.StatusForbidden:
		authError(w, 403, "no_node_access", "账号尚未分配加速节点。")
	case code == http.StatusTooManyRequests:
		authError(w, 429, "login_rate_limit", "登录请求过多，请稍后再试。")
	case err != nil || code != 200 || grant.Name == "" || len(grant.Credential) != credentialLength:
		authError(w, 503, "login_unavailable", "暂时无法连接登录服务，请稍后再试。")
	default:
		if err := l.Network.Login(r.Context(), grant.Credential, grant.Name); err != nil {
			_, _ = l.Network.Request(r.Context(), http.MethodPost, "/api/v1/login/logout", grant.Credential, nil, nil)
			authError(w, 502, "device_credential", "设备会话验证失败，请重新登录。")
			return
		}
		previous := l.remembered
		l.remembered, l.notice, l.restorePending = "", "", false
		if input.Remember && len(grant.RememberToken) == credentialLength {
			l.remembered = grant.RememberToken
			if err := l.Store.Save(l.remembered); err != nil {
				l.notice = "已登录。钥匙串暂时无法保存登录，下次打开需重新输入密码。"
			}
		} else {
			if l.Store != nil && l.Store.Delete() != nil {
				l.notice = "已登录。钥匙串中的上一次登录凭据未能清除。"
			} else if input.Remember {
				l.notice = "已登录。网关暂未提供记住登录，下次打开需重新输入密码。"
			}
		}
		if previous != "" && previous != l.remembered {
			l.revoke(r.Context(), previous)
		}
		l.writeAccount(w)
	}
}

func (l *Login) logout(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.readStore()
	revoked := false
	if l.Store != nil && l.Store.Delete() != nil {
		revoked = l.remembered != "" && l.revoke(r.Context(), l.remembered)
		if !revoked {
			authError(w, 503, "logout_failed", "当前无法清除钥匙串中的登录凭据，请重试退出。")
			return
		}
	}
	key := l.Network.Logout()
	if l.StopBoost != nil {
		l.StopBoost()
	}
	if l.remembered != "" {
		key = l.remembered
	}
	l.remembered, l.restorePending, l.notice = "", false, ""
	if !revoked && key != "" && !l.revoke(r.Context(), key) {
		l.notice = "已在本机退出。网关暂时无法确认会话撤销。"
	}
	l.writeAccount(w)
}

func (l *Login) revoke(ctx context.Context, credential string) bool {
	code, err := l.Network.Request(ctx, http.MethodPost, "/api/v1/login/logout", credential, nil, nil)
	return err == nil && (code == http.StatusNoContent || code == http.StatusUnauthorized)
}
