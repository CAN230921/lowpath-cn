import { AlertTriangle, CheckCircle2, Repeat, X, Zap } from "lucide-react";

export interface Toast {
  id: number;
  kind: "applied" | "switch" | "degraded" | "fallback" | "recovered" | "error" | "success";
  msg: string;
  title?: string; // overrides the kind's default title
}

const META: Record<Toast["kind"], { title: string; color: string; Icon: typeof Zap }> = {
  applied: { title: "线路已应用", color: "var(--accent)", Icon: Zap },
  switch: { title: "线路已切换", color: "var(--info)", Icon: Repeat },
  degraded: { title: "线路质量下降", color: "var(--warn)", Icon: AlertTriangle },
  fallback: { title: "线路中断", color: "var(--bad)", Icon: AlertTriangle },
  recovered: { title: "线路已恢复", color: "var(--good)", Icon: CheckCircle2 },
  error: { title: "操作失败", color: "var(--bad)", Icon: AlertTriangle },
  success: { title: "完成", color: "var(--good)", Icon: CheckCircle2 },
};

export function Toasts({ toasts, onDismiss }: { toasts: Toast[]; onDismiss: (id: number) => void }) {
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((t) => {
        const m = META[t.kind];
        return (
          <div className="toast" key={t.id} role={t.kind === "fallback" || t.kind === "error" ? "alert" : "status"}>
            <m.Icon size={17} color={m.color} className="ico" />
            <div className="body">
              <div className="ttl">{t.title ?? m.title}</div>
              <div className="msg">{t.msg}</div>
            </div>
            <button className="btn btn-ghost icon-btn" style={{ width: 24, height: 24 }} onClick={() => onDismiss(t.id)} aria-label="关闭提示">
              <X size={14} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
