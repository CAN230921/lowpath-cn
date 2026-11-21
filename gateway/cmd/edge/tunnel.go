package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/quicvarint"

	"lowpath-masque-edge/internal/access"
)

const (
	lowpathMagic      = 0xa7
	lowpathData       = 1
	lowpathProbe      = 2
	lowpathProbeReply = 3
	lowpathStats      = 4
	lowpathHeader     = 27
	maxPacket         = 1500
)

func (rt *routes) tunnel(w http.ResponseWriter, r *http.Request, n *node, grant access.Grant) {
	udp, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(n.upstream))
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer udp.Close()

	var saltBytes [8]byte
	rand.Read(saltBytes[:])
	salt := binary.BigEndian.Uint64(saltBytes[:])
	ctx, cancel := context.WithDeadline(r.Context(), grant.ExpiresAt)
	defer cancel()

	stream := w.(http3.HTTPStreamer).HTTPStream()
	w.Header().Set(http3.CapsuleProtocolHeader, "?1")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()

	// The workers never outlive the request, including when either peer closes
	// its stream or when a device is revoked while the tunnel is idle.
	finished := make(chan struct{}, 3)
	go func() {
		defer func() { finished <- struct{}{} }()
		for {
			datagram, err := stream.ReceiveDatagram(ctx)
			if err != nil {
				return
			}
			id, size, err := quicvarint.Parse(datagram)
			if err != nil || id != 0 {
				continue
			}
			packet := datagram[size:]
			if !relayFrame(packet, 0, n.token, salt, true) {
				continue
			}
			if _, err := udp.Write(packet); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { finished <- struct{}{} }()
		var buf [maxPacket + 2]byte
		for {
			size, err := udp.Read(buf[1:])
			if err != nil {
				return
			}
			if !relayFrame(buf[1:1+size], n.token, 0, salt, false) {
				continue
			}
			// Buffer byte 0 is the RFC 9298 UDP payload context ID.
			if err := stream.SendDatagram(buf[:1+size]); err != nil {
				if _, tooLarge := errors.AsType[*quic.DatagramTooLargeError](err); !tooLarge {
					return
				}
			}
		}
	}()
	go func() {
		defer func() { finished <- struct{}{} }()
		// This endpoint transports game packets in HTTP Datagrams. Drain the
		// reliable stream so FIN closes even a tunnel with no UDP traffic.
		_, _ = io.Copy(io.Discard, stream)
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	active := 3
wait:
	for {
		select {
		case <-ctx.Done():
			break wait
		case <-finished:
			active--
			break wait
		case now := <-ticker.C:
			if !rt.stillValid(grant, n.id, now) {
				break wait
			}
		}
	}
	cancel()
	udp.Close()
	stream.CancelRead(quic.StreamErrorCode(http3.ErrCodeNoError))
	stream.Close()
	for range active {
		<-finished
	}
}

func relayFrame(packet []byte, from, to, salt uint64, upstream bool) bool {
	if len(packet) < lowpathHeader || len(packet) > maxPacket || packet[0] != lowpathMagic {
		return false
	}
	kind := packet[1]
	if upstream {
		if kind != lowpathData && kind != lowpathProbe {
			return false
		}
	} else if kind != lowpathData && kind != lowpathProbeReply && kind != lowpathStats {
		return false
	}
	if binary.BigEndian.Uint64(packet[2:10]) != from {
		return false
	}
	binary.BigEndian.PutUint64(packet[2:10], to)
	// Each tunnel gets a distinct flow namespace at the upstream relay.
	flow := binary.BigEndian.Uint64(packet[10:18]) ^ salt
	binary.BigEndian.PutUint64(packet[10:18], flow)
	return true
}
