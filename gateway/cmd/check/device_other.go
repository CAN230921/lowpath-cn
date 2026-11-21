//go:build !darwin

package main

import (
	"context"
	"errors"
	"net"
)

func deviceSocket(context.Context, string) (net.PacketConn, error) {
	return nil, errors.New("interface diagnostics are only supported on macOS")
}
