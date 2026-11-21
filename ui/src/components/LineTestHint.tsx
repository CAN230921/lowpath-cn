import { Gauge, X } from "lucide-react";

const DISMISSED_KEY = "lowpath.lineTestHint";

export function lineTestHintDismissed(): boolean {
  try {
    return localStorage.getItem(DISMISSED_KEY) === "1";
  } catch {
    return false;
  }
}

// Shown during a boost while no bufferbloat test has measured the line:
// the passive detector then compares other traffic with the most it saw
// in the last 10 minutes, a guess that can keep the "something is filling
// the connection" banner from showing. The test can't run during a boost,
// so this only points to it.
export function LineTestHint({ onOpen, onDismiss }: { onOpen: () => void; onDismiss: () => void }) {
  const dismiss = () => {
    try {
      localStorage.setItem(DISMISSED_KEY, "1");
    } catch {
      // Only a convenience; it still hides for this run.
    }
    onDismiss();
  };
  return (
    <div className="banner banner-info" role="status" aria-label="建议测试缓冲膨胀">
      <Gauge size={16} style={{ flex: "none" }} />
      <span style={{ flex: 1 }}>
        <b>尚未测量当前线路的速度。</b> 测过线路速度后，Lowpath 能更准确地识别带宽占用引起的卡顿。请在游戏结束后到「诊断」运行测试。
      </span>
      <button className="btn btn-sm" onClick={onOpen} style={{ flex: "none" }}>
        打开诊断
      </button>
      <button className="btn btn-ghost icon-btn" onClick={dismiss} aria-label="关闭提示" title="关闭提示" style={{ flex: "none", width: 24, height: 24 }}>
        <X size={14} />
      </button>
    </div>
  );
}
