import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, Radar, Wifi } from "lucide-react";
import { api } from "../api";
import type { WiFiLink, WiFiSurveyView, WiFiTuning, WiFiView } from "../types";
import "../diag.css";

// The Wi-Fi card: signal, band and link rate of the Wi-Fi adapter, read
// from the system every few seconds, and on request the access point's own
// load report. Nothing here is estimated: a value the system didn't report
// is "—" with the reason.

const POLL_MS = 3000;

type Tone = "good" | "warn" | "bad" | "none";

// Signal words follow the usual RSSI bands: above -60 dBm strong, down to
// -70 good, below -70 weak (where the verdict starts blaming the radio),
// below -80 very weak.
function signalWords(rssi: number | null): { tone: Tone; word: string } {
  if (rssi == null) return { tone: "none", word: "信号强度未知" };
  if (rssi >= -60) return { tone: "good", word: "信号强" };
  if (rssi >= -70) return { tone: "good", word: "信号良好" };
  if (rssi >= -80) return { tone: "warn", word: "信号弱" };
  return { tone: "bad", word: "信号很弱" };
}

const num = (v: number | null | undefined, digits = 0) => (v == null ? "—" : v.toLocaleString(undefined, { maximumFractionDigits: digits }));

// "5,180 MHz", or every link of a Wi-Fi 7 multi-link connection.
function frequency(l: WiFiLink): string {
  if (l.mlo && l.links && l.links.length > 1) {
    return l.links.map((k) => `${num(k.freq_mhz)} MHz${k.rssi_dbm != null ? ` (${k.rssi_dbm} dBm)` : ""}`).join(" + ");
  }
  return l.freq_mhz != null ? `${num(l.freq_mhz)} MHz` : "—";
}

export function WiFiCard({ tuningEnabled }: { tuningEnabled: boolean }) {
  const [v, setV] = useState<WiFiView | null>(null);
  const [failed, setFailed] = useState(false);
  const [surveying, setSurveying] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const alive = useRef(true);

  const poll = useCallback(async () => {
    window.clearTimeout(timer.current);
    try {
      const r = await api.wifi();
      if (!alive.current) return;
      setV(r);
      setFailed(false);
    } catch {
      if (alive.current) setFailed(true);
    }
    if (alive.current) timer.current = window.setTimeout(poll, POLL_MS);
  }, []);

  useEffect(() => {
    alive.current = true;
    poll();
    return () => {
      alive.current = false;
      window.clearTimeout(timer.current);
    };
  }, [poll]);

  const survey = async () => {
    setSurveying(true);
    try {
      const r = await api.wifiSurvey();
      if (alive.current) setV(r);
    } catch {
      if (alive.current) setFailed(true);
    } finally {
      if (alive.current) setSurveying(false);
    }
  };

  const l = v?.state === "ok" ? v.link : null;
  const sig = signalWords(l?.rssi_dbm ?? null);

  return (
    <section className="bloat wifi" aria-label="Wi-Fi 连接" data-testid="wifi-card">
      <div className="bloat-head">
        <span className="bloat-ico">
          <Wifi size={18} />
        </span>
        <div className="txt">
          <div className="t">Wi-Fi 连接</div>
          <div className="d">每隔几秒从系统读取本机到路由器的信号、频段和连接速率。</div>
        </div>
        <button className="btn" onClick={survey} disabled={!l || surveying} title="读取上次扫描得到的接入点负载和邻近 Wi-Fi">
          {surveying ? <Loader2 size={15} className="spin" /> : <Radar size={15} />} 检查信道
        </button>
      </div>

      {!v && !failed && <div className="bloat-empty muted">正在读取 Wi-Fi 连接…</div>}
      {failed && <div className="note-warn bloat-note">— Lowpath 服务暂未响应，正在重试。</div>}

      {v && !l && (
        <div className="bloat-result">
          <div className="bloat-verdict">
            <div className="bloat-grade g-none" aria-label="暂无数据">
              —
            </div>
            <div>
              <div className="t">{v.route === "ethernet" ? "当前未使用 Wi-Fi" : "暂无 Wi-Fi 数据"}</div>
              <div className="d">{v.message || "系统未返回 Wi-Fi 连接信息。"}</div>
            </div>
          </div>
        </div>
      )}

      {l && v && (
        <div className="bloat-result">
          <div className="bloat-verdict">
            <div className={`bloat-grade g-${sig.tone}`} aria-label={l.rssi_dbm != null ? `${l.rssi_dbm} dBm` : "信号未知"}>
              {l.rssi_dbm ?? "—"}
            </div>
            <div>
              <div className="t">
                {sig.word}
                {l.band ? ` on ${l.band} GHz` : ""}
              </div>
              <div className="d">
                {v.message ||
                  (l.band === "2.4"
                    ? "2.4 GHz reaches far but is crowded and slow; a 5 GHz network on the same router usually lags less."
                    : "dBm 越接近 0，信号越强。低于 −70 dBm 时，游戏更容易丢包。")}
              </div>
            </div>
          </div>

          <div className="bloat-stats">
            <Stat k="信号" v={num(l.rssi_dbm)} unit="dBm" s={l.quality_pct != null ? `quality ${l.quality_pct}%` : "—"} />
            <Stat k="信道" v={l.channel != null ? String(l.channel) : "—"} unit="" s={[l.band ? `${l.band} GHz` : "", l.width_mhz ? `带宽 ${l.width_mhz} MHz` : ""].filter(Boolean).join(" · ") || "—"} />
            <Stat k="连接速率" v={num(l.tx_mbps)} unit="Mbit/s" s={l.rx_mbps != null ? `send · ${num(l.rx_mbps)} receive` : "send"} />
          </div>

          <dl className="bloat-facts">
            <dt>网卡</dt>
            <dd>{[l.interface, l.adapter].filter(Boolean).join(" · ") || "—"}</dd>
            <dt>无线标准</dt>
            <dd>
              {[l.standard, l.generation].filter(Boolean).join(" · ") || "—"}
              {l.mlo && ` · multi-link (${l.links?.length ?? 0} links)`}
            </dd>
            <dt>频率</dt>
            <dd>{frequency(l)}</dd>
            <dt>信道负载</dt>
            <dd>
              <SurveyText s={v.survey} />
            </dd>
            <dt>扫描暂停</dt>
            <dd>
              <TuningText t={v.tuning} enabled={tuningEnabled} />
            </dd>
          </dl>
          {l.notes && l.notes.length > 0 && (
            <ul className="wifi-notes faint">
              {l.notes.map((n, i) => (
                <li key={i}>{n}</li>
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  );
}

function SurveyText({ s }: { s: WiFiSurveyView | null }) {
  if (!s) return <>—（点击「检查信道」读取上次扫描结果）</>;
  if (s.state !== "ok" || !s.result) return <>— ({s.message || "未能读取扫描结果"})</>;
  const r = s.result;
  const neighbors =
    r.same_channel === 0
      ? "此信道未发现其他网络"
      : `此信道还有 ${r.same_channel} 个网络` +
        (r.loudest_neighbor_dbm != null ? ` (loudest ${r.loudest_neighbor_dbm} dBm)` : "");
  const overlap = r.overlapping > 0 ? `, ${r.overlapping} overlapping` : "";
  return (
    <>
      {r.load ? (
        <>
          <b>{Math.round(r.load.busy_pct)}% 占用</b> （接入点报告）， {r.load.stations} 台设备{r.load.stations === 1 ? "" : "s"} 在线 ·{" "}
        </>
      ) : (
        <>— 负载（{r.load_note || "未报告"}) · </>
      )}
      {neighbors}
      {overlap}
      <span className="faint"> · {r.networks} 个可见网络</span>
    </>
  );
}

function TuningText({ t, enabled }: { t: WiFiTuning | null; enabled: boolean }) {
  if (!t) return <>{enabled ? "使用 Wi-Fi 加速时暂停后台扫描。" : "已关闭，可在「设置 → 优化」调整。"}</>;
  switch (t.state) {
    case "applying":
      return <>正在暂停后台扫描…</>;
    case "on":
      return <b>已暂停后台扫描，并为本次加速启用串流模式。</b>;
    case "partial":
      return (
        <>
          {t.scan_paused ? "已暂停后台扫描" : "已启用串流模式"}; {t.notes?.join(" ") || "另一项设置未能应用。"}
        </>
      );
    case "failed":
      return <>扫描未暂停： {t.message || t.notes?.join(" ") || "网卡拒绝了操作。"}</>;
    default:
      return <>已恢复。</>;
  }
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
