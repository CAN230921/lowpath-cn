import { useEffect, useRef, useState } from "react";
import { Radar } from "lucide-react";
import { api } from "../api";
import type { Mapping as MappingResult } from "../types";
import { ms } from "../util";

// The startup "mapping the best routes for you" step: one latency sweep
// to every relay before the main window opens.
export function Mapping({ onDone }: { onDone: (m: MappingResult | null) => void }) {
  const [progress, setProgress] = useState(4);
  const [result, setResult] = useState<MappingResult | null>(null);
  const [failed, setFailed] = useState(false);
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    const t0 = performance.now();
    const tick = window.setInterval(() => {
      // Ease toward 92% while the sweep runs (it takes ~2.2 s).
      const p = 92 * (1 - Math.exp(-(performance.now() - t0) / 900));
      setProgress((cur) => Math.max(cur, p));
    }, 80);
    api
      .mapping()
      .then((m) => {
        setResult(m);
        setProgress(100);
        window.setTimeout(() => onDone(m), 650);
      })
      .catch(() => {
        setFailed(true);
        window.setTimeout(() => onDone(null), 1200);
      })
      .finally(() => window.clearInterval(tick));
    return () => window.clearInterval(tick);
  }, [onDone]);

  return (
    <div className="mapping">
      <div className="mapping-inner">
        <div className="radar">
          <Radar size={36} />
        </div>
        <h2 style={{ margin: "26px 0 4px", fontSize: 19 }}>{result ? "线路测量完成" : "正在测量可用线路…"}</h2>
        <div className="muted">
          {failed
            ? "无法连接中继。你可以继续浏览，稍后重新测量。"
            : result
              ? `已探测 ${result.relays} 个节点 · 最佳：${result.best}（${ms(result.best_ms)} ms${result.loaded ? "，在缓冲膨胀测试期间测量" : ""}）`
              : "正在测量各中继的延迟"}
        </div>
        <div className="progress" aria-label="线路测量进度" role="progressbar" aria-valuenow={Math.round(progress)} aria-valuemin={0} aria-valuemax={100}>
          <div style={{ width: `${progress}%` }} />
        </div>
      </div>
    </div>
  );
}
