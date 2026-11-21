import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { ChevronDown, ChevronUp, CircleAlert, CircleCheck, CircleDashed, Download, Stethoscope } from "lucide-react";
import { api } from "../api";
import type { CheckStatus, Confidence, Diagnosis, RouteSegment, VerdictCheck, VerdictId } from "../types";
import "../diag.css";

// The verdict: one plain answer to "why am I lagging?" built from the tests
// below, the running boost and the last route analysis. It only reads what
// was measured, so it refreshes on its own as tests finish; a check with
// nothing to go on says "Not measured" and what to run, never a guess.

const POLL_MS = 4000;

type Tone = "good" | "warn" | "bad" | "none";

function verdictTone(id: VerdictId, c: Confidence): Tone {
  switch (id) {
    case "optimal":
    case "clean":
      return "good";
    case "unknown":
      return "none";
    default:
      return c === "low" ? "warn" : "bad";
  }
}

function checkTone(c: VerdictCheck): Tone {
  if (c.status === "unknown") return "none";
  if (c.rule === "optimal") return "good"; // either answer is good news
  return c.status === "found" ? "bad" : "good";
}

function checkLabel(c: VerdictCheck): string {
  const labels: Record<CheckStatus, string> = { found: "异常", clear: "正常", unknown: "未测量" };
  if (c.rule === "optimal" && c.status === "found") return "无需加速";
  if (c.rule === "optimal" && c.status === "clear") return "中继有改善";
  return labels[c.status];
}

const CONFIDENCE: Record<Confidence, string> = { high: "可信度高", medium: "可信度中等", low: "可信度低" };

// renderSegments draws the route part by part under the verdict (the
// Diagnostics tab passes its segment bar).
export function Verdict({ renderSegments }: { renderSegments?: (s: RouteSegment[]) => ReactNode } = {}) {
  const [d, setD] = useState<Diagnosis | null>(null);
  const [failed, setFailed] = useState(false);
  // The measurements under each check are long; they open on request.
  const [details, setDetails] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const alive = useRef(true);

  const poll = useCallback(async () => {
    window.clearTimeout(timer.current);
    try {
      const v = await api.verdict();
      if (!alive.current) return;
      setD(v);
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

  const v = d?.verdict;
  // The first check that found something gave the verdict; its facts are
  // already shown under the verdict.
  const winner = d?.checks?.find((c) => c.status === "found")?.rule;
  const tone = v ? verdictTone(v.id, v.confidence) : "none";

  return (
    <section className="bloat verdict" aria-label="诊断结论">
      <div className="bloat-head">
        <span className="bloat-ico">
          <Stethoscope size={18} />
        </span>
        <div className="txt">
          <div className="t">游戏为什么卡顿？</div>
          <div className="d">根据诊断测试、当前加速和最近一次测量生成，随测试结果更新。</div>
        </div>
        <a className="btn" href="/api/diag/verdict/report" download="lowpath-diagnosis.txt" aria-disabled={!d}>
          <Download size={15} /> 导出报告
        </a>
      </div>

      {!d && !failed && <div className="bloat-empty muted">正在读取测量结果…</div>}
      {!d && failed && <div className="note-warn bloat-note">— 暂时无法读取诊断结论，系统会重试。</div>}
      {d && failed && (
        <div className="note-warn bloat-note">Lowpath 服务暂未响应。结论可能已过期，系统会重试。</div>
      )}

      {v && d && (
        <>
          <div className={`verdict-main t-${tone}`} role="status" aria-live="polite">
            <div className="verdict-title">
              <span className="t">{v.title}</span>
              <span className={`tag verdict-conf c-${v.confidence}`}>{CONFIDENCE[v.confidence]}</span>
              {v.lowpath_helps && <span className="tag tag-accent">适合使用加速</span>}
            </div>
            <div className="verdict-detail">{v.detail}</div>
            {v.advice && (
              <div className="verdict-advice">
                <b>处理建议</b> {v.advice}
              </div>
            )}
            {v.facts && v.facts.length > 0 && (
              <ul className="verdict-facts" aria-label="原因">
                {v.facts.map((f, i) => (
                  <li key={i}>{f}</li>
                ))}
              </ul>
            )}
          </div>

          {renderSegments && d.segments && d.segments.length > 0 && renderSegments(d.segments)}

          <ul className="verdict-checks" aria-label="检查项目">
            {(d.checks ?? []).map((c) => (
              <CheckRow key={c.rule} c={c} showFacts={details && c.rule !== winner} />
            ))}
          </ul>

          {(d.checks ?? []).some((c) => c.rule !== winner && c.facts && c.facts.length > 0) && (
            <button className="btn btn-ghost verdict-more" onClick={() => setDetails(!details)} aria-expanded={details}>
              {details ? <ChevronUp size={14} /> : <ChevronDown size={14} />} {details ? "隐藏测量结果" : "查看各项测量结果"}
            </button>
          )}
        </>
      )}
    </section>
  );
}

function CheckRow({ c, showFacts }: { c: VerdictCheck; showFacts: boolean }) {
  const tone = checkTone(c);
  const Icon = tone === "none" ? CircleDashed : tone === "bad" ? CircleAlert : CircleCheck;
  return (
    <li className={`verdict-check k-${tone}`} data-rule={c.rule} data-status={c.status}>
      <Icon size={16} className="ico" aria-hidden />
      <div className="body">
        <div className="head">
          <span className="name">{c.name}</span>
          <span className="state">{checkLabel(c)}</span>
        </div>
        <div className="sum">{c.summary}</div>
        {showFacts && c.facts && c.facts.length > 0 && (
          <ul className="facts">
            {c.facts.map((f, i) => (
              <li key={i}>{f}</li>
            ))}
          </ul>
        )}
      </div>
    </li>
  );
}
