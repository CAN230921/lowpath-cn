import { useEffect, useRef, useState } from "react";
import { Loader2, ShieldAlert } from "lucide-react";
import { api, describeError } from "../api";
import type { Platform } from "../types";

interface Props {
  platform: Platform;
  down: boolean;
  onOpenNetwork: () => void;
  onError: (msg: string, title?: string) => void;
}

// How long to wait for the elevated copy to take over before assuming the
// administrator prompt was cancelled.
const RESTART_GRACE_MS = 30_000;

// Route mode is selected but capture can't start in this process. Offers a
// restart with administrator rights when the platform can ask for them, and
// proxy mode otherwise. The server decides which (platform.can_elevate), so
// this component has no OS checks.
export function CaptureBanner({ platform, down, onOpenNetwork, onError }: Props) {
  const [state, setState] = useState<"idle" | "busy" | "restarting">("idle");
  const sawDown = useRef(false);
  const canElevate = platform.can_elevate && !platform.admin;

  useEffect(() => {
    if (down) sawDown.current = true;
  }, [down]);

  useEffect(() => {
    if (state !== "restarting") return;
    const t = window.setTimeout(() => {
      // If the service went away, the elevated copy is starting; the
      // bootstrap refresh after reconnect decides what to show.
      if (sawDown.current) return;
      setState("idle");
      onError("Lowpath 尚未获得路由模式所需的权限。若刚才取消了授权，可重试。", "尚未重启");
    }, RESTART_GRACE_MS);
    return () => window.clearTimeout(t);
  }, [state, onError]);

  const elevate = async () => {
    setState("busy");
    sawDown.current = false;
    try {
      await api.elevate();
      setState("restarting");
    } catch (e) {
      setState("idle");
      const d = describeError(e, "无法以提升后的权限重启");
      onError(d.msg, d.title);
    }
  };

  // The server words the restart for its OS (UAC, polkit, the macOS
  // administrator dialog).
  const elevateHint = platform.elevate_hint || "请以管理员身份重启 Lowpath。";
  const reason = platform.capture.reason || (canElevate ? "需要管理员权限" : "流量捕获驱动不可用");

  return (
    <div className="banner banner-warn" role="alert">
      <ShieldAlert size={16} style={{ flex: "none" }} />
      <span style={{ flex: 1 }}>
        <b>路由模式无法捕获游戏流量：</b> {reason}.{" "}
        {canElevate ? (
          platform.service?.state === "not_installed" ? (
            // Only where `lowpathsvc install` works (Windows today): the
            // other OSes report "unsupported".
            <>
              {elevateHint.replace(/\.$/, "")}，或用以下命令安装后台服务：{" "}
              <code className="inline-code">lowpathsvc install</code> ，后续便无需重复授权。
            </>
          ) : (
            elevateHint
          )
        ) : (
          <>
            请改用{" "}
            <button className="link" onClick={onOpenNetwork}>
              代理模式
            </button>{" "}
            。
          </>
        )}
      </span>
      {canElevate && (
        <button className="btn btn-xs" onClick={elevate} disabled={state !== "idle"} style={{ flex: "none" }}>
          {state !== "idle" && <Loader2 size={13} className="spin" />}
          {state === "restarting" ? "正在重启…" : platform.elevate_action || "以管理员身份重启"}
        </button>
      )}
    </div>
  );
}
