package diag

import (
	"fmt"
	"strings"
	"time"
)

// DiagnosisReport is the exportable text version of a diagnosis: the
// verdict, every check, and a summary of the measurements behind them, for
// a forum post or a support ticket. Build it from redacted evidence and a
// redacted location (Location.Redacted, TraceResult.Redacted): the report
// may be handed to anyone.
type DiagnosisReport struct {
	Version   string
	OS        string
	Diagnosis Diagnosis
	Evidence  Evidence
	Location  *Location // nil: not looked up
}

// reportWidth is where the report's prose wraps.
const reportWidth = 78

// Text renders the report. Unknown numbers print as "—".
func (r DiagnosisReport) Text() string {
	var b strings.Builder
	d := r.Diagnosis
	b.WriteString("Lowpath 诊断报告\n")
	fmt.Fprintf(&b, "生成时间 %s", d.At.Format(time.RFC3339))
	if r.Version != "" {
		fmt.Fprintf(&b, " · Lowpath %s", r.Version)
	}
	if r.OS != "" {
		fmt.Fprintf(&b, " · %s", r.OS)
	}
	b.WriteString(".\n")
	conn := "未知"
	if d.Link != "" || d.Interface != "" {
		conn = linkWords(d.Link, d.Interface)
	}
	fmt.Fprintf(&b, "连接：%s。\n", conn)
	if w := wifiLine(r.Evidence.WiFi); w != "" && d.Link == LinkWiFi {
		fmt.Fprintf(&b, "Wi-Fi：%s。\n", w)
	}
	if n := networkLine(r.Location); n != "" {
		fmt.Fprintf(&b, "网络：%s。\n", n)
	}

	v := d.Verdict
	confidence := map[Confidence]string{"high": "高", "medium": "中", "low": "低"}
	fmt.Fprintf(&b, "\n诊断结论：%s（可信度：%s）\n", v.Title, confidence[v.Confidence])
	wrap(&b, "", v.Detail)
	if v.Advice != "" {
		wrap(&b, "", "处理建议："+v.Advice)
	}
	if v.LowpathHelps {
		wrap(&b, "", "可以尝试使用 Lowpath 中继绕行。")
	}
	if len(v.Facts) > 0 {
		b.WriteString("原因\n")
		for _, f := range v.Facts {
			wrap(&b, "  - ", f)
		}
	}

	b.WriteString("\n检查项目\n")
	for _, c := range d.Checks {
		wrap(&b, fmt.Sprintf("  [%s] ", checkWord(c)), c.Name+": "+c.Summary)
		for _, f := range c.Facts {
			wrap(&b, "      - ", f)
		}
	}
	if len(d.Segments) > 0 {
		b.WriteString("\n分段线路\n")
		status := map[string]string{SegmentGood: "正常", SegmentWarn: "留意", SegmentBad: "异常", SegmentUnknown: "未测量", SegmentOff: "未启用"}
		for _, sg := range d.Segments {
			nums := "—"
			if sg.RTTMS != nil || sg.LossPct != nil {
				nums = fmt.Sprintf("%s ms，丢包 %s%%", msText(sg.RTTMS), pctText(sg.LossPct))
			}
			mark := ""
			if sg.Blamed {
				mark = "（异常位置）"
			}
			wrap(&b, fmt.Sprintf("  [%s] ", status[sg.Status]), fmt.Sprintf("%s: %s%s. %s", sg.Name, nums, mark, sg.Summary))
		}
	}
	if len(d.Next) > 0 {
		b.WriteString("\n建议继续测量\n")
		for _, n := range d.Next {
			wrap(&b, "  - ", n.Text)
		}
	}

	b.WriteString("\n测量结果\n")
	r.measurements(&b)
	b.WriteString("\n地址仅保留 /24（IPv4）或 /48（IPv6）网段。路由器主机名已省略。完整路由追踪可单独导出。\n")
	return b.String()
}

func checkWord(c Check) string {
	switch {
	case c.Status == CheckFound && c.Rule == ruleOptimal:
		return "无需加速"
	case c.Status == CheckFound:
		return "异常"
	case c.Status == CheckClear:
		return "正常"
	}
	return "未测量"
}

func networkLine(l *Location) string {
	if l == nil {
		return ""
	}
	var parts []string
	for _, e := range []*Endpoint{l.IPv4, l.IPv6} {
		if e == nil {
			continue
		}
		s := e.IP.String()
		if e.Prefix.IsValid() {
			s = e.Prefix.String()
		}
		if e.ASN != 0 {
			s = fmt.Sprintf("AS%d, %s", e.ASN, s)
		}
		if e.ISP != "" {
			s = e.ISP + " (" + s + ")"
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		if l.Message != "" {
			return "— (" + strings.TrimSuffix(l.Message, ".") + ")"
		}
		return ""
	}
	where := ""
	for _, e := range []*Endpoint{l.IPv4, l.IPv6} {
		if e != nil && e.City != "" {
			where = e.City
			if e.Country != "" {
				where += " (" + e.Country + ")"
			}
			break
		}
	}
	s := strings.Join(parts, "; ")
	if where != "" {
		s += "; " + where
	}
	return s
}

// measurements summarizes each input the engine had.
func (r DiagnosisReport) measurements(b *strings.Builder) {
	ev := r.Evidence
	at := r.Diagnosis.At
	line := func(s string) { wrap(b, "  ", s) }
	if g := ev.Gateway; g != nil {
		s := fmt.Sprintf("路由器探测（%s，%s前", g.Source, ago(at.Sub(g.At)))
		if g.Link != "" || g.Interface != "" {
			s += ", " + linkWords(g.Link, g.Interface)
		}
		s += fmt.Sprintf("）：发送 %d，超时 %d，中位数 %s ms，P%d 为 %s ms，%d 次达到 %d ms。", g.Sent, g.Lost, msText(g.P50MS), g.TailPct, msText(g.TailMS), g.Spikes, spikeMS)
		if g.Limited {
			s += " 路由器限制探测回包，报告不将其计为丢包。"
		}
		line(s)
	} else {
		line("路由器探测尚未运行。打开诊断页或开始加速即可测量。")
	}
	if bl := ev.Bloat; bl != nil {
		grade := bl.Grade
		if grade == "" {
			grade = "—"
		}
		line(fmt.Sprintf("缓冲膨胀测试（%s前，%s）：评级 %s。空载 %s ms，下载增加 %s ms（%s Mbit/s），上传增加 %s ms（%s Mbit/s）。",
			ago(at.Sub(bl.FinishedAt)), linkWords(bl.Link, bl.Interface), grade, msText(bl.IdleMS), plusText(bl.DownIncreaseMS), optMbps(bl.DownMbps),
			plusText(bl.UpIncreaseMS), optMbps(bl.UpMbps)))
		if bl.Note != "" {
			line("  " + bl.Note)
		}
	} else {
		line("缓冲膨胀测试尚未运行。")
	}
	if p := ev.Passive; p != nil {
		state := "正在监测，带宽尚未占满"
		switch {
		case p.Active:
			state = passiveFact(p)
		case !p.Watching:
			state = "未监测：" + p.Reason
		}
		line("加速期间网络监测：" + state)
	}
	if t := ev.Trace; t != nil {
		mode := "ICMP"
		if t.Mode == TraceUDP {
			mode = "UDP 至中继端口"
		}
		line(fmt.Sprintf("追踪至 %s（%s前，%s，完成 %d/%d 轮，%d 跳）：目标丢包 %s%%。",
			traceHost(t), ago(at.Sub(t.FinishedAt)), mode, t.Rounds, t.Planned, len(t.Hops), pctText(t.DestLossPct)))
		if t.Verdict != "" {
			line("  " + t.Verdict)
		}
	} else {
		line("路由追踪尚未运行。")
	}
	for _, p := range []*PathEvidence{ev.Boost, ev.Analysis} {
		if p == nil {
			continue
		}
		what := "线路分析"
		if p.Source == PathBoost {
			what = "当前加速"
		}
		s := fmt.Sprintf("%s · %s（%s前）：直连 %s，本机至中继 %s，中继至服务器 %s ms。", what, p.Game, ago(at.Sub(p.At)),
			pathText(p.Direct), pathText(p.Relay), msText(p.ServerMS))
		line(s)
	}
}

func pathText(s *PathStats) string {
	if s == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f ms，丢包 %.1f%%，抖动 %.1f ms（%d 个样本）", s.RTTMS, s.LossPct, s.JitterMS, s.Samples)
}

// wrap writes text after prefix, wrapped at reportWidth with following
// lines indented to match the prefix.
func wrap(b *strings.Builder, prefix, text string) {
	indent := strings.Repeat(" ", len([]rune(prefix)))
	cur := prefix
	n := len([]rune(cur))
	empty := true
	for _, w := range strings.Fields(text) {
		wl := len([]rune(w))
		if !empty && n+1+wl > reportWidth {
			b.WriteString(cur + "\n")
			cur, n, empty = indent, len([]rune(indent)), true
		}
		if !empty {
			cur += " "
			n++
		}
		cur += w
		n += wl
		empty = false
	}
	b.WriteString(cur + "\n")
}

// wifiLine is the radio link in one line: "-61 dBm, 5 GHz channel 36
// (80 MHz), Wi-Fi 6, 866 Mbit/s Tx, channel 12% busy". No network name or
// access point address: those can locate a home.
func wifiLine(w *WiFiSignal) string {
	if w == nil {
		return ""
	}
	var parts []string
	if w.RSSI != nil {
		parts = append(parts, fmt.Sprintf("%d dBm", *w.RSSI))
	}
	if w.Band != "" {
		ch := w.Band + " GHz"
		if w.Channel != nil {
			ch += fmt.Sprintf(" 信道 %d", *w.Channel)
		}
		if w.WidthMHz != nil {
			ch += fmt.Sprintf(" (%d MHz)", *w.WidthMHz)
		}
		parts = append(parts, ch)
	}
	if w.Generation != "" {
		parts = append(parts, w.Generation)
	}
	if w.TxMbps != nil {
		parts = append(parts, fmt.Sprintf("发送 %.0f Mbit/s", *w.TxMbps))
	}
	if w.BusyPct != nil {
		parts = append(parts, fmt.Sprintf("信道占用 %.0f%%", *w.BusyPct))
	}
	return strings.Join(parts, ", ")
}
