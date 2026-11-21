import { useState, type ReactNode } from "react";
import { Activity, AlertTriangle, Check, Cpu, Download, Gauge, Network, Server, SlidersHorizontal, Wifi, Wrench, X } from "lucide-react";
import { api } from "../api";
import type { CaptureMode, Platform, RelayView, Settings } from "../types";
import { Modal, Toggle } from "./Bits";
import { Connection } from "./Connection";
import { OtherSoftware } from "./Conflicts";
import { RelaysPane } from "./RelaysPane";
import { ServiceRow } from "./ServiceRow";
import { Diagnostics } from "./Diagnostics";

export type SettingsTab = "general" | "network" | "relays" | "diagnostics" | "optimization" | "advanced";

interface Props {
  initialTab?: SettingsTab;
  settings: Settings;
  relays: RelayView[];
  platform: Platform | null;
  version: string;
  demo: boolean;
  managed?: boolean;
  onChange: (s: Settings) => void;
  onRelaysChanged: () => void;
  onRemap: () => void;
  onClose: () => void;
  onError: (msg: string, title?: string) => void;
}

export function SettingsModal(props: Props) {
  const { initialTab, settings, relays, platform, version, demo, managed, onChange, onRelaysChanged, onRemap, onClose, onError } = props;
  const [tab, setTab] = useState<SettingsTab>(initialTab ?? "network");
  const [saved, setSaved] = useState(false);

  const set = async (patch: Partial<Settings>) => {
    const next = { ...settings, ...patch };
    onChange(next); // optimistic
    try {
      onChange(await api.saveSettings(next));
      setSaved(true);
      window.setTimeout(() => setSaved(false), 1400);
    } catch {
      onChange(settings);
    }
  };

  const tabs: { id: SettingsTab; label: string; icon: ReactNode }[] = [
    { id: "general", label: "通用", icon: <SlidersHorizontal size={16} /> },
    { id: "network", label: "网络", icon: <Network size={16} /> },
    { id: "relays", label: "中继节点", icon: <Server size={16} /> },
    { id: "diagnostics", label: "诊断", icon: <Activity size={16} /> },
    { id: "optimization", label: "优化", icon: <Gauge size={16} /> },
    { id: "advanced", label: "高级", icon: <Wrench size={16} /> },
  ];

  const captureMode: CaptureMode = settings.capture_mode ?? "proxy";

  return (
    <Modal title="设置" onClose={onClose} className="settings">
      <div className="modal-head">
        <h2>设置</h2>
        {saved && (
          <span className="saved" style={{ marginLeft: 12 }}>
            <Check size={13} /> 已保存
          </span>
        )}
        <button className="btn btn-ghost icon-btn close" onClick={onClose} aria-label="关闭设置">
          <X size={18} />
        </button>
      </div>
      <div className="settings-body">
        <nav className="settings-nav" role="tablist" aria-orientation="vertical">
          {tabs.map((t) => (
            <button key={t.id} role="tab" aria-selected={tab === t.id} onClick={() => setTab(t.id)}>
              {t.icon}
              {t.label}
            </button>
          ))}
        </nav>
        <div className="settings-pane" role="tabpanel" key={tab} aria-label={tabs.find((t) => t.id === tab)?.label}>
          {tab === "general" && (
            <>
              <h3>通用</h3>
              <Row t="主题" d="默认使用深色主题。">
                <div className="segmented">
                  {(["dark", "light"] as const).map((th) => (
                    <button key={th} role="radio" aria-checked={settings.theme === th} onClick={() => set({ theme: th })}>
                      {th === "dark" ? "深色" : "浅色"}
                    </button>
                  ))}
                </div>
              </Row>
              <Row t="随 Windows 启动" d="登录 Windows 后自动启动 Lowpath。" soon>
                <Toggle label="开机启动" checked={false} disabled onChange={() => {}} />
              </Row>
              <Row t="最小化到系统托盘" d="关闭窗口后继续在系统托盘运行。" soon>
                <Toggle label="最小化到系统托盘" checked={false} disabled onChange={() => {}} />
              </Row>
              <Row t="语言" d="当前界面语言。">
                <span className="chip">简体中文</span>
              </Row>
            </>
          )}

          {tab === "network" && (
            <>
              <h3>网络</h3>
              <Connection />
              {!demo && <OtherSoftware />}
              <div className="muted" style={{ fontSize: 12.5, marginTop: 10, marginBottom: 6 }}>
                设置在下次启动加速时生效。
              </div>
              <div className="setting" style={{ flexDirection: "column", alignItems: "stretch", gap: 10 }}>
                <div className="txt">
                  <div className="t" id="capture-mode-label">
                    流量捕获模式
                  </div>
                  <div className="d">选择游戏流量进入 Lowpath 的方式。{demo ? " 演示游戏始终使用代理模式。" : ""}</div>
                </div>
                <div className="choices" role="radiogroup" aria-labelledby="capture-mode-label">
                  {CAPTURE_MODES.map((m) => (
                    <button key={m.id} className="choice" role="radio" aria-checked={captureMode === m.id} onClick={() => set({ capture_mode: m.id })}>
                      <span className="radio" />
                      <span>
                        <span className="t">
                          {m.title}
                          {m.id === "route" && <span className="tag tag-accent">推荐</span>}
                        </span>
                        <span className="d">{m.desc(platform?.os)}</span>
                      </span>
                    </button>
                  ))}
                </div>
                {captureMode === "route" && platform && !platform.capture.available && !demo && (
                  <div className="note-warn">
                    <AlertTriangle size={14} />
                    <span>当前无法启动路由模式： {platform.capture.reason || "流量捕获驱动不可用"}.</span>
                  </div>
                )}
              </div>
              <ServiceRow platform={platform} demo={demo} />
              <Row t="封包冗余" d="设置每个包的发送副本数。更多副本会占用更多上传带宽。自动模式按实测丢包率选择 2–4 份。">
                <div className="segmented">
                  {([0, 1, 2, 4] as const).map((v) => (
                    <button key={v} role="radio" aria-checked={settings.dup_level === v} onClick={() => set({ dup_level: v })}>
                      {{ 0: "自动", 1: "关闭", 2: "均衡", 4: "最大" }[v]}
                    </button>
                  ))}
                </div>
              </Row>
              <Row t="自动切换线路" d="运行中的线路质量下降时，切换到明显更好的线路。">
                <Toggle label="自动切换线路" checked={settings.auto_switch} onChange={(v) => set({ auto_switch: v })} />
              </Row>
              <Row t="断线保护" d="所有加速线路中断时暂停游戏流量。">
                <Toggle label="断线保护" checked={!!managed || settings.kill_switch} disabled={!!managed} onChange={(v) => set({ kill_switch: v })} />
              </Row>
              <Row t="选择游戏时测量" d="选择游戏后自动探测所有可用线路。">
                <Toggle label="自动测量" checked={settings.auto_analyze} onChange={(v) => set({ auto_analyze: v })} />
              </Row>
              <Row t="游戏退出后停止加速" d="检测到游戏进程退出时自动清理加速线路。">
                <Toggle label="随游戏退出停用" checked={settings.stop_on_game_exit} onChange={(v) => set({ stop_on_game_exit: v })} />
              </Row>
            </>
          )}

          {tab === "relays" && <RelaysPane managed={managed} relays={relays} settings={settings} onSettings={set} onRelaysChanged={onRelaysChanged} />}

          {tab === "diagnostics" && <Diagnostics onError={onError} wifiTuning={settings.wifi_tuning ?? true} />}

          {tab === "optimization" && (
            <>
              <h3>优化</h3>
              <Row
                t="加速时暂停 Wi-Fi 扫描"
                d="Wi-Fi 网卡扫描其他网络时可能产生周期性延迟尖峰。加速期间暂停扫描并启用串流模式，结束加速、重连 Wi-Fi 或关闭客户端时恢复。"
                icon={<Wifi size={18} />}
              >
                <Toggle label="加速时暂停 Wi-Fi 扫描" checked={settings.wifi_tuning ?? true} onChange={(v) => set({ wifi_tuning: v })} />
              </Row>
              <Row t="流量整形" d="优先处理游戏流量。" soon icon={<Gauge size={18} />}>
                <Toggle label="流量整形" checked={false} disabled onChange={() => {}} />
              </Row>
              <Row t="帧率优化" d="调整游戏进程优先级、电源计划和游戏模式。" soon icon={<Cpu size={18} />}>
                <Toggle label="帧率优化" checked={false} disabled onChange={() => {}} />
              </Row>
              <Row t="多网络接入" d="同时使用两条网络传输游戏流量，例如有线网络和 4G。" soon icon={<Network size={18} />}>
                <Toggle label="多网络接入" checked={false} disabled onChange={() => {}} />
              </Row>
            </>
          )}

          {tab === "advanced" && (
            <>
              <h3>高级</h3>
              <Row t="诊断报告" d="以 JSON 导出设置、上次测量和实时统计。报告省略中继密钥。">
                <a className="btn" href="/api/diagnostics" download="lowpath-report.json">
                  <Download size={15} /> 导出
                </a>
              </Row>
              <Row t="重新测量线路" d="重新测量所有中继的延迟。">
                <button className="btn" onClick={onRemap}>
                  重新测量
                </button>
              </Row>
              <Row t="关于" d={`Lowpath v${version} · ${demo ? "模拟网络" : managed ? "托管中继" : "自定义中继"}`}>
                <span />
              </Row>
            </>
          )}
        </div>
      </div>
    </Modal>
  );
}

// desc takes the platform's OS: route mode needs different rights on each.
const CAPTURE_MODES: { id: CaptureMode; title: string; desc: (os?: string) => string }[] = [
  {
    id: "route",
    title: "路由模式",
    desc: (os) =>
      "自动捕获游戏流量，游戏设置保持原样。" +
      (os === "linux" ? "需要 root 权限，或 CAP_NET_ADMIN 和 CAP_NET_RAW 能力。" : "需要管理员权限。"),
  },
  {
    id: "proxy",
    title: "代理模式",
    desc: () => "将游戏连接到本地 Lowpath 端口，适用于可以填写服务器地址的游戏。",
  },
];

function Row({ t, d, soon, icon, children }: { t: string; d: string; soon?: boolean; icon?: ReactNode; children: ReactNode }) {
  return (
    <div className="setting">
      {icon && <span className="muted">{icon}</span>}
      <div className="txt">
        <div className="t">
          {t}
          {soon && <span className="soon">尚未提供</span>}
        </div>
        <div className="d">{d}</div>
      </div>
      {children}
    </div>
  );
}
