import { useState, type FormEvent, type ReactNode } from "react";
import { Check, Copy, KeyRound, Loader2, Plus, Server, Trash2, Wifi } from "lucide-react";
import { api, ApiError } from "../api";
import type { RelayTest, RelayView, Settings } from "../types";
import { ms, pct } from "../util";
import { Toggle } from "./Bits";

const DEPLOY_CMD = "powershell -ExecutionPolicy Bypass -File deploy\\deploy-relay.ps1 -Server root@YOUR_VPS_IP";
const CONTINENTS = ["Europe", "North America", "South America", "Asia", "Middle East", "Oceania", "Africa"];
const REGION_NAMES: Record<string, string> = {
  Europe: "欧洲", "North America": "北美洲", "South America": "南美洲", Asia: "亚洲",
  "Middle East": "中东", Oceania: "大洋洲", Africa: "非洲",
};

interface Props {
  managed?: boolean;
  relays: RelayView[];
  settings: Settings;
  onSettings: (patch: Partial<Settings>) => Promise<void>;
  onRelaysChanged: () => void;
}

type TestState = { busy: boolean; result?: RelayTest; error?: string };

// An egress relay is one whose traffic leaves from itself: either it names
// no egress or names its own id.
const isEgress = (r: RelayView) => !r.egress || r.egress === r.id;

export function RelaysPane({ managed, relays, settings, onSettings, onRelaysChanged }: Props) {
  const [tests, setTests] = useState<Record<string, TestState>>({});
  const [deleting, setDeleting] = useState<string | null>(null);
  const [listError, setListError] = useState<string | null>(null);
  const [loopbackBusy, setLoopbackBusy] = useState(false);

  const test = async (id: string) => {
    setTests((t) => ({ ...t, [id]: { busy: true } }));
    try {
      const result = await api.testRelay({ id });
      setTests((t) => ({ ...t, [id]: { busy: false, result } }));
    } catch (e) {
      setTests((t) => ({ ...t, [id]: { busy: false, error: e instanceof ApiError ? e.message : "测试失败" } }));
    }
  };

  const remove = async (r: RelayView) => {
    if (!window.confirm(`移除中继「${r.label || r.addr}」？`)) return;
    setDeleting(r.id);
    setListError(null);
    try {
      await api.deleteRelay(r.id);
      onRelaysChanged();
    } catch (e) {
      setListError(e instanceof ApiError ? e.message : "无法移除中继");
    } finally {
      setDeleting(null);
    }
  };

  const setLoopback = async (on: boolean) => {
    setLoopbackBusy(true);
    try {
      await onSettings({ loopback_relay: on });
    } finally {
      setLoopbackBusy(false);
      onRelaysChanged();
    }
  };

  const byId = new Map(relays.map((r) => [r.id, r]));
  const egresses = relays.filter((r) => isEgress(r) && !r.builtin);

  return (
    <>
      <h3>中继节点</h3>
      <div className="muted" style={{ fontSize: 12.5, marginBottom: 12 }}>
        {managed ? "通过设备授权访问下列加密节点。" : "这些中继负责转发游戏流量。共享同一出口的中继会归在一起。"}
      </div>

      {listError && (
        <div className="form-error" role="alert" style={{ marginBottom: 10 }}>
          {listError}
        </div>
      )}

      <div className="relay-list" role="list" aria-label="中继节点">
        {relays.length === 0 && <div className="empty-list" style={{ padding: "18px 12px" }}>{managed ? "暂无可用节点，请检查设备授权。" : "暂无可用节点。可在下方添加中继或启用本机测试中继。"}</div>}
        {relays.map((r) => {
          const t = tests[r.id];
          const via = !isEgress(r) ? byId.get(r.egress) : undefined;
          return (
            <div className="relay-row" role="listitem" key={r.id}>
              <span className="relay-ico" aria-hidden="true">
                {r.builtin ? <Wifi size={16} /> : <Server size={16} />}
              </span>
              <div className="relay-txt">
                <div className="t">
                  {r.label || r.addr}
                  {r.builtin && <span className="tag">{managed ? "托管节点" : "内置"}</span>}
                  {managed ? <span className="tag tag-accent"><KeyRound size={10} />MASQUE 加密</span> : <span className={`tag ${r.has_key ? "tag-accent" : ""}`} title={r.has_key ? "数据包会携带此中继的访问令牌" : "开放中继，无访问密钥"}>
                    <KeyRound size={10} /> {r.has_key ? "有密钥" : "无密钥"}
                  </span>}
                </div>
                <div className="d">
                  <span className="num">{r.addr}</span>
                  {[r.region, r.continent].filter(Boolean).length > 0 && <> · {[r.region, REGION_NAMES[r.continent] ?? r.continent].filter(Boolean).join("、")}</>}
                  {!isEgress(r) && <> · 转发到 {via ? via.label || via.addr : r.egress}</>}
                </div>
                <TestResult state={t} />
              </div>
              <div className="relay-actions">
                <button className="btn btn-xs" onClick={() => test(r.id)} disabled={t?.busy} aria-label={`测试 ${r.label || r.addr}`}>
                  {t?.busy ? <Loader2 size={13} className="spin" /> : null}
                  测试
                </button>
                {!r.builtin && (
                  <button className="btn btn-xs btn-ghost" onClick={() => remove(r)} disabled={deleting === r.id} aria-label={`删除 ${r.label || r.addr}`} title="删除">
                    {deleting === r.id ? <Loader2 size={13} className="spin" /> : <Trash2 size={14} />}
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {!managed && <><div className="setting">
        <div className="txt">
          <div className="t">使用本机测试中继</div>
          <div className="d">
            在 127.0.0.1 运行测试中继，用于验证本机捕获、转发和回注。游戏流量仍由本机网络发出。
          </div>
        </div>
        <Toggle label="使用本机测试中继" checked={!!settings.loopback_relay} disabled={loopbackBusy} onChange={setLoopback} />
      </div>

      <AddRelayForm egresses={egresses} onAdded={onRelaysChanged} />

      <div className="help-box">
        <div className="t">部署自有中继</div>
        <div className="d">
          准备一台离游戏服务器较近、可通过 SSH 登录的 Linux VPS（x64 或 ARM）。在包含 Lowpath.exe 和{" "}
          <span className="num">deploy</span> 的目录中运行以下命令：
        </div>
        <CopyLine text={DEPLOY_CMD} />
        <div className="d">安装程序会将中继设为系统服务，并输出地址和密钥。将它们填入上方表单。</div>
      </div></>}
    </>
  );
}

function TestResult({ state }: { state?: TestState }) {
  if (!state || state.busy) return null;
  if (state.error) return <div className="relay-test q-bad">{state.error}</div>;
  const r = state.result;
  if (!r) return null;
  return (
    <div className={`relay-test ${r.ok ? "q-good" : "q-bad"}`} role="status">
      {r.ok ? (
        <span className="num">
          {ms(r.rtt_ms)} ms · {pct(r.loss_pct)}% 丢包
        </span>
      ) : (
        "无回包"
      )}
      {r.message && <span className="muted"> · {r.message}</span>}
    </div>
  );
}

function AddRelayForm({ egresses, onAdded }: { egresses: RelayView[]; onAdded: () => void }) {
  const empty = { label: "", addr: "", key: "", region: "", continent: "", egress: "" };
  const [f, setF] = useState(empty);
  const [busy, setBusy] = useState<"add" | "test" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [test, setTest] = useState<TestState | undefined>();
  const [added, setAdded] = useState(false);

  const field = (k: keyof typeof empty) => ({
    value: f[k],
    onChange: (e: { target: { value: string } }) => {
      setF((x) => ({ ...x, [k]: e.target.value }));
      setAdded(false);
    },
  });

  // host:port with a numeric port; IPv6 literals must be bracketed.
  const addrOk = /^(\[[0-9a-fA-F:.]+\]|[^\s:[\]]+):\d{1,5}$/.test(f.addr.trim());

  const runTest = async () => {
    setError(null);
    setBusy("test");
    setTest({ busy: true });
    try {
      setTest({ busy: false, result: await api.testRelay({ addr: f.addr.trim(), key: f.key.trim() }) });
    } catch (e) {
      setTest({ busy: false, error: e instanceof ApiError ? e.message : "测试失败" });
    } finally {
      setBusy(null);
    }
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!addrOk) return setError("请输入主机名或 IP 与端口，例如 203.0.113.10:40000。");
    setBusy("add");
    try {
      await api.addRelay({
        label: f.label.trim() || f.addr.trim(),
        addr: f.addr.trim(),
        key: f.key.trim(),
        region: f.region.trim(),
        continent: f.continent.trim(),
        egress: f.egress,
      });
      setF(empty);
      setTest(undefined);
      setAdded(true);
      onAdded();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "无法添加中继");
    } finally {
      setBusy(null);
    }
  };

  return (
    <form className="relay-form" onSubmit={submit} noValidate aria-label="添加中继">
      <div className="t">添加中继</div>
      {error && (
        <div className="form-error" role="alert">
          {error}
        </div>
      )}
      <div className="form-grid">
        <Field id="relay-label" label="名称">
          <input id="relay-label" className="input" placeholder="法兰克福 VPS" autoComplete="off" {...field("label")} />
        </Field>
        <Field id="relay-addr" label="地址（主机:端口）">
          <input id="relay-addr" className="input num" placeholder="203.0.113.10:40000" autoComplete="off" spellCheck={false} required aria-invalid={!!f.addr && !addrOk} {...field("addr")} />
        </Field>
        <Field id="relay-key" label="访问密钥">
          <input id="relay-key" className="input num" placeholder="由 deploy-relay.ps1 提供（可选）" autoComplete="off" spellCheck={false} {...field("key")} />
        </Field>
        <Field id="relay-egress" label="转发至出口">
          <select id="relay-egress" className="input" {...field("egress")}>
            <option value="">本节点是出口中继</option>
            {egresses.map((r) => (
              <option key={r.id} value={r.id}>
                {r.label || r.addr}
              </option>
            ))}
          </select>
        </Field>
        <Field id="relay-region" label="地区">
          <input id="relay-region" className="input" placeholder="EU-Central" autoComplete="off" {...field("region")} />
        </Field>
        <Field id="relay-continent" label="区域">
          <input id="relay-continent" className="input" list="relay-continents" placeholder="Europe" autoComplete="off" {...field("continent")} />
          <datalist id="relay-continents">
            {CONTINENTS.map((c) => (
              <option key={c} value={c} />
            ))}
          </datalist>
        </Field>
      </div>
      <div className="relay-form-foot">
        <TestResult state={test} />
        {added && !test && (
          <span className="saved">
            <Check size={13} /> 中继已添加
          </span>
        )}
        <span className="spacer" />
        <button type="button" className="btn" onClick={runTest} disabled={!!busy || !addrOk}>
          {busy === "test" && <Loader2 size={15} className="spin" />}
          测试
        </button>
        <button type="submit" className="btn btn-primary" disabled={!!busy || !f.addr.trim()}>
          {busy === "add" ? <Loader2 size={15} className="spin" /> : <Plus size={15} />}
          添加中继
        </button>
      </div>
    </form>
  );
}

function Field({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {children}
    </div>
  );
}

function CopyLine({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    } catch {
      // Clipboard can be blocked; the command is selectable text anyway.
    }
  };
  return (
    <div className="copy-line">
      <code>{text}</code>
      <button type="button" className="btn btn-xs btn-ghost" onClick={copy} aria-label="复制命令">
        {copied ? <Check size={14} /> : <Copy size={14} />}
        {copied ? "已复制" : "复制"}
      </button>
    </div>
  );
}
