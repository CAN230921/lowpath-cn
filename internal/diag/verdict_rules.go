package diag

import (
	"fmt"
	"strings"
)

// The verdict engine's rules, in the order NewVerdictEngine runs them.

// Rule IDs (Check.Rule).
const (
	ruleHome    = "home"
	ruleLine    = "line"
	ruleISP     = "isp"
	ruleServer  = "server"
	ruleOptimal = "optimal"
)

// VerdictRelay: the route to the user's relay loses traffic on its last
// link or at the relay (a UDP trace that loses only at the relay's port).
const VerdictRelay = "relay"

// homeRule: the router loses pings or answers slowly (gateway loss > 0.5%
// or p99 > 15 ms), or a trace finds loss starting inside the home network.
// On Wi-Fi a weak signal (RSSI < -70 dBm) or the 2.4 GHz band confirms the
// radio link as the cause.
type homeRule struct{}

func (homeRule) check(v *view) (Check, *Verdict) {
	c := Check{Rule: ruleHome, Name: "Wi-Fi 与家庭网络"}
	g := v.gateway
	var facts []string
	if g != nil && g.Lost == g.Sent && !g.Stopped {
		// Many routers don't answer ping at all; that is no loss.
		facts = append(facts, "路由器未回复探测（"+g.Source+"）。部分路由器会忽略探测，无法据此判断丢包。")
		g = nil
	}
	hop := v.homeLossHop()
	// A trace that didn't reach its destination can't confirm any loss.
	traced := v.trace != nil && v.trace.Reached
	if g == nil && !traced {
		c.Status, c.Facts = CheckUnknown, facts
		c.Summary = "保持诊断页打开一分钟以测量路由器，也可在当前连接运行缓冲膨胀测试。"
		if v.ev.Bloat != nil && v.bloat == nil {
			c.Summary = v.bloatWhy + " 保持诊断页打开一分钟以测量路由器，或在当前连接重试测试。"
		}
		return c, nil
	}
	lossBad, tailBad := false, false
	conf := ConfidenceLow
	if g != nil {
		facts = append(facts, gatewayFact(*g, v))
		// A router that limits its replies drops pings, not traffic.
		lossBad = !g.Limited && g.lossPct() > gatewayLossPct && g.Lost >= 2
		tailBad = g.TailMS != nil && *g.TailMS > gatewayTailMS
		if g.Limited {
			facts = append(facts, "路由器限制探测回包，测量排除这些超时。")
		}
		if g.Stopped {
			facts = append(facts, "路由器最初有应答，随后所有探测均超时。")
		}
		conf = v.gatewayConf
	}
	if hop != nil {
		facts = append(facts, fmt.Sprintf("路由追踪（%s前）显示，家庭网络内第 %d 跳开始丢包并持续到终点，丢包率 %.1f%%。",
			ago(v.ev.At.Sub(v.trace.FinishedAt)), hop.TTL, *v.trace.DestLossPct))
		if !lossBad && !tailBad {
			conf = v.traceConf
		} else {
			conf = maxConf(conf, v.traceConf)
		}
	}
	if !lossBad && !tailBad && hop == nil {
		if _, known, fact := wifiSignal(v.ev.WiFi); known && v.linkKind() == LinkWiFi {
			facts = append(facts, fact)
		}
		c.Status, c.Facts = CheckClear, facts
		switch {
		case g != nil:
			c.Summary = "路由器应答快，未测到丢包。"
		default:
			c.Summary = "路由追踪未在家庭网络中发现丢包。"
		}
		if g != nil && g.TailPct < 99 && g.TailMS != nil {
			c.Summary += fmt.Sprintf("（统计到 P%d。罕见的尖峰需要更长的测试。）", g.TailPct)
		}
		return c, nil
	}
	if tailBad && !lossBad && hop == nil {
		// Routers answer pings to themselves at low priority, so slow
		// replies alone can come from the router's CPU, not the link.
		conf = conf.lower()
		facts = append(facts, "仅路由器自身应答偏慢。路由器应答探测优先级较低，结论暂不确定。")
	}

	kind := v.linkKind()
	vd := &Verdict{Segment: SegmentHome, Confidence: conf}
	switch kind {
	case LinkWiFi:
		weak, known, fact := wifiSignal(v.ev.WiFi)
		facts = append(facts, fact)
		if known && !weak {
			vd.ID = VerdictHome
			vd.Title = "家庭网络存在异常"
			vd.Detail = "Wi-Fi 信号较强，但路由器探测存在丢包或高延迟。原因可能在路由器、无线干扰或其他设备占用带宽。"
			vd.Advice = "可重启路由器，再用网线对比是否仍有问题。"
			vd.Confidence = minConf(conf, ConfidenceMedium)
		} else {
			vd.ID = VerdictWiFi
			vd.Title = "Wi-Fi 连接异常"
			vd.Detail = "本机到路由器的 Wi-Fi 链路出现丢包或高延迟，会影响各游戏。"
			vd.Advice = "可改用网线或靠近路由器，切到 5 GHz，避开微波炉等使用 2.4 GHz 的设备。重新测量后对比两种接入。"
			if !known {
				vd.Confidence = minConf(conf, ConfidenceMedium)
			}
		}
	case LinkEthernet:
		vd.ID = VerdictHome
		vd.Title = "家庭网络存在异常"
		vd.Detail = "有线连接下，路由器探测也有丢包或延迟尖峰。可能与路由器、网线或端口有关。"
		vd.Advice = "可重启路由器，并更换网线或端口进行对比。"
		facts = append(facts, "当前使用有线网络。")
	default:
		vd.ID = VerdictHome
		vd.Title = "家庭网络存在异常"
		vd.Detail = "本机到路由器出现丢包或延迟尖峰，游戏流量在家庭网络内就会受影响。"
		vd.Advice = "可重启路由器。若当前使用 Wi-Fi，可改用网线对比。"
		if kind == LinkOther {
			facts = append(facts, "当前使用 VPN 或其他连接，探测目标可能是 VPN 的网关。")
			vd.Confidence = ConfidenceLow
		} else {
			facts = append(facts, "无法识别本机的接入类型。")
			vd.Confidence = minConf(conf, ConfidenceMedium)
		}
	}
	vd.Facts = facts
	c.Status, c.Facts = CheckFound, facts
	c.Summary = vd.Title + "."
	return c, vd
}

// linkKind is the kind of the connection in use, or, when the system
// didn't say, of the one the router pings or the bufferbloat test ran on.
func (v *view) linkKind() string {
	if l := v.ev.Link; l != nil && l.Kind != LinkUnknown {
		return l.Kind
	}
	if v.gateway != nil && v.gateway.Link != LinkUnknown {
		return v.gateway.Link
	}
	if v.bloat != nil {
		return v.bloat.Link
	}
	return LinkUnknown
}

// wifiSignal says whether the radio link is weak or crowded: RSSI below
// -70 dBm, the 2.4 GHz band, or a channel the access point reports busy
// more than 60% of the time. known is false when the system gave none.
func wifiSignal(w *WiFiSignal) (weak, known bool, fact string) {
	if w == nil || (w.RSSI == nil && w.Band == "" && w.BusyPct == nil) {
		return false, false, "无法读取 Wi-Fi 信号强度，暂时无法确认无线链路是否存在异常。"
	}
	var parts []string
	if w.RSSI != nil {
		parts = append(parts, fmt.Sprintf("%d dBm", *w.RSSI))
		weak = *w.RSSI < weakRSSI
	}
	if w.Band != "" {
		parts = append(parts, w.Band+" GHz")
		weak = weak || w.Band == "2.4"
	}
	s := strings.Join(parts, " on ")
	if w.BusyPct != nil {
		if s != "" {
			s += ", "
		}
		s += fmt.Sprintf("channel %.0f%% busy", *w.BusyPct)
		weak = weak || *w.BusyPct > busyChannelPct
	}
	word := "信号良好"
	if weak {
		word = "信号偏弱或信道拥挤"
	}
	return weak, true, fmt.Sprintf("Wi-Fi signal: %s (%s).", s, word)
}

func gatewayFact(g GatewaySample, v *view) string {
	s := fmt.Sprintf("路由器：%d/%d 次超时（%.1f%%），中位数 %s ms", g.Lost, g.Sent, g.lossPct(), msText(g.P50MS))
	if g.TailPct > 0 {
		s += fmt.Sprintf(", %d%% within %s ms", g.TailPct, msText(g.TailMS))
	}
	if g.Spikes > 0 {
		s += fmt.Sprintf("，%d 次回包达到 %d ms", g.Spikes, spikeMS)
	}
	return s + fmt.Sprintf(" (%s, %s ago).", g.Source, ago(v.ev.At.Sub(g.At)))
}

// lineRule: the bufferbloat grade is C or worse, or the passive detector
// sees other traffic filling the line while the game's ping rises.
type lineRule struct{}

func (lineRule) check(v *view) (Check, *Verdict) {
	c := Check{Rule: ruleLine, Name: "线路负载"}
	var facts []string
	vd := &Verdict{ID: VerdictLine, Segment: SegmentHome,
		Advice: "对局中可暂停同一网络的下载、更新、云备份和串流。路由器启用 SQM（fq_codel 或 CAKE）有助于控制带宽占满时的延迟。"}
	if p := v.ev.Passive; p != nil && p.Active {
		facts = append(facts, passiveFact(p))
		vd.Title = "其他流量正在占满带宽"
		vd.Detail = "同一网络的其他流量占用了大部分带宽，游戏延迟随之升高。"
		vd.Confidence = ConfidenceHigh
	}
	b := v.bloat
	bad := false
	if b != nil && b.Grade != "" {
		facts = append(facts, bloatFact(b, v))
		bad = b.Grade == "C" || b.Grade == "D" || b.Grade == "F"
		if bad && vd.Title == "" {
			vd.Title = "高负载时连接延迟明显增加"
			vd.Detail = fmt.Sprintf("缓冲膨胀评级 %s。上传或下载占满带宽后延迟明显增加。", b.Grade)
			vd.Confidence = v.bloatConf
		}
	}
	if vd.Title != "" {
		vd.Facts = facts
		c.Status, c.Facts, c.Summary = CheckFound, facts, vd.Title+"."
		return c, vd
	}
	watching := v.ev.Passive != nil && v.ev.Passive.Watching
	if watching {
		facts = append(facts, "本次加速期间未发现其他流量占满带宽。")
	}
	c.Facts = facts
	switch {
	case b != nil && b.Grade != "":
		c.Status = CheckClear
		c.Summary = fmt.Sprintf("缓冲膨胀评级 %s。带宽占满时延迟仍然稳定。", b.Grade)
		if b.Grade == "B" {
			c.Summary = "缓冲膨胀评级 B。带宽占满时延迟仅小幅升高。"
		}
	case watching:
		c.Status = CheckClear
		c.Summary = "本次加速期间未发现其他流量占满带宽。"
	default:
		c.Status = CheckUnknown
		switch {
		case b != nil && b.Note != "":
			c.Summary = "上次缓冲膨胀测试未获得评级：" + b.Note
		case v.bloatWhy != "":
			c.Summary = v.bloatWhy + " 请在当前连接上运行缓冲膨胀测试。"
		default:
			c.Summary = "请在当前连接上运行缓冲膨胀测试。"
		}
	}
	return c, nil
}

func bloatFact(b *BloatResult, v *view) string {
	return fmt.Sprintf("缓冲膨胀评级 %s（%s前）。下载增加 %s ms，上传增加 %s ms，空载 %s ms。",
		b.Grade, ago(v.ev.At.Sub(b.FinishedAt)), plusText(b.DownIncreaseMS), plusText(b.UpIncreaseMS), msText(b.IdleMS))
}

func passiveFact(p *PassiveState) string {
	var use []string
	if p.OtherDownMbps != nil {
		use = append(use, fmt.Sprintf("%.0f of %s Mbit/s downloading", *p.OtherDownMbps, optMbps(p.LineDownMbps)))
	}
	if p.OtherUpMbps != nil {
		use = append(use, fmt.Sprintf("%.0f of %s Mbit/s uploading", *p.OtherUpMbps, optMbps(p.LineUpMbps)))
	}
	s := "加速期间，其他流量正在占满带宽"
	if len(use) > 0 {
		s += " (" + strings.Join(use, ", ") + ")"
	}
	if p.RTTMS != nil && p.BaselineMS != nil {
		s += fmt.Sprintf("，游戏延迟从 %.0f ms 升至 %.0f ms", *p.BaselineMS, *p.RTTMS)
	}
	if t := p.Top; t != nil {
		s += fmt.Sprintf("，主要流量来自本机的 %s（下行 %.0f Mbit/s，上行 %.0f Mbit/s）", t.Name, t.DownMbps, t.UpMbps)
	}
	return s + "."
}

func optMbps(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f", *v)
}

// ispRule: loss starts at a hop past the home network and persists to the
// destination; or, during a boost, the direct route to the game server
// loses traffic while the relay route doesn't.
type ispRule struct{}

func (ispRule) check(v *view) (Check, *Verdict) {
	c := Check{Rule: ruleISP, Name: "运营商线路"}
	var facts []string
	t := v.trace
	traceClean := false
	if t != nil && t.Reached && v.homeLossHop() == nil {
		age := ago(v.ev.At.Sub(t.FinishedAt))
		switch from := t.LossFrom; {
		case from == nil:
			traceClean = true
			facts = append(facts, fmt.Sprintf("追踪至 %s（%s前）未发现持续至终点的丢包。", traceHost(t), age))
		case *from == 0:
			fact := fmt.Sprintf("UDP 追踪至 %s（%s前），中继端口的探测丢包率为 %.1f%%，前序路由器未发现丢包。", traceHost(t), age, *t.DestLossPct)
			vd := &Verdict{ID: VerdictRelay, Segment: SegmentLowpath, Confidence: v.traceConf, LowpathHelps: true,
				Title:  "到中继的链路存在丢包",
				Detail: "丢包出现在中继前的最后一段链路或中继本身。",
				Advice: "可尝试切换节点，或检查当前中继的负载和网络。",
				Facts:  []string{fact}}
			c.Status, c.Facts, c.Summary = CheckFound, vd.Facts, vd.Title+"."
			return c, vd
		case *from == len(t.Hops) && t.End == nil:
			// Only the destination loses replies: the route up to it is
			// clean (the server rule looks at the destination).
			traceClean = true
			facts = append(facts, fmt.Sprintf("追踪至 %s（%s前），仅目标的探测回包超时，比例 %.1f%%。", traceHost(t), age, *t.DestLossPct))
		case *from >= 1 && *from <= len(t.Hops):
			h := t.Hops[*from-1]
			where := ""
			if h.Name != "" {
				where = " (" + h.Name + ")"
			}
			fact := fmt.Sprintf("追踪至 %s（%s前），家庭网络外第 %d 跳%s开始丢包并持续至终点，丢包率 %.1f%%。",
				traceHost(t), age, h.TTL, where, *t.DestLossPct)
			vd := ispVerdict(v.traceConf, fact, v.ev.Boost != nil)
			c.Status, c.Facts, c.Summary = CheckFound, vd.Facts, vd.Title+"."
			return c, vd
		}
	}
	if p := v.path; p != nil && p.Direct != nil && p.Relay != nil && p.ServerMS != nil && p.Direct.Samples >= minPathSamples && p.Relay.Samples >= minPathSamples {
		// A lossy relay->server leg means the loss is at the server's end,
		// which both routes share: the server rule's case, not the ISP's.
		legClean := p.ServerLossPct == nil || *p.ServerLossPct < routeLossPct
		if p.Direct.LossPct >= routeLossPct && p.Relay.LossPct < cleanLossPct && legClean {
			fact := fmt.Sprintf("%s：游戏直连探测丢包 %.1f%%，中继线路丢包 %.1f%%，中继至服务器延迟 %.0f ms。",
				pathWords(p, v), p.Direct.LossPct, p.Relay.LossPct, *p.ServerMS)
			conf := ConfidenceMedium
			if p.Source == PathAnalysis {
				conf = ConfidenceLow
			}
			vd := ispVerdict(conf, fact, p.Source == PathBoost)
			vd.Facts = append(vd.Facts, methodFact(p))
			c.Status, c.Facts, c.Summary = CheckFound, vd.Facts, vd.Title+"."
			return c, vd
		}
		if p.Direct.LossPct < cleanLossPct {
			facts = append(facts, fmt.Sprintf("%s：游戏直连探测丢包 %.1f%%。", pathWords(p, v), p.Direct.LossPct))
			traceClean = true
		}
	}
	c.Facts = facts
	switch {
	case traceClean:
		c.Status = CheckClear
		c.Summary = "家庭网络以外的线路未发现持续至终点的丢包。"
	case t != nil && !t.Reached:
		c.Status = CheckUnknown
		c.Summary = "上次追踪未抵达目标。请追踪一个中继或能回复探测的游戏服务器。"
	case t != nil && v.homeLossHop() != nil:
		c.Status = CheckUnknown
		c.Summary = "家庭网络内已出现丢包。待本地连接稳定后，再测量运营商线路。"
	default:
		c.Status = CheckUnknown
		c.Summary = v.traceWhy + " 请运行路由追踪。"
		if v.traceWhy == "" {
			c.Summary = "请对游戏服务器或中继执行路由追踪。"
		}
	}
	return c, nil
}

// ispVerdict is the ISP route verdict; boosting: a boost is running, so the
// advice is to keep it on rather than to start one.
func ispVerdict(conf Confidence, fact string, boosting bool) *Verdict {
	advice := "可在游戏页面对比加速和直连的延迟、丢包。若持续发生，可把导出的报告交给运营商。"
	if boosting {
		advice = "中继线路绕开了直连丢包，可继续使用加速。若持续发生，可把导出的报告交给运营商。"
	}
	return &Verdict{ID: VerdictISP, Segment: SegmentISP, Confidence: conf, LowpathHelps: true,
		Title:  "运营商线路存在丢包",
		Detail: "丢包从家庭网络以外开始并延续到终点，中继可能绕开这一段。",
		Advice: advice,
		Facts:  []string{fact}}
}

func traceHost(t *TraceResult) string {
	switch {
	case t.Host != "":
		return t.Host
	case t.Target != "":
		return t.Target
	}
	return "目标"
}

// pathWords says where path numbers come from: "During the Counter-Strike
// 2 boost" or "The route analysis for Dota 2 (5 min ago)".
func pathWords(p *PathEvidence, v *view) string {
	game := p.Game
	if game == "" {
		game = "游戏"
	}
	if p.Source == PathBoost {
		return game + " 加速期间"
	}
	return fmt.Sprintf("%s 的线路分析（%s前）", game, ago(v.ev.At.Sub(p.At)))
}

func methodFact(p *PathEvidence) string {
	if p.SameMethod {
		return "两条线路使用同一方式测量同一台服务器。"
	}
	return "直连采用 ICMP 探测，中继采用 UDP 探测，比较时需考虑协议差异。"
}

// serverRule: the route is clean up to the relay, and the relay's leg to
// the game server loses traffic or jitters; or, during a boost, both the
// direct route and the relay fail to get steady answers from the server.
type serverRule struct{}

func (serverRule) check(v *view) (Check, *Verdict) {
	c := Check{Rule: ruleServer, Name: "游戏服务器"}
	var facts []string
	vd := &Verdict{ID: VerdictServer, Segment: SegmentServer,
		Title:  "游戏服务器一侧存在异常",
		Detail: "本机到中继的线路稳定，异常可能位于中继到游戏服务器之间，或游戏服务器本身。",
		Advice: "可尝试切换游戏区服，或查看游戏服务器状态。"}
	p := v.path
	relayClean := p != nil && p.Relay != nil && p.Relay.Samples >= minPathSamples && p.Relay.LossPct < cleanLossPct && p.Relay.JitterMS < cleanJitterMS
	if relayClean {
		legLoss := p.ServerLossPct != nil && *p.ServerLossPct >= routeLossPct
		legJitter := p.ServerJitterMS != nil && *p.ServerJitterMS >= badJitterMS
		if legLoss || legJitter {
			fact := fmt.Sprintf("%s：本机到中继丢包 %.1f%%，抖动 %.0f ms。中继到服务器丢包 %s%%，抖动 %s ms。",
				pathWords(p, v), p.Relay.LossPct, p.Relay.JitterMS, pctText(p.ServerLossPct), msText(p.ServerJitterMS))
			vd.Confidence = ConfidenceHigh
			if p.Source == PathAnalysis {
				vd.Confidence = ConfidenceMedium
			}
			vd.Facts = []string{fact}
			c.Status, c.Facts, c.Summary = CheckFound, vd.Facts, vd.Title+"."
			return c, vd
		}
		if p.Source == PathBoost && p.ServerMS == nil && p.Direct != nil && p.Direct.Samples >= minPathSamples && p.Direct.LossPct >= routeLossPct {
			// Two different routes, one shared end. Only a hint: the relay
			// reporting no time can also mean it can't ping at all (no ICMP
			// rights, an older relay, a boost a few seconds old), and an ISP
			// problem on the direct route would look the same.
			vd.Confidence = ConfidenceLow
			vd.Title = "游戏服务器一侧可能存在异常"
			vd.Detail = "游戏直连探测丢包，本机到中继的线路稳定，但中继未报告到服务器的延迟。游戏服务器一侧和运营商直连线路都可能存在异常。"
			vd.Advice = "如果加速有改善，可继续使用。也可切换区服或查看游戏服务器状态。"
			vd.Facts = []string{
				fmt.Sprintf("%s：游戏直连探测丢包 %.1f%%，本机到中继丢包 %.1f%%，中继未报告到服务器的延迟。",
					pathWords(p, v), p.Direct.LossPct, p.Relay.LossPct),
				"游戏服务器可能限制探测应答，这个结果只提供排查线索。",
			}
			c.Status, c.Facts, c.Summary = CheckFound, vd.Facts, vd.Title+"."
			return c, vd
		}
	}
	if t := v.trace; t != nil && v.ev.TraceKind == TraceKindServer && t.Reached && t.LossFrom != nil && *t.LossFrom == len(t.Hops) && t.End == nil {
		facts = append(facts, fmt.Sprintf("仅游戏服务器探测超时（%.1f%%）。部分服务器会限制应答，仍需结合其他测量判断。", *t.DestLossPct))
	}
	switch {
	case p != nil && p.Direct != nil && p.Direct.Samples >= minPathSamples && p.Direct.LossPct < cleanLossPct && p.Direct.JitterMS < badJitterMS:
		facts = append(facts, fmt.Sprintf("%s：游戏服务器回复了本机 %.1f%% 的直连探测，抖动 %.0f ms。", pathWords(p, v), 100-p.Direct.LossPct, p.Direct.JitterMS))
		c.Status = CheckClear
		c.Summary = "游戏服务器应答稳定。"
	case p != nil && p.ServerLossPct != nil && *p.ServerLossPct < cleanLossPct && (p.ServerJitterMS == nil || *p.ServerJitterMS < badJitterMS):
		c.Status = CheckClear
		c.Summary = "中继到游戏服务器的链路表现稳定。"
	case p != nil && p.Direct == nil:
		c.Status = CheckUnknown
		c.Summary = "游戏服务器未回复本机探测。"
	default:
		c.Status = CheckUnknown
		c.Summary = "请在游戏页测量线路，或启动加速。"
	}
	if p != nil && p.ServerMS != nil {
		facts = append(facts, fmt.Sprintf("中继到游戏服务器延迟 %.0f ms，该段丢包率暂无数据。", *p.ServerMS))
	}
	c.Facts = facts
	return c, nil
}

// optimalRule: the direct route is within 2 ms of the relay route and no
// lossier, so Lowpath isn't needed for this game.
type optimalRule struct{}

func (optimalRule) check(v *view) (Check, *Verdict) {
	c := Check{Rule: ruleOptimal, Name: "中继与直连"}
	p := v.path
	switch {
	case p == nil || p.Relay == nil:
		c.Status = CheckUnknown
		c.Summary = "暂无直连与中继的对照数据。请在游戏页测量线路或启动加速。"
		return c, nil
	case p.Direct == nil:
		c.Status = CheckUnknown
		c.Summary = "游戏服务器未回复探测，暂时无法比较直连和中继。"
		return c, nil
	case p.Direct.Samples < minPathSamples || p.Relay.Samples < minPathSamples:
		c.Status = CheckUnknown
		c.Summary = "线路样本太少，暂时无法比较。"
		return c, nil
	}
	d := p.Direct
	relayMS, whole := p.relayTotal()
	if !whole {
		relayMS = p.Relay.RTTMS
	}
	relayLoss := p.Relay.LossPct
	if p.ServerLossPct != nil {
		relayLoss += *p.ServerLossPct
	}
	route := "中继线路"
	if !whole {
		route = "仅本机到中继"
	}
	fact := fmt.Sprintf("%s：直连 %.0f ms、丢包 %.1f%%，%s %.0f ms、丢包 %.1f%%。", pathWords(p, v), d.RTTMS, d.LossPct, route, relayMS, relayLoss)
	facts := []string{fact, methodFact(p)}
	// Without the relay's leg to the server the relay route is at least
	// relayMS, so "直连和中继差异很小" still holds; "the relay is
	// better" can't be said.
	if d.RTTMS <= relayMS+optimalMS && d.LossPct <= relayLoss {
		vd := &Verdict{ID: VerdictOptimal, Segment: SegmentLowpath, Confidence: ConfidenceMedium,
			Title:  "直连已经足够稳定",
			Detail: fmt.Sprintf("直连和最佳中继的延迟差在 %d ms 以内，且丢包率相当，预计加速改善有限。", optimalMS),
			Advice: "当前可使用直连。晚高峰线路可能变化，届时可重新测量。",
			Facts:  facts}
		switch {
		case p.SameMethod:
			vd.Confidence = ConfidenceHigh
		case p.Source == PathAnalysis && v.ev.At.Sub(p.At) > v.cfg.Fresh:
			vd.Confidence = ConfidenceLow
		}
		c.Status, c.Facts, c.Summary = CheckFound, facts, vd.Title+"."
		return c, vd
	}
	c.Facts = facts
	if !whole {
		c.Status = CheckUnknown
		c.Summary = "中继到游戏服务器的延迟尚未测量，暂时无法比较完整线路。"
		return c, nil
	}
	c.Status = CheckClear
	c.Summary = "中继线路的测量结果优于直连。"
	return c, nil
}

func maxConf(a, b Confidence) Confidence {
	if confRank(a) >= confRank(b) {
		return a
	}
	return b
}

func minConf(a, b Confidence) Confidence {
	if confRank(a) <= confRank(b) {
		return a
	}
	return b
}

func confRank(c Confidence) int {
	switch c {
	case ConfidenceHigh:
		return 2
	case ConfidenceMedium:
		return 1
	}
	return 0
}

// msText is a latency for the facts: one decimal below 10 ms, "—" unknown.
func msText(v *float64) string {
	switch {
	case v == nil:
		return "—"
	case *v < 10:
		return fmt.Sprintf("%.1f", *v)
	}
	return fmt.Sprintf("%.0f", *v)
}

// pctText is a loss for the facts, "—" unknown.
func pctText(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f", *v)
}

// plusText is a latency increase for the facts: "+12", "—" unknown.
func plusText(v *float64) string {
	if v == nil {
		return "—"
	}
	return "+" + msText(v)
}
