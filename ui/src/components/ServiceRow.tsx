import type { ReactNode } from "react";
import { ShieldCheck } from "lucide-react";
import type { Platform } from "../types";

// The background service (LowpathSvc) as the server sees it: whether this
// window goes through it, and if not, what to do about it. Hidden where the
// OS has no service support yet.
export function ServiceRow({ platform, demo }: { platform: Platform | null; demo: boolean }) {
  const svc = platform?.service;
  if (!svc || svc.state === "unsupported") return null;
  const view = describe(svc.state, svc.message, demo);
  return (
    <div className="setting" data-testid="service-row">
      <span className="muted">
        <ShieldCheck size={16} />
      </span>
      <div className="txt">
        <div className="t">
          后台服务
          <span className={`tag ${view.tag}`}>{view.label}</span>
        </div>
        <div className="d">{view.text}</div>
      </div>
    </div>
  );
}

function describe(state: string, message: string | undefined, demo: boolean): { label: string; tag: string; text: ReactNode } {
  switch (state) {
    case "in_use":
      return {
        label: "已启用",
        tag: "tag-accent",
        text: "后台服务负责流量捕获和路由，应用窗口无需再次请求管理员权限。",
      };
    // For running and stopped, message is the window's own reason for not
    // using the service (it wouldn't start, didn't answer, -no-service).
    case "running":
      return {
        label: "未使用",
        tag: "",
        text: demo
          ? "后台服务已运行。当前窗口使用演示模式的模拟网络。"
          : `后台服务已运行，当前窗口使用独立引擎${message ? `：${message}` : ""}。`,
      };
    case "stopped":
      return {
        label: "已停止",
        tag: "",
        text: message
          ? `后台服务已安装但未运行。当前窗口使用独立引擎：${message}。`
          : "后台服务已安装，下次启动 Lowpath 时会尝试启动。",
      };
    case "not_installed":
      return {
        label: "未安装",
        tag: "",
        text: (
          <>
            路由模式需要管理员权限。可使用以下命令安装后台服务： <code className="inline-code">lowpathsvc install</code> （仅需一次管理员授权）。
          </>
        ),
      };
    default:
      return { label: "—", tag: "", text: `无法查询 Windows 服务${message ? `：${message}` : ""}。` };
  }
}
