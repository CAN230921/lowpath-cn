import { ShieldCheck, X } from "lucide-react";
import type { Failsafe } from "../types";

// Shown after a boost ended without the user asking: the capture failed
// open during this run ("stopped"), or the last run was closed or crashed
// mid-boost and this start cleaned up ("recovered"). The message comes from
// the service; the game is on the normal connection either way.
export function FailsafeBanner({ failsafe, onDismiss }: { failsafe: Failsafe; onDismiss: () => void }) {
  const title = failsafe.kind === "recovered" ? "上次加速意外中断" : "已停止加速以恢复游戏连接";
  return (
    <div className="banner banner-warn" role="alert" aria-label="加速结束">
      <ShieldCheck size={16} style={{ flex: "none" }} />
      <span style={{ flex: 1 }}>
        <b>{title}.</b> {failsafe.message}
        {failsafe.detail && <span className="banner-detail"> {failsafe.detail}</span>} 点击「加速」重新开始。
      </span>
      <button className="btn btn-ghost icon-btn" onClick={onDismiss} aria-label="关闭提示" title="关闭提示" style={{ flex: "none", width: 24, height: 24 }}>
        <X size={14} />
      </button>
    </div>
  );
}
