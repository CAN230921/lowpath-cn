import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Download, Loader2, Play, Route, Square } from "lucide-react";
import { api, describeError } from "../api";
import type { TraceHop, TraceMode, TraceResult, TraceStatus, TraceTarget } from "../types";
import { fmtMs, pct } from "../util";

// The route trace (MTR): every router between this PC and a target, with
// loss and delay at each, measured many times. Loss only counts when it
// carries on to the destination; routers that skip answering trace probes
// are marked, not blamed. Unknown numbers show "—" with the reason.

const POLL_MS = 1000;
const OTHER = "__other";

interface Props {
  onError: (msg: string, title?: string) => void;
}

export function RouteTrace({ onError }: Props) {
  const [st, setSt] = useState<TraceStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [targetId, setTargetId] = useState("");
  const [host, setHost] = useState("");
  const [mode, setMode] = useState<TraceMode>("icmp");
  const timer = useRef<number | undefined>(undefined);
  const alive = useRef(true);

  const poll = useCallback(async () => {
    window.clearTimeout(timer.current);
    try {
      const s = await api.trace();
      if (!alive.current) return;
      setSt(s);
      if (s.running) timer.current = window.setTimeout(poll, POLL_MS);
    } catch {
      if (alive.current) timer.current = window.setTimeout(poll, 3000);
    }
  }, []);

  useEffect(() => {
    alive.current = true;
    poll();
    return () => {
      alive.current = false;
      window.clearTimeout(timer.current);
    };
  }, [poll]);

  const targets = useMemo(() => st?.targets ?? [], [st?.targets]);
  // Default to the first relay (the route games take with Lowpath), else
  // whatever comes first.
  const chosen: TraceTarget | undefined = targets.find((t) => t.id === targetId) ?? (targetId === OTHER ? undefined : targets[0]);
  const selectValue = targetId === OTHER ? OTHER : (chosen?.id ?? "");
  const udpOk = !!chosen?.udp && targetId !== OTHER;
  const effMode: TraceMode = udpOk ? mode : "icmp";

  const start = async () => {
    const target = targetId === OTHER ? { host: host.trim() } : chosen ? { target_id: chosen.id } : null;
    if (!target) return;
    setBusy(true);
    try {
      setSt(await api.startTrace(target, effMode));
      timer.current = window.setTimeout(poll, POLL_MS);
    } catch (e) {
      const d = describeError(e, "无法启动路由追踪");
      onError(d.msg, d.title);
    } finally {
      setBusy(false);
    }
  };

  const stop = async () => {
    setBusy(true);
    try {
      setSt(await api.stopTrace());
    } catch (e) {
      onError(describeError(e, "无法停止路由追踪").msg);
    } finally {
      setBusy(false);
    }
  };

  const running = !!st?.running;
  const shown = running ? st?.progress : st?.result;
  const minutes = st ? duration(st.defaults.count * st.defaults.interval_s) : "";
  const canStart = !!st && (targetId === OTHER ? host.trim() !== "" : !!chosen);

  return (
    <section className="bloat trace" aria-label="路由追踪">
      <div className="bloat-head">
        <span className="bloat-ico">
          <Route size={18} />
        </span>
        <div className="txt">
          <div className="t">路由追踪（MTR）</div>
          <div className="d">
            逐跳测量从本机到目标的延迟和丢包，每跳发送 {st?.defaults.count ?? 200} 次探测。只有延续到终点的丢包才计入。通常需要 {minutes || "几分钟"}.
          </div>
        </div>
      </div>

      <div className="trace-controls">
        <label className="trace-field">
          <span className="section-label">目标</span>
          <select className="input" value={selectValue} disabled={running || !st} onChange={(e) => setTargetId(e.target.value)} aria-label="追踪目标">
            {targets.map((t) => (
              <option key={t.id} value={t.id}>
                {targetLabel(t)}
              </option>
            ))}
            <option value={OTHER}>其他主机或 IP…</option>
          </select>
        </label>
        {targetId === OTHER && (
          <label className="trace-field trace-host">
            <span className="section-label">主机</span>
            <input
              className="input num"
              placeholder="game.example.com or 203.0.113.10"
              value={host}
              disabled={running}
              spellCheck={false}
              autoComplete="off"
              onChange={(e) => setHost(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && canStart && !running && start()}
              aria-label="要追踪的主机"
            />
          </label>
        )}
        <div className="trace-field">
          <span className="section-label" id="trace-mode-label">
            探测协议
          </span>
          <div className="segmented" role="radiogroup" aria-labelledby="trace-mode-label">
            <button role="radio" aria-checked={effMode === "icmp"} disabled={running} onClick={() => setMode("icmp")}>
              ICMP
            </button>
            <button
              role="radio"
              aria-checked={effMode === "udp"}
              disabled={running || !udpOk}
              onClick={() => setMode("udp")}
              title={udpOk ? "通过中继实际使用的 UDP 端口测量" : "UDP 追踪需要选择一个中继"}
            >
              UDP
            </button>
          </div>
        </div>
        <div className="trace-go">
          {running ? (
            <button className="btn" onClick={stop} disabled={busy}>
              <Square size={14} /> 停止
            </button>
          ) : (
            <button className="btn btn-primary" onClick={start} disabled={busy || !canStart}>
              {busy ? <Loader2 size={14} className="spin" /> : <Play size={14} />} {st?.result ? "重新追踪" : "开始追踪"}
            </button>
          )}
        </div>
      </div>
      {effMode === "udp" && !running && (
        <div className="trace-hint faint">中间路由器用 ICMP 探测，末跳测量中继的实际 UDP 端口。</div>
      )}

      {running && shown && <Progress r={shown} />}
      {!running && st?.error && <div className="note-warn bloat-note">{st.error}</div>}
      {!running && shown && <Verdict r={shown} />}
      {shown && (shown.hops?.length ?? 0) + (shown.end ? 1 : 0) > 0 && <HopTable r={shown} />}
      {!running && shown && <Footer r={shown} />}
      {!running && !shown && st && !st.error && (
        <div className="bloat-empty muted">暂无追踪结果。可选择一个中继，查看沿途从哪一跳开始丢包。</div>
      )}
    </section>
  );
}

function targetLabel(t: TraceTarget): string {
  const kind = t.kind === "relay" ? "relay" : t.kind === "server" ? "上次的服务器" : "";
  return `${t.label} · ${t.addr}${kind && !t.label.toLowerCase().includes(kind) ? ` (${kind})` : ""}`;
}

function duration(s: number): string {
  if (!s || s <= 0) return "";
  if (s < 90) return `about ${Math.round(s)} seconds`;
  const m = Math.floor(s / 60);
  const r = Math.round(s - m * 60);
  return r >= 10 ? `about ${m} min ${r} s` : `about ${m} minutes`;
}

function Progress({ r }: { r: TraceResult }) {
  const frac = r.planned > 0 ? Math.min(1, r.rounds / r.planned) : 0;
  const hops = r.hops?.length ?? 0;
  return (
    <div className="trace-progress" role="status" aria-live="polite">
      <div className="bar">
        <div style={{ width: `${frac * 100}%` }} />
      </div>
      <span className="muted">
        {r.rounds === 0 ? (
          <>
            <Loader2 size={13} className="spin" /> 正在追踪 {r.host || r.target}…
          </>
        ) : (
          <>
            轮次 <b className="num">{r.rounds}</b> / {r.planned} · {hops} {hops === 1 ? "hop" : "hops"} 当前
          </>
        )}
      </span>
    </div>
  );
}

function tone(r: TraceResult): "good" | "warn" | "bad" | "none" {
  if (!r.reached) return "none";
  if (r.loss_from == null) return (r.dest_loss_pct ?? 0) > 0 ? "warn" : "good";
  return "bad";
}

function Verdict({ r }: { r: TraceResult }) {
  return (
    <div className={`trace-verdict t-${tone(r)}`}>
      <div className="t">{r.verdict || "本次未形成结论。"}</div>
      {r.notes && r.notes.length > 0 && (
        <ul>
          {r.notes.map((n, i) => (
            <li key={i}>{n}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

function HopTable({ r }: { r: TraceResult }) {
  const rows = [...(r.hops ?? []), ...(r.end ? [r.end] : [])];
  const noDest = !r.running && !r.reached;
  return (
    <div className="trace-table-wrap">
      <table className="trace-table">
        <thead>
          <tr>
            <th className="n">#</th>
            <th>主机</th>
            <th className="v" title="此跳未回复的探测比例，与 MTR 和 WinMTR 显示口径一致">
              丢包
            </th>
            <th className="v" title="从这一跳延续至终点的丢包率">
              实际丢包
            </th>
            <th className="v">平均</th>
            <th className="v">最低</th>
            <th className="v">最高</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((h) => (
            <HopRow key={`${h.probe}-${h.ttl}`} h={h} r={r} noDest={noDest} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function HopRow({ h, r, noDest }: { h: TraceHop; r: TraceResult; noDest: boolean }) {
  const udp = h.probe === "udp";
  // A hop that only answered "unreachable" (a firewall refusing the
  // probes) is known, but none of its answers count as replies.
  const refuses = h.recv === 0 && (h.unreachable ?? 0) > 0 && h.addr !== "";
  const silent = h.recv === 0 && !refuses;
  const lossBad = h.persist_pct != null && h.persist_pct >= r.threshold_pct;
  const host = udp ? "中继 UDP 端口" : h.name || h.addr;
  const local = h.addr.startsWith("127.") || h.addr === "::1";
  const where = h.name ? h.addr : local ? "本机" : h.private ? "私网地址" : "";
  const sub = udp ? (r.end_name ?? "") : refuses ? "拒绝探测（不可达）" : where;
  const persistWhy = noDest ? "目标未回复，无法确认丢包" : r.running ? "追踪结束后可见" : undefined;
  return (
    <tr className={`${h.loss_starts ? "starts" : ""}${silent ? " silent" : ""}`}>
      <td className="n num">{udp ? "UDP" : h.ttl}</td>
      <td className="host">
        {silent && !udp ? (
          <span className="faint" title={h.error || "此路由器未回复追踪探测，流量仍可能正常转发。"}>
            无回包
          </span>
        ) : (
          <>
            <span className="addr" title={h.others?.length ? `其他应答地址：${h.others.join("、")}` : undefined}>
              {host}
              {h.others && h.others.length > 0 && <span className="faint"> +{h.others.length}</span>}
            </span>
            {sub && <span className="sub faint">{sub}</span>}
          </>
        )}
        {h.loss_starts && <span className="tag tag-bad">开始丢包</span>}
        {h.limited && (
          <span className="tag" title="该路由器限制探测回包，后续路由仍收到探测。">
            回包限速
          </span>
        )}
      </td>
      <td className={`v num${h.limited ? " faint" : h.loss_pct > 0 && !silent ? " q-warn" : ""}`}>
        {h.sent > 0 ? `${pct(h.loss_pct)}%` : "—"}
      </td>
      <td className={`v num${lossBad ? " q-bad" : ""}`} title={h.persist_pct == null ? persistWhy : undefined}>
        {h.persist_pct == null ? "—" : `${pct(h.persist_pct)}%`}
      </td>
      <td className="v num">{fmtMs(h.avg_ms)}</td>
      <td className="v num faint">{fmtMs(h.best_ms)}</td>
      <td className="v num faint">{fmtMs(h.worst_ms)}</td>
    </tr>
  );
}

function Footer({ r }: { r: TraceResult }) {
  const when = new Date(r.finished_at);
  const how = r.mode === "udp" ? "中间跳 ICMP，末跳 UDP" : "ICMP";
  return (
    <div className="trace-foot">
      <span className="faint">
        {r.host && r.host !== r.target ? `${r.host} (${r.target})` : r.target} · {how} · {r.rounds} 轮
        {isNaN(when.getTime()) ? "" : ` · ${when.toLocaleString()}`}
      </span>
      <a className="btn btn-sm" href="/api/diag/trace/report" download="lowpath-route-trace.txt">
        <Download size={14} /> 导出报告
      </a>
    </div>
  );
}
