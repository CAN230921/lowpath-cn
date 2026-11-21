package main

import (
	"errors"
	"net/netip"

	"lowpath/internal/capture"
)

func testCapture(target netip.AddrPort, gatewayIP string) (capture.Backend, error) {
	ip, err := netip.ParseAddr(gatewayIP)
	if err != nil || !ip.Is4() || ip == target.Addr() {
		return nil, errors.New("test-route requires a different -gateway-ip to avoid recursion")
	}
	return capture.NewHostRoute(target), nil
}
