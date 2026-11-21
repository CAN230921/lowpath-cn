import { useCallback, useEffect, useRef, useState } from "react";
import { Globe, Monitor, Router, Server, Waypoints } from "lucide-react";
import { api } from "../api";
import type { GatewaySecond, GatewayStats, GatewayView, RouteSegment, SegmentId } from "../types";
import { fmtMs, pct } from "../util";
import { BloatTest } from "./BloatTest";
import { RouteTrace } from "./RouteTrace";
import { Verdict } from "./Verdict";
import { WiFiCard } from "./WiFiCard";
import "../diag.css";

// Settings → Diagnostics: the verdict with the route split into Home / ISP
// / Lowpath / Game server, the live router pings, and the on-demand tests.
// Nothing here invents a number: a part nobody measured shows "—" and says
// what would measure it.

export function Diagnostics({ onError, wifiTuning }: { onError: (msg: string, title?: string) => void; wifiTuning: boolean }) {
  return (
    <>
      <h3>诊断</h3>
      <div className="muted" style={{ fontSize: 12.5, marginBottom: 6 }}>
        可分别在 Wi-Fi 和有线网络下打开此页，比较本机到路由器的连接质量。
      </div>
      <Verdict renderSegments={(segs) => <RouteSegments segments={segs} />} />
      <GatewayCard />
      <WiFiCard tuningEnabled={wifiTuning} />
      <BloatTest onError={onError} />
      <RouteTrace onError={onError} />
    </>
  );
}

// --- The route, part by part ------------------------------------------------

const SEGMENT_ICON: Record<SegmentId, typeof Router> = { home: Router, isp: Globe, lowpath: Waypoints, server: Server };

const SEGMENT_STATE: Record<RouteSegment["status"], string> = {
  good: "正常",
  warn: "留意",
  bad: "异常",
  unknown: "未测量",
  off: "未启用",
};

// RouteSegments is the bar under the verdict: this PC, then each part of
// the way to the game server with its own numbers.
export function RouteSegments({ segments }: { segments: RouteSegment[] }) {
  return (
    <div className="segs" aria-label="分段线路">
      <div className="segs-end" aria-hidden>
        <Monitor size={16} />
        <span>本机</span>
      </div>
      <ol className="segs-list">
        {segments.map((s) => {
          const Icon = SEGMENT_ICON[s.id] ?? Globe;
          const nums = s.rtt_ms != null || s.loss_pct != null;
          return (
            <li key={s.id} className={`seg s-${s.status}${s.blamed ? " blamed" : ""}`} data-segment={s.id} data-status={s.status}>
              <div className="seg-bar" aria-hidden />
              <div className="seg-head">
                <Icon size={14} aria-hidden />
                <span className="name">{s.name}</span>
                <span className="state">{s.blamed ? "异常位置" : SEGMENT_STATE[s.status]}</span>
              </div>
              <div className="seg-nums num">
                {nums ? (
                  <>
                    <span>
                      {fmtMs(s.rtt_ms)}
                      {s.rtt_ms != null && <small> ms</small>}
                    </span>
                    <span>
                      {s.loss_pct == null ? "—" : pct(s.loss_pct)}
                      {s.loss_pct != null && <small>% 丢包</small>}
                    </span>
                  </>
                ) : (
                  <span>—</span>
                )}
              </div>
              <div className="seg-sum">{s.summary}</div>
              {s.source && <div className="seg-src faint">{s.source}</div>}
            </li>
          );
        })}
      </ol>
    </div>
  );
}

// --- Live router pings -------------------------------------------------------

// The pinger keeps going while this polls (it stops 15 s after the last
// poll), so the card polls once a second while it is on screen.
const GATEWAY_POLL_MS = 1000;

function linkName(v: GatewayView): string {
  const kind = v.link === "wifi" ? "Wi-Fi" : v.link === "ethernet" ? "有线网络" : v.link === "other" ? "VPN 或其他连接" : "";
  const parts = [kind, v.interface].filter(Boolean) as string[];
  if (parts.length === 2 && parts[0].toLowerCase() === parts[1].toLowerCase()) parts.pop();
  return parts.join(" · ") || "—";
}

function routerTone(st: GatewayStats): "good" | "warn" | "bad" {
  // The verdict's thresholds: more than 0.5% lost or a p99 above 15 ms.
  if ((!st.limited && st.loss_pct > 0.5 && st.lost >= 2) || (st.p99_ms ?? st.p95_ms ?? 0) > 15) return "bad";
  if (st.spikes > 0 || (!st.limited && st.lost > 0)) return "warn";
  return "good";
}

export function GatewayCard() {
  const [v, setV] = useState<GatewayView | null>(null);
  const [failed, setFailed] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const alive = useRef(true);

  const poll = useCallback(async () => {
    window.clearTimeout(timer.current);
    try {
      const g = await api.gateway();
      if (!alive.current) return;
      setV(g);
      setFailed(false);
    } catch {
      if (alive.current) setFailed(true);
    }
    if (alive.current) timer.current = window.setTimeout(poll, GATEWAY_POLL_MS);
  }, []);

  useEffect(() => {
    alive.current = true;
    poll();
    return () => {
      alive.current = false;
      window.clearTimeout(timer.current);
    };
  }, [poll]);

  const st = v?.state === "on" ? v.stats : null;
  const tone = st ? routerTone(st) : "none";
  const rate = v?.running && v.rate_hz > 0 ? `每秒 ${Math.round(v.rate_hz)} 次` : "";

  return (
    <section className="bloat gw" aria-label="路由器延迟">
      <div className="bloat-head">
        <span className="bloat-ico">
          <Router size={18} />
        </span>
        <div className="txt">
          <div className="t">路由器延迟</div>
          <div className="d">
            打开此页或启用加速时，Lowpath 每秒最多探测路由器 20 次。此处的丢包和高延迟会影响所有游戏。
          </div>
        </div>
        <span className={`tag gw-live${v?.running ? " on" : ""}`} role="status">
          {v?.running ? <span className="dot" aria-hidden /> : null}
          {v?.running ? `实时 · ${rate}` : "已停止"}
        </span>
      </div>

      {!v && !failed && <div className="bloat-empty muted">正在获取 Lowpath 服务数据…</div>}
      {failed && <div className="note-warn bloat-note">— Lowpath 服务暂未响应。数据可能已过期，系统会重试。</div>}

      {v && !st && v.message && (
        <div className={`gw-msg ${v.state === "measuring" ? "muted" : "note-warn"}`} data-state={v.state}>
          {v.state === "measuring" ? v.message : `— ${v.message}`}
        </div>
      )}

      {v && st && (
        <>
          <div className={`gw-stats t-${tone}`} data-state={v.state}>
            <Stat k="中位数" v={fmtMs(st.p50_ms)} unit="ms" s={`最快 ${fmtMs(st.min_ms)} ms`} />
            <Stat
              k={st.p99_ms != null ? "p99" : "p95"}
              v={fmtMs(st.p99_ms ?? st.p95_ms)}
              unit="ms"
              s={st.p99_ms != null ? `99% of replies; slowest ${fmtMs(st.max_ms)} ms` : "95% of replies; p99 after 100"}
            />
            <Stat
              k="超时"
              v={st.limited ? "—" : pct(st.loss_pct)}
              unit="%"
              s={st.limited ? "路由器限制探测回包" : `${st.lost}/${st.sent}${st.max_loss_run > 1 ? `，最多连续 ${st.max_loss_run} 次` : ""}`}
            />
            <Stat k="延迟尖峰" v={String(st.spikes)} unit="" s="延迟达到 100 ms 的回包" />
            <Stat k="抖动" v={fmtMs(st.jitter_ms)} unit="ms" s="相邻回包的延迟变化" />
          </div>
          {v.message && (
            <div className="gw-msg note-warn" data-state={v.state}>
              {v.message}
            </div>
          )}
          <GatewayChart series={v.series ?? []} />
        </>
      )}

      {v && v.note && <div className="gw-note muted">{v.note}</div>}

      {v && (v.gateway || v.interface || v.link) && (
        <dl className="bloat-facts">
          <dt>路由器</dt>
          <dd className="num">{v.gateway || "—"}</dd>
          <dt>连接</dt>
          <dd>{linkName(v)}</dd>
          {st && (
            <>
              <dt>统计范围</dt>
              <dd>最近 {Math.max(1, Math.round(st.window_s))} s</dd>
            </>
          )}
        </dl>
      )}
    </section>
  );
}

function Stat({ k, v, unit, s }: { k: string; v: string; unit: string; s: string }) {
  return (
    <div className="stat">
      <div className="k">{k}</div>
      <div className="v num">
        {v}
        {v !== "—" && unit && <small>{unit}</small>}
      </div>
      <div className="s">{s}</div>
    </div>
  );
}

// The chart's scale: 0 to 100 ms on a square-root axis, so 2 ms and 15 ms
// are both visible next to a 100 ms spike; taller bars are clipped and
// marked.
const CHART_MAX_MS = 100;
const CHART_W = 600;
const CHART_H = 72;
const CHART_SECONDS = 60;

function y(msv: number): number {
  const f = Math.sqrt(Math.min(msv, CHART_MAX_MS) / CHART_MAX_MS);
  return CHART_H - Math.max(1.5, f * (CHART_H - 4));
}

// GatewayChart: one bar per second for the last minute up to the slowest
// reply, a tick at the median; a red cap marks a second with lost pings.
function GatewayChart({ series }: { series: GatewaySecond[] }) {
  if (series.length === 0) return null;
  const last = series[series.length - 1].t;
  const slot = CHART_W / CHART_SECONDS;
  const bars = series.filter((s) => last - s.t < CHART_SECONDS);
  const lostSecs = bars.filter((s) => s.lost > 0).length;
  return (
    <figure className="gw-chart">
      <svg viewBox={`0 0 ${CHART_W} ${CHART_H}`} preserveAspectRatio="none" role="img" aria-label={`最近一分钟的路由器探测，其中 ${lostSecs} 秒出现超时`}>
        {[15, CHART_MAX_MS].map((g) => (
          <line key={g} x1={0} x2={CHART_W} y1={y(g)} y2={y(g)} className="grid" />
        ))}
        {bars.map((s) => {
          const x = (CHART_SECONDS - 1 - (last - s.t)) * slot;
          const w = Math.max(1, slot - 2);
          const lo = s.p50_ms != null ? y(s.p50_ms) : CHART_H;
          const hi = s.max_ms != null ? y(s.max_ms) : CHART_H;
          const spike = (s.max_ms ?? 0) >= CHART_MAX_MS;
          return (
            <g key={s.t}>
              {s.max_ms != null && <rect x={x} y={hi} width={w} height={Math.max(1.5, CHART_H - hi)} className={spike ? "bar spike" : "bar"} />}
              {s.p50_ms != null && <rect x={x} y={lo - 1} width={w} height={2} className="mid" />}
              {s.lost > 0 && <rect x={x} y={0} width={w} height={5} className="lost" />}
            </g>
          );
        })}
      </svg>
      <figcaption className="faint">
        <span>1 min ago</span>
        <span>
          柱形：每秒最高延迟。刻线：中位数。参考线：15 和 100 ms。 <span className="lost-key">红色</span>：探测超时
        </span>
        <span>现在</span>
      </figcaption>
    </figure>
  );
}
