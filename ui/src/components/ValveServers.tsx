import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, Loader2, RotateCw, Server } from "lucide-react";
import { api } from "../api";
import type { POPView, ServerList } from "../types";
import { ms, pct, since } from "../util";

const SHOWN = 6; // rows before "Show all"
const POLL_MS = 1200; // while the list downloads or a ping round runs
// After a failed download: the service retries on its own backoff, but only
// when asked, so keep asking or "Trying again in 30 s" would never happen.
const RETRY_POLL_MS = 10_000;
// While the page stays open: lets the hourly list refresh and the 5-minute
// re-ping happen without leaving and reopening the game.
const IDLE_POLL_MS = 60_000;

// Ping colors match the route quality thresholds (util.ts quality()).
function pingQ(v: number): "good" | "warn" | "bad" {
  if (v >= 160) return "bad";
  if (v >= 90) return "warn";
  return "good";
}

const GEO_NOTE: Record<string, string> = {
  swapped: "已纠正 Valve 数据中颠倒的经纬度。",
  corrected: "已将异常坐标替换为城市坐标。",
};

// ValveServers lists the Steam Datagram Relay points of presence a Valve
// game plays through, with a direct ping from this PC to each, before the
// game is even started. The list comes from Steam and is refreshed in the
// background; nothing here waits on the network.
export function ValveServers({ gameId, gameName, now }: { gameId: string; gameName: string; now: number }) {
  const [list, setList] = useState<ServerList | null>(null);
  const [failed, setFailed] = useState(false);
  const [all, setAll] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  // Bumped when the game changes or the card unmounts: a reply that comes
  // back after that belongs to the old game and is dropped.
  const gen = useRef(0);

  const load = useCallback(
    async (ping = false) => {
      window.clearTimeout(timer.current);
      const g = gen.current;
      try {
        const l = ping ? await api.pingServers(gameId) : await api.servers(gameId);
        if (g !== gen.current) return;
        setList(l);
        setFailed(false);
        if (l.state === "unsupported") return;
        const wait = l.state === "loading" || l.pinging ? POLL_MS : l.state === "error" ? RETRY_POLL_MS : IDLE_POLL_MS;
        timer.current = window.setTimeout(() => load(), wait);
      } catch {
        if (g !== gen.current) return;
        // The local service is restarting or busy: say so, try again later.
        setFailed(true);
        timer.current = window.setTimeout(() => load(), 5000);
      }
    },
    [gameId],
  );

  useEffect(() => {
    gen.current++;
    setList(null);
    setAll(false);
    load();
    return () => {
      gen.current++;
      window.clearTimeout(timer.current);
    };
  }, [load]);

  if (list?.state === "unsupported") return null;

  const pops = list?.pops ?? [];
  const rows = all ? pops : pops.slice(0, SHOWN);
  const loading = !list || list.state === "loading";
  const pinging = !!list?.pinging;
  // No list after a failed download: the button asks for it again now.
  const retry = list?.state === "error";

  return (
    <section className="card pops" aria-label="Valve 服务器">
      <div className="pops-head">
        <div className="pops-titles">
          <span className="section-label">Valve 服务器</span>
          <div className="pops-sub">
            本机直连 {gameName} 的 Valve 中继延迟。对局由 Valve 中继转发，Lowpath 优化本机到中继的一段。
          </div>
        </div>
        <button className="btn btn-sm" onClick={() => load(true)} disabled={loading || pinging || (list?.state !== "ready" && !retry)}>
          {pinging ? <Loader2 size={14} className="spin" /> : <RotateCw size={14} />}
          {pinging ? "正在探测…" : retry ? "重试" : "重新探测"}
        </button>
      </div>

      {failed && !list && <div className="pops-msg faint">暂时无法连接 Lowpath 服务，正在重试。</div>}

      {list?.state === "error" && (
        <div className="note-warn" role="status">
          <AlertTriangle size={14} /> {list.message}
        </div>
      )}

      {list?.state === "ready" && list.stale && list.message && (
        <div className="note-warn" role="status">
          <AlertTriangle size={14} /> {list.message}
        </div>
      )}

      {loading && !failed && (
        <div className="pop-list" aria-busy="true">
          {[0, 1, 2].map((i) => (
            <div className="pop-row" key={i}>
              <span className="pop-name">
                <span className="skeleton" style={{ width: 120 }} />
              </span>
              <span className="metric">
                <span className="skeleton" />
              </span>
              <span className="metric">
                <span className="skeleton" style={{ width: 30 }} />
              </span>
            </div>
          ))}
          <div className="pops-msg faint">正在从 Steam 读取 Valve 服务器列表…</div>
        </div>
      )}

      {pops.length > 0 && (
        <>
          <div className="pop-row pop-row-head section-label" aria-hidden="true">
            <span>位置</span>
            <span style={{ textAlign: "right" }}>延迟</span>
            <span style={{ textAlign: "right" }}>丢包</span>
          </div>
          <div className="pop-list" role="list" aria-label="Valve 服务器">
            {rows.map((p) => (
              <POPRow key={p.id} pop={p} pinging={pinging} />
            ))}
          </div>
          {pops.length > SHOWN && (
            <button className="link pops-more" onClick={() => setAll((a) => !a)} aria-expanded={all}>
              {all ? "收起" : `展开全部 ${pops.length} 个`}
            </button>
          )}
        </>
      )}

      {list?.state === "ready" && pops.length === 0 && <div className="pops-msg faint">此游戏的 Valve 列表中没有公开中继地址： {gameName}.</div>}

      {list?.state === "ready" && (
        <div className="pops-foot faint">
          来自 Steam{list.fetched_at ? `，${since(list.fetched_at, now)}前${list.stale ? "缓存" : "更新"}` : ""} · ICMP，取最多 4 次探测中的最低延迟
          {list.no_relay > 0 ? ` · 另有 ${list.no_relay} 个数据中心未公开中继地址` : ""}
        </div>
      )}
    </section>
  );
}

function POPRow({ pop, pinging }: { pop: POPView; pinging: boolean }) {
  const measured = pop.ping_ms != null;
  const pending = !measured && !pop.pinged_at;
  const loss = pop.loss_pct;
  const sub = [pop.id, pop.country].filter(Boolean).join(" · ");
  const geo = pop.geo_fix ? GEO_NOTE[pop.geo_fix] : undefined;
  return (
    <div className="pop-row" role="listitem" title={geo}>
      <span className="pop-name">
        <Server size={14} className="pop-icon" aria-hidden="true" />
        <span style={{ minWidth: 0 }}>
          <span className="title">{pop.city}</span>
          <span className="desc">
            {sub}
            {!measured && !pending && pop.note ? ` · ${pop.note}` : ""}
          </span>
        </span>
      </span>
      <span className="metric num">
        {measured ? (
          <>
            <span className={`q-${pingQ(pop.ping_ms!)}`}>{ms(pop.ping_ms!)}</span>
            <small>ms</small>
          </>
        ) : pending && pinging ? (
          <span className="skeleton" />
        ) : (
          <span className="faint" title={pop.note ?? (pending ? "尚未测量" : undefined)}>
            —
          </span>
        )}
      </span>
      <span className="metric num">
        {measured && loss != null ? (
          <>
            <span className={loss > 0 ? "q-warn" : ""}>{pct(loss)}</span>
            <small>%</small>
          </>
        ) : (
          <span className="faint">—</span>
        )}
      </span>
    </div>
  );
}
