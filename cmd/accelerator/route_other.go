//go:build !darwin

package main

import (
	"errors"
	"net/netip"

	"lowpath/internal/capture"
)

func testCapture(netip.AddrPort, string) (capture.Backend, error) { return nil, errors.ErrUnsupported }
