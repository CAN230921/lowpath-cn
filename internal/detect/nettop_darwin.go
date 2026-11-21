package detect

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// KernelNetwork observes the game's connected sockets through macOS nettop.
// Only endpoint metadata and byte deltas are read. The watcher never receives
// a packet payload and never changes a route.
type KernelNetwork struct{}

func NewKernelNetwork() *KernelNetwork { return &KernelNetwork{} }

func (*KernelNetwork) Reset() (bool, error) { return false, nil }

func (*KernelNetwork) Watch(onEvent func(FlowEvent)) (FlowWatch, error) {
	if onEvent == nil {
		return nil, errors.New("nettop: missing flow callback")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "/usr/bin/nettop", "-n", "-x", "-d", "-s", "1", "-L", "0", "-J", "bytes_in,bytes_out")
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start nettop: %w", err)
	}
	w := &nettopWatch{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		readNettop(out, onEvent)
		cancel()
		cmd.Wait()
	}()
	return w, nil
}

type nettopWatch struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (w *nettopWatch) Close() error {
	w.cancel()
	<-w.done
	return nil
}

func (w *nettopWatch) Done() <-chan struct{} { return w.done }

func readNettop(stream io.Reader, onEvent func(FlowEvent)) {
	rows := csv.NewReader(stream)
	rows.FieldsPerRecord = -1
	var pid uint32
	in, out := -1, -1
	for {
		row, err := rows.Read()
		if errors.Is(err, io.EOF) || (err != nil && len(row) == 0) {
			return
		}
		if err != nil || len(row) == 0 {
			pid = 0
			continue
		}
		if row[0] == "" {
			in, out = slices.Index(row, "bytes_in"), slices.Index(row, "bytes_out")
			pid = 0
			continue
		}
		event, isSocket := nettopSocket(row[0])
		if !isSocket {
			cut := strings.LastIndexByte(row[0], '.')
			pid = 0
			if cut < 0 {
				continue
			}
			number, _ := strconv.ParseUint(row[0][cut+1:], 10, 32)
			pid = uint32(number)
			continue
		}
		if pid == 0 || !event.Remote.IsValid() || in < 0 || out < 0 || max(in, out) >= len(row) {
			continue
		}
		event.PID = pid
		for _, direction := range []int{out, in} {
			count, err := strconv.ParseUint(row[direction], 10, 64)
			if err == nil && count > 0 {
				event.Outbound = direction == out
				event.Bytes = uint32(min(count, math.MaxUint32))
				onEvent(event)
			}
		}
	}
}

func nettopSocket(label string) (FlowEvent, bool) {
	protocol, endpoints, _ := strings.Cut(strings.TrimSpace(label), " ")
	switch protocol {
	case "tcp4", "tcp6", "udp4", "udp6":
	default:
		return FlowEvent{}, false
	}
	local, remote, _ := strings.Cut(endpoints, "<->")
	ipv6 := protocol[3] == '6'
	return FlowEvent{Proto: protocol[:3], Local: nettopAddress(local, ipv6),
		Remote: nettopAddress(remote, ipv6)}, true
}

func nettopAddress(s string, ipv6 bool) netip.AddrPort {
	if !ipv6 {
		addr, _ := netip.ParseAddrPort(strings.TrimSpace(s))
		return addr
	}
	host := strings.TrimSpace(s)
	i := strings.LastIndexByte(host, '.')
	if i < 0 {
		return netip.AddrPort{}
	}
	port := host[i+1:]
	ip, err := netip.ParseAddr(host[:i])
	if err != nil {
		return netip.AddrPort{}
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return netip.AddrPort{}
	}
	return netip.AddrPortFrom(ip.Unmap(), uint16(n))
}
