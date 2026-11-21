import { Activity } from "lucide-react";
import type { LineState } from "../types";
import { fmtMbps, fmtMs } from "../util";

const what: Record<NonNullable<LineState["direction"]>, string> = {
  download: "下载",
  upload: "上传",
  both: "上传和下载",
};

// Shown during a boost when the passive detector sees other traffic fill
// the line while the game's ping rises.
export function LineBanner({ line }: { line: LineState }) {
  const rise = line.rtt_ms != null && line.baseline_ms != null ? Math.max(0, line.rtt_ms - line.baseline_ms) : null;
  const up = line.direction === "upload";
  const other = up ? line.other_up_mbps : line.other_down_mbps;
  const cap = up ? line.line_up_mbps : line.line_down_mbps;
  const top = line.top;
  const topMbps = top ? (up ? top.up_mbps : line.direction === "both" ? top.down_mbps + top.up_mbps : top.down_mbps) : null;
  return (
    <div className="banner banner-warn" role="alert">
      <Activity size={16} style={{ flex: "none" }} />
      <span style={{ flex: 1 }}>
        <b>当前网络带宽接近占满</b> ({what[line.direction ?? "download"]}），游戏延迟已增加{" "}
        {rise == null ? "" : <span className="num">+{fmtMs(rise)} ms</span>}
        {rise == null ? "" : " "}
        ，其他流量速率为 <span className="num">{fmtMbps(other)}</span>
        {cap != null && (
          <>
            {" "}
            / <span className="num">{fmtMbps(cap)}</span>
          </>
        )}{" "}
        Mbit/s.{" "}
        {top ? (
          <>
            主要流量来自 <b>{top.name}</b> （本机，<span className="num">{fmtMbps(topMbps)}</span> Mbit/s）。可暂停该程序或其他设备上的下载、云备份和串流。
          </>
        ) : (
          "可暂停当前网络内的游戏更新、下载、云备份或串流。"
        )}
      </span>
    </div>
  );
}
