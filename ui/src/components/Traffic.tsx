import { ArrowDown, ArrowUp, Radar } from "lucide-react";
import type { CaptureStats, GameServer } from "../types";
import { since } from "../util";

interface Props {
  gameName: string;
  processes: string[];
  servers: GameServer[];
  capture: CaptureStats | null;
  now: number;
}

// Route mode, while optimizing: either "launch the game now" or the game
// servers the captured traffic is going to, plus the capture counters that
// prove packets are really being intercepted and put back.
export function TrafficCard({ gameName, processes, servers, capture, now }: Props) {
  const pids = capture?.game_pids ?? [];
  const sorted = [...servers].sort((a, b) => Number(b.primary) - Number(a.primary) || b.pkts_up - a.pkts_up);

  return (
    <div className="card traffic" aria-live="polite">
      {sorted.length === 0 ? (
        <div className="waiting">
          <span className="radar radar-sm" aria-hidden="true">
            <Radar size={20} />
          </span>
          <div>
            <div className="t">正在等待游戏流量，请启动游戏</div>
            <div className="d">
              {pids.length > 0
                ? `${gameName} 正在运行（PID ${pids.join("、")}）。加入对局或打开服务器列表后，此处会显示连接目标。`
                : `Lowpath 会在 ${processes.length ? processes.join("、") : gameName} 发包时自动捕获，游戏里无需额外设置。`}
            </div>
          </div>
        </div>
      ) : (
        <>
          <div className="traffic-head">
            <span className="section-label">游戏服务器</span>
            {pids.length > 0 && <span className="hint num">PID {pids.join(", ")}</span>}
          </div>
          <div className="server-list" role="list" aria-label="游戏服务器">
            {sorted.map((s) => (
              <div className="server-row" role="listitem" key={s.addr}>
                <span className="addr num">{s.addr}</span>
                {s.primary && <span className="tag tag-accent">主要连接</span>}
                <span className="spacer" />
                <span className="num pk" title="发往此服务器的数据包">
                  <ArrowUp size={12} /> {s.pkts_up.toLocaleString()}
                </span>
                <span className="num pk" title="从此服务器收到的数据包">
                  <ArrowDown size={12} /> {s.pkts_down.toLocaleString()}
                </span>
                <span className="faint num seen">{seen(s.last_seen, now)}</span>
              </div>
            ))}
          </div>
        </>
      )}
      {capture && (
        <div className="counters">
          <Counter k="已捕获" v={capture.captured} title="已捕获的游戏出站包" />
          <Counter k="已直连发送" v={capture.passed} title="线路中断且断线保护关闭时直连发送的数据包" />
          <Counter k="已回注" v={capture.injected} title="已回注到游戏的包" />
          <Counter k="错误" v={capture.errors} bad={capture.errors > 0} />
          {(capture.route_repairs ?? 0) > 0 && (
            <Counter
              k="路由修复"
              v={capture.route_repairs ?? 0}
              bad
              title="路由被网络管理器、VPN 或防火墙移除后，Lowpath 重新安装路由的次数。每次可能短暂恢复直连。"
            />
          )}
        </div>
      )}
      {capture?.last_error && (
        <div className="capture-error q-bad" role="alert">
          最近的流量捕获错误： {capture.last_error}
        </div>
      )}
    </div>
  );
}

function Counter({ k, v, title, bad }: { k: string; v: number; title?: string; bad?: boolean }) {
  return (
    <div className="counter" title={title}>
      <div className="k">{k}</div>
      <div className={`v num ${bad ? "q-bad" : ""}`}>{(v ?? 0).toLocaleString()}</div>
    </div>
  );
}

function seen(iso: string, now: number): string {
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return "";
  return now - t < 2000 ? "now" : `${since(iso, now)}前`;
}
