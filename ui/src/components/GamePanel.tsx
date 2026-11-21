import { useCallback, useEffect, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { ChevronDown, Info, Loader2, Plus, Power, RotateCw, ShieldAlert, Wifi, Zap } from "lucide-react";
import { api, ApiError, describeError } from "../api";
import type { Analysis, Conflict, Game, Live, RelayView, Route, RouteMode, Settings, Stats } from "../types";
import { ms, pct, quality, score } from "../util";
import { StatusPill } from "./Bits";
import { ArtImg, GameIcon, hasArt } from "./GameArt";
import { LiveGraph, type Point } from "./LiveGraph";
import { RouteMap } from "./RouteMap";
import { TrafficCard } from "./Traffic";
import { CurrentServer } from "./CurrentServer";
import { ValveServers } from "./ValveServers";

interface Props {
  game: Game;
  live: Live;
  settings: Settings;
  relays: RelayView[];
  demo: boolean;
  managed?: boolean;
  history: Point[];
  now: number;
  otherGameName: string | null;
  onError: (msg: string, title?: string) => void;
  onOpenRelays: () => void;
  onEnableLoopback: () => Promise<void>;
}

const DUP_LABEL: Record<number, string> = { 0: "自动", 1: "关闭", 2: "均衡", 4: "最大" };

// The server picker's value: "smart", "region:<continent>" or "route:<id>".
type Choice = string;

function parseChoice(c: Choice): { mode: RouteMode; region?: string; routeId?: string } {
  if (c.startsWith("region:")) return { mode: "region", region: c.slice(7) };
  if (c.startsWith("route:")) return { mode: "specific", routeId: c.slice(6) };
  return { mode: "smart" };
}

function choiceOf(mode: RouteMode, region?: string, routeId?: string): Choice {
  if (mode === "region") return `region:${region ?? ""}`;
  if (mode === "specific") return `route:${routeId ?? ""}`;
  return "smart";
}

// Jitter in words: what a player actually notices.
function stability(s?: Stats): { word: string; q: "good" | "warn" | "bad" } | null {
  if (!s?.up) return null;
  if (s.jitter_ms < 2 && s.loss_pct < 0.5) return { word: "非常稳定", q: "good" };
  if (s.jitter_ms < 5 && s.loss_pct < 1) return { word: "稳定", q: "good" };
  if (s.jitter_ms < 12 && s.loss_pct < 3) return { word: "略有波动", q: "warn" };
  return { word: "不稳定", q: "bad" };
}

const DETAILS_KEY = "lowpath.details";

function readDetailsOpen(): boolean {
  try {
    return localStorage.getItem(DETAILS_KEY) === "1";
  } catch {
    return false;
  }
}

export function GamePanel(props: Props) {
  const { game, live, settings, relays, demo, managed, history, now, otherGameName, onError, onOpenRelays, onEnableLoopback } = props;
  const [routes, setRoutes] = useState<Route[]>([]);
  const [analysis, setAnalysis] = useState<Analysis | null>(null);
  const [analyzing, setAnalyzing] = useState(false);
  const [busy, setBusy] = useState<"apply" | "stop" | null>(null);
  const [choice, setChoice] = useState<Choice>("smart");
  const [details, setDetails] = useState(readDetailsOpen);
  // Boost was refused because another program captures game traffic.
  const [refused, setRefused] = useState<{ message: string; conflicts: Conflict[] } | null>(null);

  const sess = live.session?.game_id === game.id ? live.session : null;
  const snap = sess ? live.snapshot : null;

  // Real relays are needed outside the demo; without them BOOST can only fail.
  const needsRelays = !demo && relays.length === 0 && !sess;
  const loopbackOnly = !demo && relays.length > 0 && relays.every((r) => r.builtin);
  const relayKey = relays.map((r) => r.id).join(",");
  const captureMode = settings.capture_mode ?? "proxy";
  const routeCapture = !!snap && (snap.mode === "capture" || sess?.capture_mode === "route");
  const servers = snap?.servers ?? [];
  const waiting = routeCapture && servers.length === 0;
  const processes = game.processes ?? [];
  const picked = parseChoice(choice);
  // Valve games: their relay POPs are known before the game starts.
  const sdr = !!game.discovery?.includes("sdr");

  const analyze = useCallback(async () => {
    setAnalyzing(true);
    try {
      setAnalysis(await api.analyze(game.id));
    } catch (e) {
      const d = describeError(e, "无法测量线路");
      onError(d.msg, d.title);
    } finally {
      setAnalyzing(false);
    }
  }, [game.id, onError]);

  // Load the game's routes (again when the relay list changes) and measure
  // them right away, so the numbers are there before the player asks.
  useEffect(() => {
    let cancelled = false;
    setRoutes([]);
    setAnalysis(null);
    api
      .routes(game.id)
      .then((r) => {
        if (cancelled) return;
        const list = r.routes ?? [];
        setRoutes(list);
        if (live.session?.game_id !== game.id) setChoice(game.smart ? "smart" : `region:${list[0]?.continent ?? ""}`);
        // One taken during the bufferbloat test is measured again.
        const fresh = r.analysis && !r.analysis.loaded && Date.now() - new Date(r.analysis.at).getTime() < 60_000;
        if (fresh) setAnalysis(r.analysis!);
        else if (settings.auto_analyze !== false && game.routable && list.length > 0 && live.session?.game_id !== game.id) analyze();
      })
      .catch(() => !cancelled && setRoutes([]));
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [game.id, relayKey]);

  // Coming back to the optimized game shows the route it is really using.
  useEffect(() => {
    if (sess) setChoice(choiceOf(sess.mode, sess.region, sess.route_id));
  }, [sess?.started_at]); // eslint-disable-line react-hooks/exhaustive-deps

  const continents = useMemo(() => [...new Set(routes.map((r) => r.continent).filter(Boolean))], [routes]);
  // Region picks are offered when they mean something (several continents),
  // or when they are what the game or the running session uses.
  const regionOpts = continents.length > 1 ? continents : picked.mode === "region" ? [picked.region ?? ""] : [];

  // Per-route numbers: live while optimizing this game, else last analysis.
  const statsFor = useCallback(
    (id: string): (Stats & { active?: boolean }) | undefined => {
      if (snap) return (snap.paths ?? []).find((p) => p.id === id);
      return analysis?.paths?.[id];
    },
    [snap, analysis],
  );

  // Routes the current pick may use, best first by the last measurement.
  const eligible = useMemo(() => {
    let list = routes;
    if (picked.mode === "region") list = routes.filter((r) => r.continent === picked.region);
    if (picked.mode === "specific") list = routes.filter((r) => r.id === picked.routeId);
    if (!analysis) return list;
    return [...list].sort((a, b) => score(analysis.paths?.[a.id]) - score(analysis.paths?.[b.id]));
  }, [routes, picked.mode, picked.region, picked.routeId, analysis]);

  const best = eligible.find((r) => statsFor(r.id)?.up) ?? eligible[0];
  const bestStats = best ? statsFor(best.id) : undefined;

  const apply = async (c: Choice = choice, ignoreConflicts = false) => {
    const p = parseChoice(c);
    setBusy("apply");
    try {
      await api.apply(game.id, p.mode, p.region, p.routeId, ignoreConflicts);
      setRefused(null);
    } catch (e) {
      if (e instanceof ApiError && e.kind === "conflict") {
        // Said next to the button, where the player looks, not in a toast.
        setRefused({ message: e.message, conflicts: e.conflicts });
        return;
      }
      setRefused(null);
      const d = describeError(e, "无法开始加速");
      onError(d.msg, d.title);
    } finally {
      setBusy(null);
    }
  };

  // The refusal goes once the program it named is closed.
  const blockingIds = (live.conflicts?.conflicts ?? [])
    .filter((c) => c.level === "block")
    .map((c) => c.id)
    .join(",");
  useEffect(() => {
    if (refused && !refused.conflicts.some((c) => blockingIds.split(",").includes(c.id))) setRefused(null);
  }, [blockingIds]); // eslint-disable-line react-hooks/exhaustive-deps

  const stop = async () => {
    setBusy("stop");
    try {
      await api.stop();
    } catch (e) {
      onError(describeError(e, "无法停止加速").msg);
    } finally {
      setBusy(null);
    }
  };

  // Changing the server while boosting switches over at once: no second
  // button to find.
  const pick = (c: Choice) => {
    setChoice(c);
    if (sess && c !== choiceOf(sess.mode, sess.region, sess.route_id)) apply(c);
  };

  const toggleDetails = () => {
    setDetails((d) => {
      try {
        localStorage.setItem(DETAILS_KEY, d ? "0" : "1");
      } catch {
        // Only a convenience; the section still toggles.
      }
      return !d;
    });
  };

  // ---- numbers ---------------------------------------------------------

  const before = snap ? snap.before : analysis?.direct;
  const after = snap ? snap.after : bestStats;
  // "Before" is only a number when the game server answered our pings; a
  // missing reply must not turn into an invented baseline.
  const beforeKnown = snap ? (snap.before_known ?? snap.before?.up) : analysis ? (analysis.direct_known ?? analysis.direct?.up) : false;
  const beforeShown = before?.up && beforeKnown ? before : undefined;
  // Without the relay → server leg, "after" stops at the relay and isn't
  // comparable with a ping to the server.
  const afterToRelay = !demo && !!after?.up && (snap ? !snap.server_leg_known : !(after.server_ms && after.server_ms > 0));
  const improvement =
    beforeShown && after?.up && !afterToRelay && beforeShown.rtt_ms > 0 ? Math.round(((beforeShown.rtt_ms - after.rtt_ms) / beforeShown.rtt_ms) * 100) : null;
  // Sub-millisecond or few-percent gaps are probe noise; calling them faster
  // or slower would overstate what the route does.
  const sameAsDirect = improvement != null && Math.abs(beforeShown!.rtt_ms - after!.rtt_ms) < Math.max(1, beforeShown!.rtt_ms * 0.05);

  const leg = snap ? (snap.server_leg_known ? (snap.server_leg_ms ?? null) : null) : after?.server_ms && after.server_ms > 0 ? after.server_ms : null;
  const toRelay = after?.up ? Math.max(0, after.rtt_ms - (leg ?? 0)) : null;
  const activePaths = (snap?.paths ?? []).filter((p) => p.active);
  const relayLabel = activePaths.length > 1 ? `${activePaths.length} 条中继线路` : (activePaths[0]?.label ?? best?.label ?? "中继");
  const primary = servers.find((s) => s.primary) ?? servers[0];
  const serverLabel = primary ? primary.addr.replace(/:\d+$/, "") : "游戏服务器";
  const stab = stability(after);

  const pingCaption = (() => {
    if (snap?.fallback) return <span className="q-bad">{managed || settings.kill_switch ? "线路中断，游戏流量已暂停" : "线路中断，已恢复直连"}</span>;
    if (waiting) return "等待游戏连接";
    const normal = beforeShown ? `直连 ${ms(beforeShown.rtt_ms)} ms` : "";
    if (sameAsDirect) return `${normal} · 基本相同`;
    if (improvement != null && improvement > 0)
      return (
        <>
          {normal} · <span className="q-good">{improvement}% 降幅</span>
        </>
      );
    if (improvement != null && improvement < 0)
      return (
        <>
          {normal} · <span className="q-warn">{-improvement}% 增幅</span>
        </>
      );
    if (!beforeShown && (snap || analysis)) {
      if (demo) return "尚未测量直连";
      if (snap) return "服务器未回复探测";
      return after?.up ? `中继 ${ms(after.rtt_ms)} ms · 等待识别游戏服务器` : "尚未识别游戏服务器";
    }
    if (afterToRelay) return "测量至中继";
    return analyzing ? "正在测量…" : "尚未测量";
  })();

  // ---- the big button ----------------------------------------------------

  type BtnState = "setup" | "blocked" | "idle" | "starting" | "stopping" | "waiting" | "on" | "down";
  const btn: BtnState = needsRelays
    ? "setup"
    : busy === "stop"
      ? "stopping"
      : busy === "apply" || (sess && live.status === "applying")
        ? "starting"
        : snap?.fallback
          ? "down"
          : sess
            ? waiting
              ? "waiting"
              : "on"
            : !game.routable
              ? "blocked"
              : "idle";

  const onBig = () => {
    if (btn === "setup") onOpenRelays();
    else if (sess) stop();
    else apply();
  };

  const procText = processes.length ? processes.join(", ") : game.name;
  const caption: ReactNode = (() => {
    switch (btn) {
      case "setup":
        return "请先为 Lowpath 配置可用的中继节点。";
      case "blocked":
        return captureMode === "route" ? (
          <>尚未识别 {game.name} 的进程。请启动游戏，然后选择 <b>添加游戏</b>.</>
        ) : (
          <>代理模式需要配置 {game.name}的服务器地址。也可以在「设置 → 网络」中切换到路由模式。</>
        );
      case "starting":
        return "正在连接中继…";
      case "stopping":
        return "正在恢复直连…";
      case "waiting":
        return (
          <>
            现在启动 <b>{game.name}</b>，Lowpath 会自动捕获游戏流量。
          </>
        );
      case "down":
        return managed || settings.kill_switch ? "所有线路均已中断。线路恢复前，游戏流量将暂停。" : "所有线路均已中断。线路恢复前，游戏将保持直连。";
      case "on":
        if (demo) return <>正在转发模拟游戏流量： {relayLabel}.</>;
        if (routeCapture) return <>正在加速 <b>{procText}</b> 经由 {relayLabel}.</>;
        return (
          <>
            将 {game.name} 连接到 <b className="num">{snap?.listen}</b>
          </>
        );
      default:
        if (analyzing) return "正在测量线路…";
        if (otherGameName) return <>开启加速后将把线路从 {otherGameName} 切换至 {game.name}.</>;
        return demo || captureMode !== "route" ? "点击「加速」开始。" : <>点击「加速」后启动 {game.name}.</>;
    }
  })();

  const isOn = btn === "on" || btn === "waiting" || btn === "down";
  // Before a session the relay-only number would pass for a game ping it
  // isn't; the caption gives it its real name instead.
  const showPing = !!after?.up && !(afterToRelay && !snap);
  const pill = snap?.fallback ? "error" : busy === "apply" || live.status === "applying" ? "applying" : analyzing && !sess ? "analyzing" : sess ? "optimizing" : "idle";
  const lossQ = after?.up ? (after.loss_pct >= 3 ? "bad" : after.loss_pct >= 1 ? "warn" : "good") : "";

  return (
    <div className="game">
      <Banner game={game} status={<StatusPill status={pill} />} />

      {btn === "setup" ? (
        <>
          <section className="card setup-wrap">
            {managed ? <div className="setup"><h2>暂无可用节点</h2><p>请检查设备授权和网关连接。</p><button className="btn" onClick={onOpenRelays}>查看中继节点</button></div> : <Setup gameName={game.name} onOpenRelays={onOpenRelays} onEnableLoopback={onEnableLoopback} />}
          </section>
          <CurrentServer gameId={game.id} gameName={game.name} />
          {sdr && <ValveServers gameId={game.id} gameName={game.name} now={now} />}
        </>
      ) : (
        <>
          <section className={`card control ${isOn ? "control-on" : ""}`}>
            <div className="control-stats">
              <div className="stat-main">
                <div className="k">{isOn ? "当前延迟" : "预计延迟"}</div>
                <div className="v num">
                  <span className={showPing ? `q-${quality(after)}` : "faint"}>{showPing ? ms(after!.rtt_ms) : "—"}</span>
                  {showPing && <small>ms</small>}
                </div>
                <div className="s">{pingCaption}</div>
              </div>
              <div className="stat-side">
                <div>
                  <div className="k">丢包率</div>
                  <div className={`v num ${lossQ ? `q-${lossQ}` : "faint"}`}>
                    {after?.up ? pct(after.loss_pct) : "—"}
                    {after?.up && <small>%</small>}
                  </div>
                </div>
                <div>
                  <div className="k">稳定性</div>
                  <div className={`v ${stab ? `q-${stab.q}` : "faint"}`}>{stab ? stab.word : "—"}</div>
                </div>
              </div>
            </div>
            <div className="control-actions">
              <label className="field-label" htmlFor="server-pick">
                服务器
              </label>
              <select id="server-pick" className="select" value={choice} onChange={(e) => pick(e.target.value)} disabled={busy != null || routes.length === 0}>
                {game.smart && <option value="smart">自动 · 最佳线路{routes.length > 1 ? `s (${routes.length})` : ""}</option>}
                {regionOpts.map((c) => (
                  <option key={c} value={`region:${c}`}>
                    {continents.length > 1 ? `${c} · 最佳线路` : "最佳线路"}
                  </option>
                ))}
                {routes.map((r) => {
                  const st = analysis?.paths?.[r.id];
                  return (
                    <option key={r.id} value={`route:${r.id}`}>
                      {r.label}
                      {st?.up ? ` · ${ms(st.rtt_ms)} ms` : ""}
                    </option>
                  );
                })}
              </select>
              <BoostButton state={btn} gameName={game.name} onClick={onBig} />
              {refused && !sess && btn === "idle" ? (
                <div className="control-caption boost-refused" role="alert">
                  <ShieldAlert size={14} style={{ flex: "none" }} />
                  <span>
                    {refused.message}.{" "}
                    <button
                      className="link"
                      onClick={() => apply(choice, true)}
                      title="继续启动路由模式。游戏流量可能由任一程序转发，也可能发生丢包。"
                    >
                      继续加速
                    </button>
                  </span>
                </div>
              ) : (
                <div className="control-caption" aria-live="polite">
                  {isOn && btn !== "down" && <span className="live-dot" aria-hidden="true" />}
                  <span>{caption}</span>
                </div>
              )}
            </div>
          </section>

          <RouteMap
            on={!!sess && !snap?.fallback}
            flowing={btn === "on"}
            relayLabel={relayLabel}
            serverLabel={serverLabel}
            toRelay={toRelay}
            relayToServer={leg}
            direct={beforeShown ? beforeShown.rtt_ms : null}
          />

          <CurrentServer gameId={game.id} gameName={game.name} />
          {sdr && <ValveServers gameId={game.id} gameName={game.name} now={now} />}

          {loopbackOnly && (
            <div className="note">
              <Wifi size={14} /> 当前使用本机测试中继，只验证转发功能。{" "}
              <button className="link" onClick={onOpenRelays}>
                添加远程中继
              </button>
            </div>
          )}

          {snap && snap.after.up && snap.after.jitter_ms > 8 && (
            <div className="tip">
              <Info size={15} /> 所有线路的抖动都偏高，可能与 Wi-Fi 或其他设备占用带宽有关。
            </div>
          )}

          <div className="more">
            <button className="more-toggle" onClick={toggleDetails} aria-expanded={details}>
              <ChevronDown size={16} className={details ? "rot" : ""} /> 详情
              <span className="more-hint">延迟图、线路、流量</span>
            </button>
            {details && (
              <div className="more-body">
                <div className="card graph-card">
                  <div style={{ display: "flex", alignItems: "center", marginBottom: 8 }}>
                    <span className="section-label">实时延迟 · 最近 60 秒</span>
                    <span className="graph-legend" style={{ marginLeft: "auto" }}>
                      <span>
                        <i style={{ background: "var(--graph-before)" }} />
                        直连
                      </span>
                      <span>
                        <i style={{ background: "var(--accent)" }} />
                        Lowpath{afterToRelay ? "（至中继）" : ""}
                      </span>
                    </span>
                  </div>
                  {snap ? (
                    <LiveGraph points={history} now={now} />
                  ) : (
                    <div className="empty-list" style={{ height: 150, display: "grid", placeItems: "center" }}>
                      点击「加速」后开始绘图。
                    </div>
                  )}
                </div>

                {routeCapture && <TrafficCard gameName={game.name} processes={processes} servers={servers} capture={live.capture ?? null} now={now} />}

                <div className="routes">
                  <div className="routes-head section-label">
                    <span>线路</span>
                    <span style={{ textAlign: "right" }}>延迟</span>
                    <span style={{ textAlign: "right" }}>丢包</span>
                    <span style={{ textAlign: "right" }}>抖动</span>
                    <span />
                  </div>
                  {routes.map((r) => (
                    <RouteRow key={r.id} title={r.label} desc={[r.region, r.continent].filter(Boolean).join(" · ")} stats={statsFor(r.id)} loading={analyzing} active={statsFor(r.id)?.active} />
                  ))}
                  {routes.length === 0 && <div className="empty-list">暂无线可用线路。</div>}
                  {!snap && analysis?.loaded && (
                    <div className="muted" role="note" style={{ fontSize: 12, padding: "6px 2px 0" }}>
                      测量时缓冲膨胀测试占满了带宽，延迟可能偏高。可在测试结束后重新测量。
                    </div>
                  )}
                </div>

                <div className="facts">
                  <span>
                    封包副本： <b>{DUP_LABEL[settings.dup_level]}</b>
                  </span>
                  <span>
                    并行线路： <b className="num">{snap ? `${snap.copies} of ${snap.paths?.length ?? 0}` : "—"}</b>
                  </span>
                  <span>
                    已去重： <b className="num">{snap ? snap.dups_dropped.toLocaleString() : "—"}</b>
                  </span>
                  <span>
                    捕获模式： <b>{captureMode === "route" ? "路由模式" : "代理模式"}</b>
                  </span>
                  {sess && live.wifi && (
                    <span title={live.wifi.message || live.wifi.notes?.join(" ") || undefined}>
                      Wi-Fi 扫描： <b>{WIFI_TUNE[live.wifi.state]}</b>
                    </span>
                  )}
                  <span className="spacer" />
                  <button className="btn btn-sm" onClick={analyze} disabled={analyzing || !game.routable || !!snap}>
                    {analyzing ? <Loader2 size={14} className="spin" /> : <RotateCw size={14} />}
                    {analyzing ? "正在测量…" : "重新测量线路"}
                  </button>
                </div>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}

// The boost's Wi-Fi scan pause, for the facts row.
const WIFI_TUNE: Record<NonNullable<Live["wifi"]>["state"], string> = {
  applying: "正在暂停…",
  on: "已暂停",
  partial: "部分暂停",
  failed: "未暂停",
  off: "正常",
};

function BoostButton({ state, gameName, onClick }: { state: string; gameName: string; onClick: () => void }) {
  const on = state === "on" || state === "waiting" || state === "down";
  const busy = state === "starting" || state === "stopping";
  return (
    <button
      className={`boost-btn ${on ? "boost-btn-on" : ""}`}
      onClick={onClick}
      disabled={busy || state === "blocked"}
      aria-label={on ? `停止加速 ${gameName}` : `加速 ${gameName}`}
      aria-pressed={on}
    >
      {busy ? <Loader2 size={18} className="spin" /> : on ? <Power size={18} /> : <Zap size={18} />}
      {state === "starting" ? "正在启动…" : state === "stopping" ? "正在停止…" : on ? "停止" : "加速"}
    </button>
  );
}

// Banner shows the game the way its launcher does: Steam's hero artwork with
// the transparent title logo on top, or the game's own icon and name.
function Banner({ game, status }: { game: Game; status: ReactNode }) {
  const [hero, setHero] = useState(() => hasArt(game.id, "hero"));
  const [logo, setLogo] = useState(() => hasArt(game.id, "logo"));
  return (
    <section className={`gbanner ${hero ? "" : "gbanner-plain"}`} style={{ "--game": game.color } as CSSProperties}>
      {hero && <ArtImg id={game.id} kind="hero" className="gbanner-img" onMissing={() => setHero(false)} />}
      <div className="gbanner-shade" />
      <div className="gbanner-status">{status}</div>
      <div className="gbanner-body">
        {logo ? (
          <>
            <h1 className="sr-only">{game.name}</h1>
            <ArtImg id={game.id} kind="logo" className="gbanner-logo" onMissing={() => setLogo(false)} />
          </>
        ) : (
          <h1 className="gbanner-title">
            <GameIcon game={game} size={44} />
            <span>{game.name}</span>
          </h1>
        )}
        <div className="gbanner-meta">
          {game.genre}
          {game.running && <span className="badge-live">● 运行中</span>}
        </div>
      </div>
    </section>
  );
}

// Shown in place of the big button until a relay exists, so the first thing a
// new player meets is a way forward rather than a button that can only fail.
function Setup({ gameName, onOpenRelays, onEnableLoopback }: { gameName: string; onOpenRelays: () => void; onEnableLoopback: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const enable = async () => {
    setBusy(true);
    try {
      await onEnableLoopback();
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="setup">
      <h2>配置加速节点</h2>
      <div className="muted">Lowpath 会通过中继转发 {gameName}的游戏流量。请选择中继来源。</div>
      <div className="setup-cards">
        <button className="setup-card" onClick={enable} disabled={busy}>
          <span className="setup-icon">{busy ? <Loader2 size={22} className="spin" /> : <Wifi size={22} />}</span>
          <span className="t">本机试用</span>
          <span className="d">启用内置中继，验证本机流量捕获和转发。</span>
        </button>
        <button className="setup-card" onClick={onOpenRelays}>
          <span className="setup-icon">
            <Plus size={22} />
          </span>
          <span className="t">添加自有服务器</span>
          <span className="d">使用位于游戏服务器附近的自有 VPS 作为中继。</span>
        </button>
      </div>
    </div>
  );
}

function RouteRow(props: { title: string; desc: string; stats?: Stats; loading?: boolean; active?: boolean }) {
  const { title, desc, stats, loading, active } = props;
  const q = stats ? quality(stats) : null;
  const metric = (v: string, unit: string) =>
    loading && !stats ? (
      <span className="metric">
        <span className="skeleton" />
      </span>
    ) : (
      <span className="metric num">
        {stats ? (stats.up ? v : "—") : "—"}
        {stats?.up && <small>{unit}</small>}
      </span>
    );
  return (
    <div className="route-card">
      <span className="label">
        <span style={{ width: 18 }} />
        <span style={{ minWidth: 0 }}>
          <span className="title" style={{ display: "block" }}>
            {title}
            {active && <span className="carrying">● 实时</span>}
          </span>
          <span className="desc">{desc}</span>
        </span>
      </span>
      {metric(stats ? ms(stats.rtt_ms) : "", "ms")}
      {metric(stats ? pct(stats.loss_pct) : "", "%")}
      {metric(stats ? ms(stats.jitter_ms) : "", "ms")}
      <span style={{ display: "grid", placeItems: "center" }} title={q ? `质量：${q}` : undefined}>
        {q && !loading ? <span className={`qdot q-${q}`} /> : <span className="qdot" style={{ color: "var(--border-strong)" }} />}
      </span>
    </div>
  );
}
