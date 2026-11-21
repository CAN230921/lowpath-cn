package diag

import (
	"fmt"
	"strings"
	"time"
)

// Report is the trace as a plain-text table in WinMTR's column order
// (loss, sent, received, best, average, worst, last), plus the loss that
// persists to the destination, for pasting into a support ticket. Unknown
// numbers print as "-".
func (r TraceResult) Report() string {
	var b strings.Builder
	mode := "ICMP"
	if r.Mode == TraceUDP {
		mode = "UDP 至中继端口（中间跳使用 ICMP）"
	}
	target := r.Target
	if r.Host != "" && r.Host != r.Target {
		target = fmt.Sprintf("%s (%s)", r.Host, r.Target)
	}
	fmt.Fprintf(&b, "Lowpath 路由追踪 · %s\n", target)
	fmt.Fprintf(&b, "方式：%s。完成 %d/%d 轮，各跳探测间隔 %s。\n", mode, r.Rounds, r.Planned, fmtInterval(r.IntervalS))
	if !r.StartedAt.IsZero() {
		fmt.Fprintf(&b, "开始时间 %s", r.StartedAt.Format(time.RFC3339))
		if !r.FinishedAt.IsZero() {
			fmt.Fprintf(&b, "，耗时 %s", r.FinishedAt.Sub(r.StartedAt).Round(time.Second))
		}
		b.WriteString(".\n")
	}
	b.WriteString("\n")

	const head = "%3s  %-44s %6s %8s %5s %5s %7s %7s %7s %7s %7s\n"
	fmt.Fprintf(&b, head, "#", "主机", "丢包%", "实际丢包%", "发送", "接收", "最低", "平均", "最高", "最近", "标准差")
	row := func(h TraceHop) {
		nr := fmt.Sprint(h.TTL)
		host := h.Addr
		if h.Probe == "udp" {
			nr, host = "UDP", r.EndName
		}
		if host == "" {
			host = "（无回包）"
		}
		if h.Name != "" {
			host = fmt.Sprintf("%s [%s]", h.Name, host)
		}
		if rs := []rune(host); len(rs) > 44 {
			// Cut by character: a relay label may not be ASCII.
			host = string(rs[:43]) + "~"
		}
		fmt.Fprintf(&b, head, nr, host, fmt.Sprintf("%.1f", h.LossPct), optPct(h.PersistPct), fmt.Sprint(h.Sent), fmt.Sprint(h.Recv),
			optMS(h.BestMS), optMS(h.AvgMS), optMS(h.WorstMS), optMS(h.LastMS), optMS(h.StDevMS))
		for _, o := range h.Others {
			fmt.Fprintf(&b, "%3s  %-44s\n", "", "  另有应答 "+o)
		}
	}
	for _, h := range r.Hops {
		row(h)
	}
	if r.End != nil {
		row(*r.End)
	}
	b.WriteString("\n")
	if r.Verdict != "" {
		b.WriteString(r.Verdict + "\n")
	}
	for _, n := range r.Notes {
		b.WriteString(n + "\n")
	}
	b.WriteString("\n实际丢包率统计从该跳延续至终点的丢包。\n" +
		"路由器可能限制探测应答，请结合后续路由判断。\n")
	return b.String()
}

func optPct(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f", *v)
}

func optMS(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f", *v)
}

func fmtInterval(s float64) string {
	if s >= 1 {
		return fmt.Sprintf("%g s", s)
	}
	return fmt.Sprintf("%g ms", s*1000)
}
