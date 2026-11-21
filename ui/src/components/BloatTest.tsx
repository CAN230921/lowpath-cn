import { useCallback, useEffect, useRef, useState } from "react";
import { Gauge, Loader2, Play, Square } from "lucide-react";
import { api, describeError } from "../api";
import type { BloatPhase, BloatResult, BloatStatus, Grade, LatencyStats } from "../types";
import { fmtMbps, fmtMs } from "../util";
import "../diag.css";

// The active bufferbloat test: 20 s of idle / download / upload / recovery
// with latency probes throughout, graded A+ to F by how much the ping rises
// while the line is full. Unknown numbers show "—", never a guess.

const PHASES: { id: Exclude<BloatPhase, "picking">; label: string }[] = [
  { id: "idle", label: "空闲" },
  { id: "download", label: "下载" },
  { id: "upload", label: "上传" },
  { id: "recovery", label: "恢复" },
];

const POLL_MS = 400;

const plus = (v: number | null | undefined) => (v == null ? "—" : `+${fmtMs(v)}`);

function gradeTone(g: Grade): "good" | "warn" | "bad" | "none" {
  if (g === "A+" || g === "A") return "good";
  if (g === "B") return "warn";
  if (g === "") return "none";
  return "bad";
}

function verdict(r: BloatResult): string {
  switch (r.grade) {
    case "A+":
    case "A":
      return "带宽占满时延迟仍然稳定，下载对游戏的影响很小。";
    case "B":
      return "带宽占满时延迟略有升高，对局期间的大文件下载可能造成卡顿。";
    case "C":
    case "D":
    case "F":
      return "带宽占满时延迟明显升高。游戏时可暂停下载和串流，或在路由器启用 SQM（fq_codel 或 CAKE）。";
    default:
      return "本次未获得评级。";
  }
}

interface Props {
  onError: (msg: string, title?: string) => void;
}

export function BloatTest({ onError }: Props) {
  const [st, setSt] = useState<BloatStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const alive = useRef(true);

  const poll = useCallback(async () => {
    window.clearTimeout(timer.current);
    try {
      const s = await api.bloat();
      if (!alive.current) return;
      setSt(s);
      if (s.running) timer.current = window.setTimeout(poll, POLL_MS);
    } catch {
      // The live stream already shows "lost connection"; try again later.
      if (alive.current) timer.current = window.setTimeout(poll, 2000);
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

  const start = async () => {
    setBusy(true);
    try {
      setSt(await api.startBloat());
      timer.current = window.setTimeout(poll, POLL_MS);
    } catch (e) {
      const d = describeError(e, "无法启动缓冲膨胀测试");
      onError(d.msg, d.title);
    } finally {
      setBusy(false);
    }
  };

  const stop = async () => {
    setBusy(true);
    try {
      setSt(await api.stopBloat());
    } catch (e) {
      onError(describeError(e, "无法停止测试").msg);
    } finally {
      setBusy(false);
    }
  };

  const running = !!st?.running;
  const r = st?.result ?? null;

  return (
    <section className="bloat" aria-label="缓冲膨胀测试">
      <div className="bloat-head">
        <span className="bloat-ico">
          <Gauge size={18} />
        </span>
        <div className="txt">
          <div className="t">缓冲膨胀测试</div>
          <div className="d">
            先下载再上传，在约 20 秒内占满带宽并测量延迟增幅。预计使用约 50 MB 至 1 GB 流量，取决于线路速度。
          </div>
        </div>
        {running ? (
          <button className="btn" onClick={stop} disabled={busy}>
            <Square size={14} /> 停止
          </button>
        ) : (
          <button className="btn btn-primary" onClick={start} disabled={busy || !st}>
            {busy ? <Loader2 size={14} className="spin" /> : <Play size={14} />} {r ? "重新测试" : "开始测试"}
          </button>
        )}
      </div>

      {running && st?.progress && <Running p={st.progress} />}
      {!running && st?.error && <div className="note-warn bloat-note">{st.error}</div>}
      {!running && r && <Result r={r} />}
      {!running && !r && st && !st.error && <div className="bloat-empty muted">尚未测试。请先暂停其他下载。</div>}
    </section>
  );
}

function Running({ p }: { p: NonNullable<BloatStatus["progress"]> }) {
  const idx = PHASES.findIndex((x) => x.id === p.phase);
  const per = p.total_s / PHASES.length || 5;
  return (
    <div className="bloat-run" role="status" aria-live="polite">
      <div className="bloat-phases">
        {PHASES.map((ph, i) => {
          const fill = i < idx ? 1 : i === idx ? Math.min(1, Math.max(0, (p.elapsed_s - i * per) / per)) : 0;
          return (
            <div key={ph.id} className={`bloat-phase${i === idx ? " on" : ""}`}>
              <div className="bar">
                <div style={{ width: `${fill * 100}%` }} />
              </div>
              <span>{ph.label}</span>
            </div>
          );
        })}
      </div>
      <div className="bloat-live">
        {p.phase === "picking" ? (
          <span className="muted">
            <Loader2 size={13} className="spin" /> 正在寻找探测目标…
          </span>
        ) : (
          <>
            <span>
              当前延迟 <b className="num">{fmtMs(p.last_ms)}</b> ms
            </span>
            {(p.phase === "download" || p.phase === "upload") && (
              <span>
                {p.phase === "download" ? "下载中" : "上传中"} <b className="num">{fmtMbps(p.load_mbps)}</b> Mbit/s
              </span>
            )}
            {p.target && <span className="faint bloat-target">切换至 {p.target}</span>}
          </>
        )}
      </div>
    </div>
  );
}

function Result({ r }: { r: BloatResult }) {
  const tone = gradeTone(r.grade);
  const gw = (phase: string) => r.phases?.find((p) => p.phase === phase)?.gateway ?? null;
  const gwIdle = gw("idle");
  const gwRise = (phase: string) => rise(gw(phase), gwIdle);
  const when = new Date(r.finished_at);
  return (
    <div className="bloat-result">
      <div className="bloat-verdict">
        <div className={`bloat-grade g-${tone}`} aria-label={r.grade ? `评级 ${r.grade}` : "暂无评级"}>
          {r.grade || "—"}
        </div>
        <div>
          <div className="t">{verdict(r)}</div>
          {r.note && <div className="d">{r.note}</div>}
        </div>
      </div>

      <div className="bloat-stats">
        <Stat k="空载延迟" v={fmtMs(r.idle_ms)} unit="ms" s="带宽占用前" />
        <Stat k="下载" v={plus(r.down_increase_ms)} unit="ms" s={`速率 ${fmtMbps(r.down_mbps)} Mbit/s`} />
        <Stat k="上传" v={plus(r.up_increase_ms)} unit="ms" s={`速率 ${fmtMbps(r.up_mbps)} Mbit/s`} />
      </div>

      <dl className="bloat-facts">
        <dt>探测目标</dt>
        <dd>{r.target || "—"}</dd>
        <dt>家庭网络</dt>
        <dd>
          {gwIdle?.p50_ms != null ? (
            <>
              路由器 {fmtMs(gwIdle.p50_ms)} ms（空载）， {plus(gwRise("download"))} ms（下载）， {plus(gwRise("upload"))} ms（上传）
              <span className="faint"> · {r.gateway}</span>
            </>
          ) : r.gateway ? (
            "—（路由器未回复）"
          ) : (
            "—（找不到当前连接的路由器地址）"
          )}
        </dd>
        <dt>连接</dt>
        <dd>{connection(r)}</dd>
        <dt>使用流量</dt>
        <dd>{Math.round(r.data_mb)} MB</dd>
        <dt>测试时间</dt>
        <dd>{isNaN(when.getTime()) ? "—" : when.toLocaleString()}</dd>
      </dl>
    </div>
  );
}

function rise(p: LatencyStats | null, idle: LatencyStats | null): number | null {
  if (!p || !idle || p.p50_ms == null || idle.p50_ms == null) return null;
  return Math.max(0, p.p50_ms - idle.p50_ms);
}

// "Wi-Fi · WLAN 2", or just "Ethernet" when the adapter has that name.
function connection(r: BloatResult) {
  const kind = r.link === "wifi" ? "Wi-Fi" : r.link === "ethernet" ? "有线网络" : "";
  const parts = [kind, r.interface].filter(Boolean);
  if (parts.length === 2 && parts[0].toLowerCase() === parts[1].toLowerCase()) parts.pop();
  return parts.join(" · ") || "—";
}

function Stat({ k, v, unit, s }: { k: string; v: string; unit: string; s: string }) {
  return (
    <div className="stat">
      <div className="k">{k}</div>
      <div className="v num">
        {v}
        {v !== "—" && <small>{unit}</small>}
      </div>
      <div className="s">{s}</div>
    </div>
  );
}
