import { useState, type FormEvent } from "react";
import { Loader2 } from "lucide-react";
import { api, ApiError } from "../api";
import { Logo } from "./Bits";

export function Login({ onSuccess }: { onSuccess: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const signIn = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.passwordLogin(username.trim(), password);
      setPassword("");
      onSuccess();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "登录失败，请重试。");
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="mapping">
      <section className="mapping-inner login" aria-labelledby="login-title">
        <div className="brand"><Logo />Lowpath</div>
        <h1 id="login-title">登录加速器</h1>
        <form onSubmit={signIn}>
          <label htmlFor="login-username">账号</label>
          <input id="login-username" autoComplete="username" spellCheck={false} value={username} onChange={(e) => setUsername(e.target.value)} maxLength={32} disabled={busy} required />
          <label htmlFor="login-password">密码</label>
          <input id="login-password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} maxLength={256} disabled={busy} required />
          <button className="btn btn-primary" disabled={busy || !username.trim() || !password}>
            {busy && <Loader2 size={15} className="spin" />}{busy ? "正在登录…" : "登录"}
          </button>
          <p className="muted">账号由管理员开通。</p>
        </form>
        {error && <p className="form-error" role="alert">{error}</p>}
      </section>
    </main>
  );
}
