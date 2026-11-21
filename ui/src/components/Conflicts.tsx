import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, Info, Layers, Loader2, RefreshCw, ShieldAlert, X } from "lucide-react";
import { api } from "../api";
import type { Conflict, ConflictReport } from "../types";

// What the conflict is, as the start of a sentence. The service sends the
// evidence, the action and the explanation; this only picks the verb.
export function headline(c: Conflict): string {
  if (c.level === "info") return `已安装 ${c.name}。`;
  switch (c.kind) {
    case "vpn":
      return `${c.name} 正在接管全部互联网流量。`;
    case "packet_filter":
      return c.id === "windivert" ? "另一个程序已加载 WinDivert。" : `已加载 ${c.name}。`;
    case "booster":
      return c.level === "block" ? `${c.name} 正在运行。` : `${c.name} 的后台服务正在运行。`;
    default:
      return `${c.name} 正在运行。`;
  }
}

function found(c: Conflict): string {
  const f = c.found ?? [];
  return f.length ? `发现：${f.join("、")}。` : "";
}

// One conflict: what it is, what to do, and a one-click "Why?".
function ConflictRow({ c, compact }: { c: Conflict; compact?: boolean }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="conflict" data-level={c.level}>
      <span>
        <b>{headline(c)}</b> {!compact && c.action}{" "}
        <button className="link" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
          {open ? "隐藏" : "原因"}
        </button>
      </span>
      {open && (
        <span className="banner-detail conflict-why">
          {compact && <>{c.action} </>}
          {c.why} {found(c)}
        </span>
      )}
    </div>
  );
}

const HIDDEN_KEY = "lowpath.conflicts.hidden";

function readHidden(): string {
  try {
    return localStorage.getItem(HIDDEN_KEY) ?? "";
  } catch {
    return "";
  }
}

// ConflictBanner sits above every screen: running conflicts as a warning
// (red when route mode won't start next to them), installed-but-idle ones
// as a quiet note the player can dismiss until the list changes.
export function ConflictBanner({ report }: { report: ConflictReport }) {
  const all = report.conflicts ?? [];
  const active = all.filter((c) => c.level !== "info");
  const idle = all.filter((c) => c.level === "info");
  const idleKey = idle
    .map((c) => c.id)
    .sort()
    .join(",");
  const [hidden, setHidden] = useState(readHidden);

  const dismiss = () => {
    setHidden(idleKey);
    try {
      localStorage.setItem(HIDDEN_KEY, idleKey);
    } catch {
      // Only a convenience: the note hides for this visit anyway.
    }
  };

  if (active.length === 0 && (idle.length === 0 || hidden === idleKey)) return null;
  const blocking = active.some((c) => c.level === "block");
  return (
    <div className="conflicts">
      {active.length > 0 && (
        <div className={`banner ${blocking ? "banner-bad" : "banner-warn"}`} role="alert" aria-label="其他网络软件">
          <ShieldAlert size={16} style={{ flex: "none", alignSelf: "flex-start", marginTop: 1 }} />
          <div className="conflict-list">
            {active.map((c) => (
              <ConflictRow key={c.id} c={c} />
            ))}
          </div>
        </div>
      )}
      {idle.length > 0 && hidden !== idleKey && (
        <div className="banner banner-info" aria-label="已安装的网络软件">
          <Info size={16} style={{ flex: "none", alignSelf: "flex-start", marginTop: 1 }} />
          <div className="conflict-list">
            {idle.map((c) => (
              <ConflictRow key={c.id} c={c} compact />
            ))}
            <span className="banner-detail">
              {idle.length > 1 ? "可以保留安装，使用 Lowpath 时请关闭它们" : "可以保留安装，使用 Lowpath 时请关闭该程序"} 。
            </span>
          </div>
          <button className="btn btn-ghost icon-btn" onClick={dismiss} aria-label="关闭提示" title="关闭提示" style={{ flex: "none", width: 24, height: 24, alignSelf: "flex-start" }}>
            <X size={14} />
          </button>
        </div>
      )}
    </div>
  );
}

const LEVEL_TAG: Record<Conflict["level"], string> = { block: "请先关闭", warn: "检查", info: "已安装" };

// OtherSoftware is the Settings → Network row: everything the check found,
// what it couldn't look at, and a "Check again" button.
export function OtherSoftware() {
  const [report, setReport] = useState<ConflictReport | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const alive = useRef(true);

  const load = useCallback(async (fresh: boolean) => {
    setBusy(true);
    setErr("");
    try {
      const r = await api.conflicts(fresh);
      if (alive.current) setReport(r);
    } catch (e) {
      if (alive.current) setErr(e instanceof Error && e.message ? e.message : "检查失败。");
    } finally {
      if (alive.current) setBusy(false);
    }
  }, []);

  useEffect(() => {
    alive.current = true;
    load(false);
    return () => {
      alive.current = false;
    };
  }, [load]);

  const list = report?.conflicts ?? [];
  const unchecked = report?.unchecked ?? [];
  const at = report?.at ? new Date(report.at) : null;
  const status = !report ? (busy ? "正在检查…" : "—") : list.length === 0 ? (unchecked.length ? "已检查范围内未发现冲突" : "未发现") : `${list.length} found`;

  return (
    <div className="setting other-software" aria-busy={busy} data-testid="other-software">
      <span className="muted">
        <Layers size={18} />
      </span>
      <div className="txt">
        <div className="t">其他网络软件</div>
        <div className="conn-headline">{status}</div>
        {list.length > 0 && (
          <ul className="software-list">
            {list.map((c) => (
              <li key={c.id}>
                <span className={`tag software-tag software-${c.level}`}>{LEVEL_TAG[c.level]}</span>
                <ConflictRow c={c} compact />
              </li>
            ))}
          </ul>
        )}
        {(err || unchecked.length > 0) && (
          <div className="note-warn">
            <AlertTriangle size={14} />
            <span>{err || `无法检查：${unchecked.join("、")}。`}</span>
          </div>
        )}
        <div className="d">
          从本机进程、服务、已安装应用和网卡中检查其他加速器、封包过滤驱动及全局 VPN。检查数据留在本机。
          {at && !Number.isNaN(at.getTime()) && <> 检查时间 {at.toLocaleTimeString()}.</>}
        </div>
      </div>
      <button className="btn" onClick={() => load(true)} disabled={busy} title="重新检查其他网络软件">
        {busy ? <Loader2 size={15} className="spin" /> : <RefreshCw size={15} />} 重新检查
      </button>
    </div>
  );
}
