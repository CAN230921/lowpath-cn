import type {
  Analysis,
  BloatStatus,
  Bootstrap,
  Conflict,
  ConflictReport,
  CurrentServer,
  Diagnosis,
  Game,
  GatewayView,
  Live,
  Mapping,
  NetLocation,
  ProcessInfo,
  RelayInput,
  RelayTest,
  RelayView,
  Route,
  RouteMode,
  ServerList,
  Settings,
  TraceMode,
  TraceStatus,
  WiFiView,
} from "./types";

export class ApiError extends Error {
  constructor(
    public kind: string,
    message: string,
    public status: number,
    // What is in the way, when the service says (kind "conflict", or a
    // capture failure next to a running booster).
    public conflicts: Conflict[] = [],
  ) {
    super(message);
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    // The daemon rejects mutating requests without this header (CSRF guard).
    headers: { "X-Lowpath": "1", ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data: { error?: string; message?: string; conflicts?: Conflict[] | null } | null = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    // A proxy or a crashed handler can answer with plain text; keep its words.
    if (!res.ok) throw new ApiError("error", `客户端后台请求失败（HTTP ${res.status}）`, res.status);
    throw new ApiError("bad_response", "Lowpath 服务返回了无法识别的响应", res.status);
  }
  if (!res.ok) {
    const message = data?.message && /\p{Script=Han}/u.test(data.message) ? data.message : "";
    throw new ApiError(data?.error ?? "error", message, res.status, data?.conflicts ?? []);
  }
  return data as T;
}

const enc = encodeURIComponent;

export type Account = {
  signed_in: boolean;
  name: string;
  expires_at: string;
  can_remember?: boolean;
  restore_pending?: boolean;
  notice?: string;
};

export const api = {
  account: () => call<Account>("GET", "/account"),
  passwordLogin: (username: string, password: string, remember: boolean) => call<Account>("POST", "/account/password", { username, password, remember }),
  logout: () => call<Account>("POST", "/account/logout"),
  bootstrap: () => call<Bootstrap>("GET", "/bootstrap"),
  mapping: () => call<Mapping>("POST", "/mapping"),
  routes: (id: string) => call<{ routes: Route[] | null; analysis?: Analysis }>("GET", `/games/${enc(id)}/routes`),
  analyze: (id: string) => call<Analysis>("POST", `/games/${enc(id)}/analyze`),
  servers: (id: string) => call<ServerList>("GET", `/games/${enc(id)}/servers`),
  pingServers: (id: string) => call<ServerList>("POST", `/games/${enc(id)}/servers/ping`),
  currentServer: (id: string) => call<CurrentServer>("GET", `/games/${enc(id)}/current-server`),
  favorite: (id: string) => call<{ favorites: string[] | null }>("POST", `/games/${enc(id)}/favorite`),
  apply: (game_id: string, mode: RouteMode, region?: string, route_id?: string, ignore_conflicts = false) =>
    call<Live>("POST", "/apply", { game_id, mode, region, route_id, ignore_conflicts }),
  conflicts: (fresh = false) => call<ConflictReport>("GET", fresh ? "/conflicts?fresh=1" : "/conflicts"),
  stop: () => call<Live>("POST", "/stop"),
  dismissFailsafe: () => call<Live>("DELETE", "/failsafe"),
  saveSettings: (s: Settings) => call<Settings>("PUT", "/settings", s),
  impair: (path_id: string, action: "degrade" | "outage" | "restore") =>
    call<{ ok: boolean }>("POST", "/demo/impair", { path_id, action }),

  relays: () => call<{ relays: RelayView[] | null }>("GET", "/relays"),
  addRelay: (r: RelayInput) => call<RelayView>("POST", "/relays", r),
  deleteRelay: (id: string) => call<{ ok: boolean }>("DELETE", `/relays/${enc(id)}`),
  testRelay: (target: { id: string } | { addr: string; key: string }) => call<RelayTest>("POST", "/relays/test", target),
  processes: () => call<{ processes: ProcessInfo[] | null }>("GET", "/processes"),
  addCustomGame: (name: string, process: string) => call<Game>("POST", "/games/custom", { name, process }),
  deleteCustomGame: (id: string) => call<{ ok: boolean }>("DELETE", `/games/custom/${enc(id)}`),
  elevate: () => call<{ ok: boolean }>("POST", "/elevate"),
  location: (refresh = false) => call<NetLocation>("GET", refresh ? "/location?refresh=1" : "/location"),
  bloat: () => call<BloatStatus>("GET", "/diag/bloat"),
  startBloat: () => call<BloatStatus>("POST", "/diag/bloat"),
  stopBloat: () => call<BloatStatus>("DELETE", "/diag/bloat"),
  trace: () => call<TraceStatus>("GET", "/diag/trace"),
  startTrace: (target: { target_id: string } | { host: string }, mode: TraceMode) =>
    call<TraceStatus>("POST", "/diag/trace", { ...target, mode }),
  stopTrace: () => call<TraceStatus>("DELETE", "/diag/trace"),
  verdict: () => call<Diagnosis>("GET", "/diag/verdict"),
  gateway: () => call<GatewayView>("GET", "/diag/gateway"),
  wifi: () => call<WiFiView>("GET", "/diag/wifi"),
  wifiSurvey: () => call<WiFiView>("POST", "/diag/wifi/survey"),
};

// Plain-language wording for the error kinds the daemon uses to say "you need
// to do something first", so the toast tells the user what to do next.
export function describeError(e: unknown, fallback: string): { title?: string; msg: string } {
  if (!(e instanceof ApiError)) return { msg: fallback };
  switch (e.kind) {
    case "needs_admin":
      // The daemon says which rights route mode needs on its OS; the
      // Windows wording below is the default.
      if (e.message && !e.message.includes("administrator")) return { title: "需要更多权限", msg: sentence(e.message) };
      return {
        title: "需要管理员权限",
        msg: "路由模式需要管理员权限。请以管理员身份重启 Lowpath，或在「设置 → 网络」中切换到代理模式。",
      };
    case "no_relays":
      return {
        title: "暂无中继",
        msg: "请在「设置 → 中继」中添加中继，或启用本机测试中继检查流量捕获。",
      };
    case "conflict":
      return { title: "请先关闭其他网络工具", msg: e.message || "另一个程序也在捕获游戏流量。" };
    case "capture_failed":
      return { title: "无法捕获游戏流量", msg: e.message || "流量捕获驱动启动失败。" };
    case "trace_running":
      return { title: "路由追踪正在运行", msg: e.message || "请等待追踪结束，或先手动停止追踪。" };
    case "bloat_running":
      return { title: "缓冲膨胀测试正在运行", msg: e.message || "请等待缓冲膨胀测试结束。" };
    case "session_active":
      return { title: "请先停止加速", msg: e.message || "缓冲膨胀测试会占用带宽，导致游戏延迟升高。" };
    default:
      return { msg: e.message || fallback };
  }
}

export function subscribeLive(onData: (l: Live) => void, onDown: (down: boolean) => void): () => void {
  let es: EventSource | null = null;
  let retry: number | undefined;
  const connect = () => {
    es = new EventSource("/api/live");
    es.onmessage = (e) => {
      let l: Live;
      try {
        l = JSON.parse(e.data);
      } catch {
        return;
      }
      onDown(false);
      onData(l);
    };
    es.onerror = () => {
      onDown(true);
      es?.close();
      retry = window.setTimeout(connect, 1500);
    };
  };
  connect();
  return () => {
    es?.close();
    window.clearTimeout(retry);
  };
}

// sentence turns a daemon message ("route mode can't …") into one for a toast.
function sentence(s: string): string {
  const t = s.charAt(0).toUpperCase() + s.slice(1);
  return /[.!?]$/.test(t) ? t : `${t}.`;
}
