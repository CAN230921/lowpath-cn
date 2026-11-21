import { useEffect, useState, type FormEvent } from "react";
import { KeyRound, Loader2 } from "lucide-react";
import { api, ApiError } from "../api";
import { Logo } from "./Bits";

export function Login({ onSuccess }: { onSuccess: () => void }) {
  const [credential, setCredential] = useState("");
  const [useDevice, setUseDevice] = useState(false);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!pending) return;
    let disposed = false;
    let timer: number;
    const next = () => { timer = window.setTimeout(poll, 1800); };
    const poll = async () => {
      try {
        const result = await api.pollLogin();
        if (disposed) return;
        if ("pending" in result) next();
        else if (result.signed_in) onSuccess();
      } catch (e) {
        if (disposed) return;
        setPending(false);
        setError(e instanceof ApiError ? e.message : "无法确认 GitHub 登录状态。");
      }
    };
    next();
    return () => { disposed = true; window.clearTimeout(timer); };
  }, [pending, onSuccess]);

  const github = async () => {
    setBusy(true);
    setError("");
    try {
      await api.githubLogin();
      setPending(true);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "无法启动 GitHub 登录。");
    } finally {
      setBusy(false);
    }
  };

  const device = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.deviceLogin(credential.trim());
      setCredential("");
      onSuccess();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "设备登录失败。");
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="mapping">
      <section className="mapping-inner login" aria-labelledby="login-title">
        <div className="brand"><Logo />Lowpath</div>
        <h1 id="login-title">登录加速器</h1>
        {pending ? (
          <>
            <p role="status"><Loader2 size={15} className="spin" /> 请在浏览器完成授权，再回到客户端。</p>
            <button className="btn" onClick={() => setPending(false)}>取消等待</button>
          </>
        ) : (
          <>
            <button className="btn btn-primary" disabled={busy} onClick={github}>使用 GitHub 登录</button>
            <button className="btn btn-ghost" disabled={busy} onClick={() => setUseDevice(!useDevice)} aria-expanded={useDevice}>
              <KeyRound size={15} />使用设备凭据
            </button>
            {useDevice && (
              <form onSubmit={device}>
                <label htmlFor="device-credential">设备凭据</label>
                <input id="device-credential" type="password" autoComplete="off" spellCheck={false} value={credential} onChange={(e) => setCredential(e.target.value)} required />
                <button className="btn" disabled={busy || !credential.trim()}>登录</button>
              </form>
            )}
          </>
        )}
        {error && <p className="form-error" role="alert">{error}</p>}
      </section>
    </main>
  );
}
