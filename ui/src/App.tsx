import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, Radio, Search, Settings as Gear, WifiOff, Zap } from "lucide-react";
import { api, ApiError, describeError, subscribeLive, type Account } from "./api";
import type { Bootstrap, Game, Live, Platform, RelayView, Settings } from "./types";
import { Logo } from "./components/Bits";
import { Mapping } from "./components/Mapping";
import { Library } from "./components/Library";
import { GamePanel } from "./components/GamePanel";
import { Home } from "./components/Home";
import { NetworkLab } from "./components/NetworkLab";
import { SettingsModal, type SettingsTab } from "./components/SettingsModal";
import { AddGameModal } from "./components/AddGame";
import { CaptureBanner } from "./components/CaptureBanner";
import { LineBanner } from "./components/LineBanner";
import { LineTestHint, lineTestHintDismissed } from "./components/LineTestHint";
import { FailsafeBanner } from "./components/FailsafeBanner";
import { ConflictBanner } from "./components/Conflicts";
import { Toasts, type Toast } from "./components/Toasts";
import { Login } from "./components/Login";
import type { Point } from "./components/LiveGraph";

type Phase = "loading" | "offline" | "unauthorized" | "login" | "mapping" | "main";

// The service answers 401 to a page that doesn't hold this run's session
// cookie: opened by hand, or left over from before Lowpath restarted.
const notConnected = (e: unknown) => e instanceof ApiError && e.status === 401;

export function App() {
  const [phase, setPhase] = useState<Phase>("loading");
  const [account, setAccount] = useState<Account | null>(null);
  const [boot, setBoot] = useState<Bootstrap | null>(null);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [games, setGames] = useState<Game[]>([]);
  const [recent, setRecent] = useState<string[]>([]);
  const [live, setLive] = useState<Live | null>(null);
  const [down, setDown] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [relays, setRelays] = useState<RelayView[]>([]);
  const [platform, setPlatform] = useState<Platform | null>(null);
  const [showSettings, setShowSettings] = useState<SettingsTab | null>(null);
  const [showAddGame, setShowAddGame] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  // The fail-open notice the user closed (by its time): hidden here at once,
  // while the service forgets it for reloads and other windows.
  const [failsafeSeen, setFailsafeSeen] = useState("");
  const [lineHintOff, setLineHintOff] = useState(lineTestHintDismissed);
  const dismissFailsafe = useCallback((at: string) => {
    setFailsafeSeen(at);
    // If the service can't be reached, the notice stays hidden in this window.
    api.dismissFailsafe().then(setLive).catch(() => {});
  }, []);
  const [history, setHistory] = useState<Point[]>([]);
  const [now, setNow] = useState(Date.now());
  const searchRef = useRef<HTMLInputElement>(null);
  const seenEvents = useRef<{ session: string; count: number }>({ session: "", count: 0 });
  const toastId = useRef(0);
  const wasDown = useRef(false);

  // The route-mapping sweep needs something to probe; with no relays yet the
  // game panel's onboarding is the useful next step instead.
  const needsMapping = (b: Bootstrap) => !b.mapping && (b.demo || b.relays == null || b.relays.length > 0);

  const applyBoot = useCallback((b: Bootstrap) => {
    setBoot(b);
    setSettings(b.settings);
    setGames(b.games ?? []);
    setRelays(b.relays ?? []);
    setPlatform(b.platform ?? null);
  }, []);

  const load = useCallback(async () => {
    try {
      const managed = await api.account().catch((e) => {
        if (e instanceof ApiError && e.status === 404) return null;
        throw e;
      });
      setAccount(managed);
      if (managed && !managed.signed_in) {
        setPhase("login");
        return;
      }
      const b = await api.bootstrap();
      applyBoot(b);
      setRecent(b.recent ?? []);
      setLive(b.live);
      if (b.live?.session) setSelected(b.live.session.game_id);
      setPhase(!managed && needsMapping(b) ? "mapping" : "main");
    } catch (e) {
      setPhase(notConnected(e) ? "unauthorized" : "offline");
    }
  }, [applyBoot]);

  useEffect(() => {
    load();
  }, [load]);

  const managedSignedIn = phase === "main" && account?.signed_in === true;
  useEffect(() => {
    if (!managedSignedIn) return;
    let stopped = false;
    const timer = window.setInterval(async () => {
      try {
        const next = await api.account();
        if (stopped) return;
        setAccount(next);
        if (!next.signed_in) {
          setLive(null);
          setShowSettings(null);
          setPhase("login");
        }
      } catch {
        // The desktop service can be temporarily unreachable after sleep.
      }
    }, 60_000);
    return () => {
      stopped = true;
      window.clearInterval(timer);
    };
  }, [managedSignedIn]);

  // After the service comes back (for example restarted as administrator),
  // platform, relays and settings may have changed under us.
  useEffect(() => {
    if (down) {
      wasDown.current = true;
      return;
    }
    if (!wasDown.current) return;
    wasDown.current = false;
    api.bootstrap().then(applyBoot, () => {});
  }, [down, applyBoot]);

  // The live stream can't tell "service gone" from "service restarted and
  // this page is no longer connected"; while it is down, ask the API.
  useEffect(() => {
    if (!down) return;
    const id = window.setInterval(() => {
      api.bootstrap().catch((e) => {
        if (notConnected(e)) setPhase("unauthorized");
      });
    }, 3000);
    return () => window.clearInterval(id);
  }, [down]);

  const refreshRelays = useCallback(async () => {
    try {
      const r = await api.relays();
      setRelays(r.relays ?? []);
    } catch {
      // Keep the last known list; the next bootstrap refresh corrects it.
    }
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = settings?.theme ?? "dark";
  }, [settings?.theme]);

  const pushToast = useCallback((kind: Toast["kind"], msg: string, title?: string) => {
    const id = ++toastId.current;
    setToasts((t) => [...t.slice(-3), { id, kind, msg, title }]);
    // Errors usually say what to do next, so they stay up long enough to read.
    window.setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), kind === "fallback" || kind === "error" ? 9000 : 4500);
  }, []);

  // Live stream: status, engine snapshot, running games.
  useEffect(() => {
    if (phase !== "main") return;
    return subscribeLive((l) => {
      const t = Date.now();
      setNow(t);
      setLive(l);
      const running = new Set(l.running ?? []);
      setGames((gs) => (gs.some((g) => g.running !== running.has(g.id)) ? gs.map((g) => ({ ...g, running: running.has(g.id) })) : gs));

      const key = l.session?.started_at ?? "";
      if (key !== seenEvents.current.session) {
        seenEvents.current = { session: key, count: 0 };
        setHistory([]);
      }
      if (l.snapshot) {
        const s = l.snapshot;
        const evs = s.events ?? [];
        evs.slice(seenEvents.current.count).forEach((e) => pushToast(e.kind, e.msg));
        seenEvents.current.count = evs.length;
        const beforeOk = s.before?.up && s.before_known !== false;
        setHistory((h) => [...h.filter((p) => p.t > t - 65_000), { t, before: beforeOk ? s.before.rtt_ms : null, after: s.after?.up ? s.after.rtt_ms : null }]);
      }
    }, setDown);
  }, [phase, pushToast]);

  // Ctrl/Cmd+K focuses the game search.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        searchRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Keep time-based labels ticking even without live updates.
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(t);
  }, []);

  const onFavorite = async (id: string) => {
    try {
      const favorites = (await api.favorite(id)).favorites ?? [];
      setGames((gs) => gs.map((g) => ({ ...g, favorite: favorites.includes(g.id) })));
    } catch (e) {
      pushToast("error", describeError(e, "无法更新收藏").msg);
    }
  };

  const selectGame = (id: string) => {
    setSelected(id);
    setRecent((r) => [id, ...r.filter((x) => x !== id)].slice(0, 8));
  };

  const onGameAdded = (g: Game) => {
    setShowAddGame(false);
    setGames((gs) => [...gs.filter((x) => x.id !== g.id), { ...g, custom: true }]);
    selectGame(g.id);
    pushToast("success", `已将 ${g.name} 添加到游戏库。点击「加速」后启动游戏。`, "已添加游戏");
    // The add response may not carry the library flags (running, routable…);
    // the bootstrap list is the source of truth.
    api.bootstrap().then((b) => setGames(b.games ?? []), () => {});
  };

  const removeGame = async (g: Game) => {
    if (!window.confirm(`要从游戏库移除 ${g.name} 吗？`)) return;
    try {
      await api.deleteCustomGame(g.id);
      setGames((gs) => gs.filter((x) => x.id !== g.id));
      setRecent((r) => r.filter((x) => x !== g.id));
      if (selected === g.id) setSelected(null);
    } catch (e) {
      const d = describeError(e, `无法移除 ${g.name}`);
      pushToast("error", d.msg, d.title);
    }
  };

  const enableLoopback = async () => {
    if (!settings) return;
    try {
      setSettings(await api.saveSettings({ ...settings, loopback_relay: true }));
      await refreshRelays();
    } catch (e) {
      const d = describeError(e, "无法启用本机测试中继");
      pushToast("error", d.msg, d.title);
    }
  };

  const onError = useCallback((msg: string, title?: string) => pushToast("error", msg, title), [pushToast]);

  const logout = async () => {
    try {
      await api.logout();
      setLive(null);
      setShowSettings(null);
      await load();
    } catch (e) {
      onError(describeError(e, "退出登录失败，请重试。").msg);
    }
  };

  if (phase === "login") return <Login account={account} onSuccess={load} />;

  if (phase === "loading") {
    return (
      <div className="mapping">
        <Loader2 className="spin" color="var(--accent)" />
      </div>
    );
  }
  if (phase === "unauthorized") {
    return (
      <div className="mapping">
        <div className="mapping-inner">
          <WifiOff size={36} color="var(--bad)" />
          <h2 style={{ margin: "18px 0 4px", fontSize: 19 }}>当前页面尚未连接 Lowpath</h2>
          <div className="muted">
            请从 Lowpath 窗口访问，或退出后重新启动应用。
          </div>
        </div>
      </div>
    );
  }

  if (phase === "offline") {
    return (
      <div className="mapping">
        <div className="mapping-inner">
          <WifiOff size={36} color="var(--bad)" />
          <h2 style={{ margin: "18px 0 4px", fontSize: 19 }}>无法连接 Lowpath 服务</h2>
          <div className="muted">请启动 Lowpath 后重试。</div>
          <button className="btn" style={{ marginTop: 18 }} onClick={load}>
            重试
          </button>
        </div>
      </div>
    );
  }
  if (!boot || !settings) {
    return (
      <div className="mapping">
        <Loader2 className="spin" color="var(--accent)" />
      </div>
    );
  }
  if (phase === "mapping") {
    return <Mapping onDone={() => setPhase("main")} />;
  }

  const game = games.find((g) => g.id === selected) ?? null;
  const sessGame = live?.session ? games.find((g) => g.id === live.session!.game_id) : null;
  // Route mode is chosen but this process can't open the capture driver.
  const captureBlocked = !boot.demo && settings.capture_mode === "route" && !!platform && !platform.capture.available;

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <Logo />
          Lowpath
        </div>
        <label className="search">
          <Search size={16} />
          <input ref={searchRef} placeholder="搜索游戏…" value={query} onChange={(e) => setQuery(e.target.value)} aria-label="搜索游戏" />
          {!query && <kbd>Ctrl K</kbd>}
        </label>
        <div className="topbar-right">
          {account?.signed_in && <button className="btn btn-ghost" onClick={logout} title={`退出 ${account.name}`}>退出登录</button>}
          {sessGame && (
            <button className="chip chip-accent" onClick={() => selectGame(sessGame.id)} title="前往正在加速的游戏">
              <Zap size={13} /> {sessGame.name}
            </button>
          )}
          {boot.demo && (
            <span className="chip" title="在本机模拟中继、运营商线路和游戏服务器">
              <Radio size={13} /> 模拟网络
            </span>
          )}
          <button className="btn btn-ghost icon-btn" onClick={() => setShowSettings("network")} aria-label="设置">
            <Gear size={18} />
          </button>
        </div>
      </header>

      <Library
        games={games}
        recent={recent}
        query={query}
        selected={selected}
        optimizingId={live?.session?.game_id ?? null}
        onSelect={selectGame}
        onFavorite={onFavorite}
        onAddGame={() => setShowAddGame(true)}
        onRemoveGame={removeGame}
      />

      <main className="main">
        {account?.notice && <div className="main-banner" role="status">{account.notice}</div>}
        {down && (
          <div className="banner banner-bad" style={{ margin: "16px 28px 0" }}>
            <WifiOff size={16} /> 已断开与 Lowpath 服务的连接，正在重连…
          </div>
        )}
        {captureBlocked && platform && (
          <div className="main-banner">
            <CaptureBanner platform={platform} down={down} onOpenNetwork={() => setShowSettings("network")} onError={onError} />
          </div>
        )}
        {live?.failsafe && live.failsafe.at !== failsafeSeen && !live.session && (
          <div className="main-banner">
            <FailsafeBanner failsafe={live.failsafe} onDismiss={() => dismissFailsafe(live.failsafe?.at ?? "")} />
          </div>
        )}
        {live?.conflicts && !boot.demo && (
          <div className="main-banner">
            <ConflictBanner report={live.conflicts} />
          </div>
        )}
        {live?.line?.active && (
          <div className="main-banner">
            <LineBanner line={live.line} />
          </div>
        )}
        {live?.line?.needs_test && !live.line.active && !lineHintOff && !boot.demo && (
          <div className="main-banner">
            <LineTestHint onOpen={() => setShowSettings("diagnostics")} onDismiss={() => setLineHintOff(true)} />
          </div>
        )}
        {game && live ? (
          <>
            <GamePanel
              key={game.id}
              game={game}
              live={live}
              settings={settings}
              relays={relays}
              demo={boot.demo}
              managed={!!account}
              history={history}
              now={now}
              otherGameName={sessGame && sessGame.id !== game.id ? sessGame.name : null}
              onError={onError}
              onOpenRelays={() => setShowSettings("relays")}
              onEnableLoopback={enableLoopback}
            />
            {boot.demo && (
              <div style={{ maxWidth: 920, margin: "-22px auto 40px", padding: "0 28px" }}>
                <NetworkLab paths={live.snapshot?.paths ?? []} />
              </div>
            )}
          </>
        ) : (
          <Home games={games} recent={recent} demo={boot.demo} onSelect={selectGame} onAddGame={() => setShowAddGame(true)} />
        )}
      </main>

      {showSettings && (
        <SettingsModal
          initialTab={showSettings}
          settings={settings}
          relays={relays}
          platform={platform}
          version={boot.version}
          demo={boot.demo}
          managed={!!account}
          onChange={setSettings}
          onRelaysChanged={refreshRelays}
          onRemap={() => {
            setShowSettings(null);
            setPhase("mapping");
          }}
          onClose={() => setShowSettings(null)}
          onError={onError}
        />
      )}
      {showAddGame && <AddGameModal games={games} onAdded={onGameAdded} onClose={() => setShowAddGame(false)} />}
      <Toasts toasts={toasts} onDismiss={(id) => setToasts((t) => t.filter((x) => x.id !== id))} />
    </div>
  );
}
